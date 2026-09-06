package gui

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"usbridge-client/internal/api"
)

// newTestUSBClient points a *api.USBClient at an httptest.Server -- NewUSBClient
// only accepts a separate host/port (it builds baseURL itself), so this pulls
// both back out of the server's URL.
func newTestUSBClient(t *testing.T, server *httptest.Server) *api.USBClient {
	t.Helper()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return api.NewUSBClient(host, port, 5)
}

// TestVerifyActiveConnectionRejectsWrongMasterKey pins the fix for a real
// false-positive-connect bug: a wrong master key/password used to leave the
// client showing "connected" with every subsequent authenticated call
// silently 401ing, because verifyActiveConnectionWithContext (the gate right
// before doConnectWithProtocol declares success) called
// TestConnectionWithContext, which hits /api/healthz -- a path the agent
// registers as public/unauthenticated (see agent/internal/api/security.go's
// isPublicPath) specifically so reachability probes work before a key is
// even known. A bad key's master-sync call gets a real 401 earlier in
// doConnect, but that's treated as non-fatal for direct/auto protocols (see
// its own comment), so this was the only remaining check -- and it wasn't
// actually checking authentication at all, just that the host answered HTTP.
//
// This server mimics exactly that split: /api/healthz always succeeds
// (unauthenticated, like the real agent), /api/device/info 401s (like the
// real agent does for a bad HMAC signature). Before the fix,
// verifyActiveConnectionWithContext would have reported success against
// this server; after it, GetDeviceInfoWithContext surfaces the 401.
func TestVerifyActiveConnectionRejectsWrongMasterKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/healthz":
			w.WriteHeader(http.StatusOK) // public, no auth check -- matches the real agent
		case "/api/device/info":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorized: Invalid signature"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mw := &MainWindow{usbClient: newTestUSBClient(t, server)}

	if err := mw.verifyActiveConnectionWithContext(context.Background()); err == nil {
		t.Fatal("verifyActiveConnectionWithContext succeeded against a server that 401s /api/device/info -- a wrong master key would falsely look connected")
	} else if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected the 401 to surface in the error, got: %v", err)
	}
}

// TestVerifyActiveConnectionAcceptsCorrectMasterKey is the flip side: a
// server where the authenticated endpoint actually succeeds must not be
// rejected -- guards against an overcorrection that breaks normal connects.
func TestVerifyActiveConnectionAcceptsCorrectMasterKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/healthz":
			w.WriteHeader(http.StatusOK)
		case "/api/device/info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mw := &MainWindow{usbClient: newTestUSBClient(t, server)}

	if err := mw.verifyActiveConnectionWithContext(context.Background()); err != nil {
		t.Fatalf("verifyActiveConnectionWithContext failed against a server that accepts /api/device/info: %v", err)
	}
}

// TestConnectionRecoveryRetryDelaysCoverRealisticTsnetReconnect guards
// against connectionRecoveryRetryDelays shrinking back down below what a
// real Tailscale/tsnet re-establishment needs after a client-side network
// path change (Wi-Fi<->cellular handoff, AP roam, DHCP renewal).
//
// Field logs (Android, tsnet client) show netcheck probing every DERP
// region plus a WireGuard re-handshake taking 15-30s after such a blip. The
// original {1,2,5}s schedule (~8s total) gave up mid-reconnect on every
// such hiccup, surfacing it to the user as a full "connection lost" dialog
// instead of riding it out -- see main_window_connection.go's
// tryRecoverConnectionAfterLoss.
func TestConnectionRecoveryRetryDelaysCoverRealisticTsnetReconnect(t *testing.T) {
	delays := connectionRecoveryRetryDelays

	if len(delays) < 4 {
		t.Fatalf("expected at least 4 recovery attempts, got %d (%v) -- too few attempts gives up before tsnet reconnects", len(delays), delays)
	}

	var total time.Duration
	for i, d := range delays {
		if d <= 0 {
			t.Fatalf("delay[%d] = %v, must be positive", i, d)
		}
		if i > 0 && d < delays[i-1] {
			t.Fatalf("delay[%d] = %v is shorter than delay[%d] = %v; schedule should back off, not speed up", i, d, i-1, delays[i-1])
		}
		total += d
	}

	const minBudget = 30 * time.Second
	if total < minBudget {
		t.Fatalf("total recovery budget = %v, want at least %v to outlast a real tsnet reconnect after a network path change (observed 15-30s in the field)", total, minBudget)
	}
}
