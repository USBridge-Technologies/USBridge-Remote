package moonlight

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeSunshine is a minimal stand-in for Sunshine/RustShine's NvHTTP surface
// (/launch, /resume, /cancel), modeling the one behavior that actually
// determines whether a codec switch takes effect: /launch applies the
// request's "mode" query param to a fresh encoder session, while /resume
// reattaches to whatever session is already running and does NOT apply it —
// exactly like real GameStream servers, which only reconfigure the encoder
// on a fresh /launch.
type fakeSunshine struct {
	mu          sync.Mutex
	running     bool
	activeMode  string // "mode" param from the /launch that is currently active
	cancelDelay time.Duration
	launches    int
	resumes     int
	cancels     int
}

func (f *fakeSunshine) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/launch":
			f.mu.Lock()
			f.launches++
			if f.running {
				f.mu.Unlock()
				fmt.Fprint(w, `<root status_code="400"><status_message>An app is already running on this host</status_message></root>`)
				return
			}
			f.running = true
			f.activeMode = r.URL.Query().Get("mode")
			f.mu.Unlock()
			fmt.Fprint(w, `<root status_code="200"><sessionUrl0>rtsp://127.0.0.1:48010</sessionUrl0></root>`)
		case "/resume":
			f.mu.Lock()
			f.resumes++
			if !f.running {
				f.mu.Unlock()
				fmt.Fprint(w, `<root status_code="400"><status_message>No running app to resume</status_message></root>`)
				return
			}
			f.mu.Unlock()
			// Deliberately does NOT update activeMode -- see doc comment above.
			fmt.Fprint(w, `<root status_code="200"><sessionUrl0>rtsp://127.0.0.1:48010</sessionUrl0></root>`)
		case "/cancel":
			f.mu.Lock()
			delay := f.cancelDelay
			f.mu.Unlock()
			if delay > 0 {
				time.Sleep(delay)
			}
			f.mu.Lock()
			f.running = false
			f.cancels++
			f.mu.Unlock()
			fmt.Fprint(w, `<root status_code="200"></root>`)
		default:
			http.NotFound(w, r)
		}
	}
}

func (f *fakeSunshine) snapshot() (running bool, activeMode string, launches, resumes, cancels int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running, f.activeMode, f.launches, f.resumes, f.cancels
}

// newTestClient points a moonlight.Client at srv (an httptest.NewTLSServer
// wrapping a fakeSunshine) using a throwaway in-memory identity -- Launch/
// Quit/Resume all go over the HTTPS client, which skips cert verification
// (see NewClient), so httptest's self-signed cert is accepted as-is.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split test server host:port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}

	identity := testIdentity(t)
	return NewClient(host, port, port, identity)
}

// testIdentity generates a throwaway client identity backed by a temp dir,
// so tests never touch the real ~/.config/usbridge-client identity file.
func testIdentity(t *testing.T) *Identity {
	t.Helper()
	prevOverride := ConfigDirOverride
	ConfigDirOverride = t.TempDir()
	t.Cleanup(func() { ConfigDirOverride = prevOverride })

	identity, err := LoadOrGenerateIdentity()
	if err != nil {
		t.Fatalf("LoadOrGenerateIdentity: %v", err)
	}
	return identity
}

// TestLaunchAppliesModeButResumeDoesNot pins the exact protocol mechanism
// behind the "codec selection silently ignored" bug: Launch() tries /launch
// first and only falls back to /resume if /launch is rejected (client.go's
// doLaunchOrResume). A fresh /launch applies the new "mode" request; a
// /resume fallback reattaches to whatever the server already had running,
// discarding it. If Sunshine still thinks the previous session is alive when
// the next Launch() call lands (e.g. because the client didn't wait for its
// own /cancel to be processed first), the request silently degrades to
// /resume and the new mode/codec never reaches the encoder.
func TestLaunchAppliesModeButResumeDoesNot(t *testing.T) {
	f := &fakeSunshine{}
	srv := httptest.NewTLSServer(f.handler())
	defer srv.Close()
	c := newTestClient(t, srv)

	// First launch: fresh session, /launch succeeds, mode is applied.
	if _, _, err := c.Launch(1, "h264", 1920, 1080, 60, 10000); err != nil {
		t.Fatalf("first Launch: %v", err)
	}
	if running, mode, launches, resumes, _ := f.snapshot(); !running || mode != "1920x1080x60" || launches != 1 || resumes != 0 {
		t.Fatalf("after first Launch: running=%v mode=%q launches=%d resumes=%d, want true/%q/1/0",
			running, mode, launches, resumes, "1920x1080x60")
	}

	// Simulate switching codecs (a new VideoMode request) WITHOUT telling
	// Sunshine to end the previous session first -- the server still thinks
	// the old app is running, so /launch is rejected and the client falls
	// back to /resume, which does not apply the new mode.
	if _, _, err := c.Launch(1, "h265", 1280, 720, 30, 8000); err != nil {
		t.Fatalf("second Launch (expected to fall back to /resume, not error): %v", err)
	}
	if running, mode, launches, resumes, _ := f.snapshot(); !running || mode != "1920x1080x60" || launches != 2 || resumes != 1 {
		t.Fatalf("after second Launch (no /cancel first): running=%v mode=%q launches=%d resumes=%d, want true/%q(unchanged)/2/1",
			running, mode, launches, resumes, "1920x1080x60")
	}

	// Now end the session first (what MoonlightService.Disconnect does via
	// Quit), then relaunch with the new mode: /launch should succeed this
	// time and the new mode should actually take effect.
	if err := c.Quit(1); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if _, _, err := c.Launch(1, "h265", 1280, 720, 30, 8000); err != nil {
		t.Fatalf("third Launch (after Quit): %v", err)
	}
	if running, mode, launches, resumes, cancels := f.snapshot(); !running || mode != "1280x720x30" || launches != 3 || resumes != 1 || cancels != 1 {
		t.Fatalf("after Quit+Launch: running=%v mode=%q launches=%d resumes=%d cancels=%d, want true/%q/3/1/1",
			running, mode, launches, resumes, cancels, "1280x720x30")
	}
}

// TestQuitEndsSession pins Quit()'s effect in isolation: after Quit(), the
// server no longer considers any app running, so the next Launch() must
// land on /launch, not /resume.
func TestQuitEndsSession(t *testing.T) {
	f := &fakeSunshine{}
	srv := httptest.NewTLSServer(f.handler())
	defer srv.Close()
	c := newTestClient(t, srv)

	if _, _, err := c.Launch(1, "h264", 1920, 1080, 60, 10000); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := c.Quit(1); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if running, _, _, _, cancels := f.snapshot(); running || cancels != 1 {
		t.Fatalf("after Quit: running=%v cancels=%d, want false/1", running, cancels)
	}

	if _, _, err := c.Launch(1, "h265", 1280, 720, 30, 8000); err != nil {
		t.Fatalf("Launch after Quit: %v", err)
	}
	if _, mode, launches, resumes, _ := f.snapshot(); mode != "1280x720x30" || launches != 2 || resumes != 0 {
		t.Fatalf("Launch after Quit: mode=%q launches=%d resumes=%d, want %q/2/0", mode, launches, resumes, "1280x720x30")
	}
}

// TestNewClientPanicsOnNilIdentity guards NewClient's documented
// panic-on-nil-identity contract, so a future refactor that accidentally
// drops the identity check fails loudly in tests instead of nil-panicking
// deep inside TLS setup.
func TestNewClientPanicsOnNilIdentity(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewClient(nil identity) did not panic")
		}
	}()
	NewClient("host", 47989, 47984, nil)
}
