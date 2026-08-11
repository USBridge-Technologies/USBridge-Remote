package moonlightclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestSession_RealSunshine is a manual, opt-in end-to-end check against a
// REAL, already-running Sunshine instance -- skipped entirely if none is
// reachable, mirroring the pattern
// agent/internal/streamhost/sunshine_realbinary_test.go uses for its own
// opt-in real-binary check (USBRIDGE_SUNSHINE_BIN).
//
// Deliberately targets an *already-running* Sunshine rather than launching
// a fresh throwaway one: getting Sunshine's video pipeline to actually
// produce frames needs a working capture backend (GPU access, a live
// X11/Wayland session, and for KMS specifically, CAP_SYS_ADMIN normally
// granted via the agent's own sunshine_capexec + setcap flow, see
// streamhost.sunshineBackend.SetCapExecPath) -- reproducing all of that
// reliably from a `go test` process isn't practical, and a throwaway
// instance that fails to find a working encoder can't prove anything about
// this package (confirmed while developing this test: capture=x11 against
// a nested Xvfb display never even attempts to open the X connection, and
// capture=kms fails outside the agent's own capability-grant flow with
// "Failed to gain CAP_SYS_ADMIN"). Pointing at whatever Sunshine instance
// is already running for real (the developer's own agent, or a manually
// started one) sidesteps all of that and is exactly how this package was
// actually verified end-to-end during development -- see the task's final
// report for the real packet counts observed this way.
//
// Configure via env vars (all default to this repo's standard bundled
// agent layout under ~/.config/usbridge-agent/sunshine):
//   - MOONLIGHTCLIENT_TEST_HTTP_PORT (default 47989)
//   - MOONLIGHTCLIENT_TEST_HTTPS_PORT (default 47984)
//   - MOONLIGHTCLIENT_TEST_ADMIN_PORT (default 47990)
//   - MOONLIGHTCLIENT_TEST_ADMIN_PASS_FILE (default
//     ~/.config/usbridge-agent/sunshine/usbridge_admin_pass)
func TestSession_RealSunshine(t *testing.T) {
	httpPort := envInt(t, "MOONLIGHTCLIENT_TEST_HTTP_PORT", 47989)
	httpsPort := envInt(t, "MOONLIGHTCLIENT_TEST_HTTPS_PORT", 47984)
	adminPort := envInt(t, "MOONLIGHTCLIENT_TEST_ADMIN_PORT", 47990)

	if !waitForPort(adminPort, 500*time.Millisecond) {
		t.Skipf("no Sunshine admin API reachable on 127.0.0.1:%d -- start a real Sunshine instance (e.g. the agent itself) to run this real end-to-end check", adminPort)
	}

	passFile := os.Getenv("MOONLIGHTCLIENT_TEST_ADMIN_PASS_FILE")
	if passFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no home directory to locate the default admin password file")
		}
		passFile = filepath.Join(home, ".config", "usbridge-agent", "sunshine", "usbridge_admin_pass")
	}
	passBytes, err := os.ReadFile(passFile)
	if err != nil {
		t.Skipf("can't read Sunshine admin password at %s: %v", passFile, err)
	}
	adminPass := strings.TrimSpace(string(passBytes))

	submitPIN := func(pin string) error {
		body, _ := json.Marshal(map[string]string{"pin": pin})
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("https://127.0.0.1:%d/api/pin", adminPort), bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth("sunshine", adminPass)
		client := &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("sunshine /api/pin returned HTTP %d", resp.StatusCode)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := Config{
		Host:        "127.0.0.1",
		HTTPPort:    httpPort,
		HTTPSPort:   httpsPort,
		StateDir:    t.TempDir(),
		Width:       1280,
		Height:      720,
		FPS:         30,
		BitrateKbps: 8000,
		SubmitPIN:   submitPIN,
	}

	session, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("moonlightclient.Start: %v", err)
	}
	defer session.Stop()

	t.Logf("video RTP addr: %s, audio RTP addr: %s", session.VideoRTPAddr(), session.AudioRTPAddr())

	videoPackets, videoBytes := countUDPPackets(t, session.VideoRTPAddr(), 8*time.Second)
	t.Logf("captured %d video RTP packets (%d bytes) in 8s", videoPackets, videoBytes)
	if videoPackets == 0 {
		t.Errorf("no video RTP packets received on %s -- streaming did not actually start", session.VideoRTPAddr())
	}
}

func envInt(t *testing.T, name string, def int) int {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("invalid %s=%q: %v", name, v, err)
	}
	return n
}

func waitForPort(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// countUDPPackets listens on addr for duration and counts how many
// datagrams arrive -- the actual "is Sunshine really streaming" proof this
// test exists for. Binding our own listener on the exact port
// establishRTPAddr (ping.go) just pinged Sunshine from/released only works
// because nothing else (in particular no real webrtcbridge) is also bound
// to it during the test.
func countUDPPackets(t *testing.T, addr string, duration time.Duration) (packets, totalBytes int) {
	t.Helper()
	udpAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		t.Fatalf("resolve %s: %v", addr, err)
	}
	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	defer conn.Close()

	buf := make([]byte, 65536)
	deadline := time.Now().Add(duration)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(remaining))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		packets++
		totalBytes += n
	}
}
