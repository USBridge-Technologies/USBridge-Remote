package streamhost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// newRustshineStatusStub starts a fake gamestream-server admin API serving
// /api/status with the given body, and returns a rustshineBackend pointed at
// it. Unlike Sunshine's /serverinfo probe, RustShine's /api/status is
// authenticated HTTPS (see fetchStatus's doc comment) -- InsecureSkipVerify
// on rustshineAdminHTTPClient means httptest's self-signed cert is accepted
// as-is, same as the real client-to-gamestream-server connection.
func newRustshineStatusStub(t *testing.T, status statusResponse) *rustshineBackend {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(status); err != nil {
			t.Fatalf("encode stub status: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse stub URL: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse stub port: %v", err)
	}
	return &rustshineBackend{adminPort: port}
}

// TestRustshineCurrentVideoCodec_ReportsActiveCodec is the RustShine-backend
// half of the "does the agent correctly report back which codec actually
// ended up running" contract that sunshine_backend_test.go already pins for
// Sunshine -- RustShine has its own admin API (fetchStatus, /api/status)
// instead of Sunshine's log-scrape, and had no test coverage at all before
// this. If this ever misreports, a client-side codec switch that DID take
// effect on the server would still look broken in the video settings popup,
// which reads this value as its "agent-hint" preselect.
func TestRustshineCurrentVideoCodec_ReportsActiveCodec(t *testing.T) {
	b := newRustshineStatusStub(t, statusResponse{ActiveVideoCodec: "h265"})
	if got := b.CurrentVideoCodec(); got != "h265" {
		t.Errorf("CurrentVideoCodec() = %q, want %q", got, "h265")
	}
}

func TestRustshineCurrentVideoCodec_EmptyDefaultsToH264(t *testing.T) {
	b := newRustshineStatusStub(t, statusResponse{ActiveVideoCodec: ""})
	if got := b.CurrentVideoCodec(); got != "h264" {
		t.Errorf("CurrentVideoCodec() = %q, want %q", got, "h264")
	}
}

func TestRustshineCurrentVideoCodec_UnreachableDefaultsToH264(t *testing.T) {
	// No server listening on this port at all.
	b := &rustshineBackend{adminPort: 1}
	if got := b.CurrentVideoCodec(); got != "h264" {
		t.Errorf("CurrentVideoCodec() = %q, want %q", got, "h264")
	}
}

// TestRustshineColorAndHdrStatus_PassThroughFromStatusAPI pins that the
// RustShine Pro color-upgrade fields (4:4:4 chroma, HDR) are relayed
// verbatim from gamestream-server's own report, not derived/guessed --
// these directly gate the video settings popup's 4:4:4/HDR checkboxes (see
// CodecProbe's doc comment in backend.go).
func TestRustshineColorAndHdrStatus_PassThroughFromStatusAPI(t *testing.T) {
	b := newRustshineStatusStub(t, statusResponse{
		ActiveVideoCodec:  "h265",
		ActiveChroma444:   true,
		Color444Available: true,
		ActiveHdr:         false,
		HdrAvailable:      true,
	})

	if active, available := b.Color444Status(); !active || !available {
		t.Errorf("Color444Status() = (%v, %v), want (true, true)", active, available)
	}
	if active, available := b.HdrStatus(); active || !available {
		t.Errorf("HdrStatus() = (%v, %v), want (false, true)", active, available)
	}
}

func TestRustshineColorAndHdrStatus_UnreachableDefaultsToFalse(t *testing.T) {
	b := &rustshineBackend{adminPort: 1}
	if active, available := b.Color444Status(); active || available {
		t.Errorf("Color444Status() = (%v, %v), want (false, false)", active, available)
	}
	if active, available := b.HdrStatus(); active || available {
		t.Errorf("HdrStatus() = (%v, %v), want (false, false)", active, available)
	}
}
