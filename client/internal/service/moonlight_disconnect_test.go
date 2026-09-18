package service

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

	"usbridge-client/internal/api/moonlight"
)

// fakeSunshine is a minimal stand-in for Sunshine/RustShine's NvHTTP surface,
// modeling the one behavior that actually determines whether a codec switch
// takes effect: /launch applies the request's "mode" query param to a fresh
// encoder session, while /resume reattaches to whatever session is already
// running and does NOT apply it. /cancel (what Quit() calls) can be given an
// artificial delay to simulate a slow-but-real network round trip, so tests
// can reproduce the exact race MoonlightService.Disconnect used to lose.
type fakeSunshine struct {
	mu          sync.Mutex
	running     bool
	activeMode  string
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

// newTestMoonlightClient points a moonlight.Client at srv using a throwaway
// in-memory identity, so tests never touch the real on-disk identity file.
func newTestMoonlightClient(t *testing.T, srv *httptest.Server) *moonlight.Client {
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

	prevOverride := moonlight.ConfigDirOverride
	moonlight.ConfigDirOverride = t.TempDir()
	t.Cleanup(func() { moonlight.ConfigDirOverride = prevOverride })

	identity, err := moonlight.LoadOrGenerateIdentity()
	if err != nil {
		t.Fatalf("LoadOrGenerateIdentity: %v", err)
	}
	return moonlight.NewClient(host, port, port, identity)
}

// TestDisconnectWaitsForCancelBeforeNextLaunchWins is the regression test for
// the codec-switch bug: changing the codec in the video settings dialog while
// already streaming stops the current session (VideoWidget.stopVideoInternal
// -> MoonlightService.Disconnect) and immediately restarts it with the new
// VideoMode (reconcileVideoState -> startVideoWithParamsInternal -> Launch).
//
// Disconnect used to fire Sunshine's /cancel (via Quit) in an unwaited
// goroutine and return immediately. With a real network round trip in
// between, the very next Launch() could reach Sunshine before /cancel had
// been processed, so Sunshine still reported the old app as running,
// Launch() silently fell back to /resume, and the newly requested
// mode/codec was discarded -- exactly matching the field report "switching
// to H265 in the popup does nothing, server keeps sending H264."
//
// This drives the real MoonlightService.Disconnect (not a reimplementation)
// against a fake NvHTTP server with an artificial /cancel delay, and asserts
// that a Launch() issued right after Disconnect returns lands on /launch
// with the new mode applied -- never on /resume.
func TestDisconnectWaitsForCancelBeforeNextLaunchWins(t *testing.T) {
	for _, delay := range []time.Duration{0, 150 * time.Millisecond, 800 * time.Millisecond} {
		delay := delay
		t.Run(delay.String(), func(t *testing.T) {
			f := &fakeSunshine{cancelDelay: delay}
			srv := httptest.NewTLSServer(f.handler())
			defer srv.Close()

			client := newTestMoonlightClient(t, srv)
			m := &MoonlightService{client: client}

			// Seed state as if an H264 session is already running -- what the
			// video widget's live state looks like right before the user
			// applies a codec change in the settings popup.
			if _, _, err := client.Launch(1, "h264", 1920, 1080, 60, 10000); err != nil {
				t.Fatalf("seed Launch: %v", err)
			}
			m.lastAppId = 1

			start := time.Now()
			if err := m.Disconnect(); err != nil {
				t.Fatalf("Disconnect: %v", err)
			}
			elapsed := time.Since(start)
			// The bounded wait added to Disconnect caps at 1.5s; StopStream's
			// own no-op path and everything else in Disconnect is effectively
			// instant, so Disconnect must never take meaningfully longer than
			// that even when /cancel is artificially slow.
			if elapsed > 2*time.Second {
				t.Fatalf("Disconnect took %v, want bounded well under 2s (never block forever)", elapsed)
			}

			// Mirrors reconcileVideoState: restart immediately with the new
			// VideoMode (H265, lower resolution/fps -- the user's new choice).
			if _, _, err := client.Launch(1, "h265", 1280, 720, 30, 8000); err != nil {
				t.Fatalf("post-Disconnect Launch: %v", err)
			}

			running, mode, launches, resumes, cancels := f.snapshot()
			if resumes != 0 {
				t.Errorf("delay=%v: post-Disconnect Launch fell back to /resume (resumes=%d) -- the new codec/mode request was silently ignored", delay, resumes)
			}
			if mode != "1280x720x30" {
				t.Errorf("delay=%v: server's active mode = %q, want %q (the new request) -- codec/resolution switch did not take effect", delay, mode, "1280x720x30")
			}
			if !running || launches != 2 || cancels != 1 {
				t.Errorf("delay=%v: running=%v launches=%d cancels=%d, want true/2/1", delay, running, launches, cancels)
			}
		})
	}
}

// TestDisconnectNeverBlocksForever pins the "best-effort, bounded" half of
// the fix: if Sunshine never answers /cancel at all, Disconnect must still
// return in bounded time rather than hanging the UI's stop/restart flow.
func TestDisconnectNeverBlocksForever(t *testing.T) {
	// block is closed to release the handler below and let it finally
	// respond, so httptest.Server.Close() (which waits for in-flight
	// requests to finish) doesn't itself hang forever. Deferred AFTER
	// srv.Close() below so it runs FIRST (defers are LIFO) -- close(block)
	// must unblock the handler before srv.Close() waits on it.
	block := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/launch":
			fmt.Fprint(w, `<root status_code="200"><sessionUrl0>rtsp://127.0.0.1:48010</sessionUrl0></root>`)
		case "/cancel":
			<-block // never responds until the test cleans up
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer close(block)

	client := newTestMoonlightClient(t, srv)
	m := &MoonlightService{client: client}
	if _, _, err := client.Launch(1, "h264", 1920, 1080, 60, 10000); err != nil {
		t.Fatalf("seed Launch: %v", err)
	}
	m.lastAppId = 1

	start := time.Now()
	if err := m.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Disconnect took %v with /cancel hanging forever, want bounded well under 2s", elapsed)
	}
}
