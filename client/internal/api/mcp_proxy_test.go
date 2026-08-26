package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestMCPProxyRejectsOrigin is the regression test for the CSRF-style
// drive-by: MCPProxy.handle forwards whatever it's given to the device
// already HMAC-signed with the paired master key (see PostRaw), so any
// caller that can reach it gets an authenticated device call for free. It
// used to also answer with Access-Control-Allow-Origin: * on top of that,
// so a page open in the user's own browser could POST here via fetch() and
// have its payload silently signed and relayed — no need to know the master
// key at all. The proxy is documented as being for local, native AI tools,
// which never set an Origin header (only browser JS does), so refusing any
// request that carries one closes the hole without affecting the real
// use case.
func TestMCPProxyRejectsOrigin(t *testing.T) {
	// Fake "device" upstream that would prove a request got all the way
	// through if the Origin check didn't stop it first.
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	host, portStr, err := splitHostPortHelper(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse upstream port: %v", err)
	}

	client := NewUSBClient(host, port, 5)

	// MCPProxy.Port() just echoes back whatever port Start was given (see
	// mcp_proxy.go) rather than resolving the OS-assigned port from a ":0"
	// listener, so pick a fixed high port for the test instead of relying
	// on ephemeral-port auto-assignment.
	const testPort = 18765
	proxy := &MCPProxy{}
	if err := proxy.Start(testPort, client); err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	defer proxy.Stop()

	proxyURL := "http://127.0.0.1:" + strconv.Itoa(testPort) + "/api/mcp"

	t.Run("browser-style request (Origin set) is refused before reaching the device", func(t *testing.T) {
		upstreamCalled = false
		req, _ := http.NewRequest(http.MethodPost, proxyURL, strings.NewReader(`{"tool":"do_something"}`))
		req.Header.Set("Origin", "http://evil.example")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
		}
		if upstreamCalled {
			t.Fatal("request with an Origin header reached the device — CSRF hole is still open")
		}
	})

	t.Run("native client request (no Origin) is forwarded normally", func(t *testing.T) {
		upstreamCalled = false
		req, _ := http.NewRequest(http.MethodPost, proxyURL, strings.NewReader(`{"tool":"do_something"}`))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if !upstreamCalled {
			t.Fatal("a legitimate native-tool request (no Origin) never reached the device")
		}
	})
}

// splitHostPortHelper pulls "host" and "port" out of an httptest.Server URL
// (e.g. "http://127.0.0.1:54321") for use with NewUSBClient(host, port, ...).
func splitHostPortHelper(rawURL string) (string, string, error) {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(rawURL, "http://"), "https://")
	idx := strings.LastIndex(trimmed, ":")
	if idx < 0 {
		return trimmed, "80", nil
	}
	return trimmed[:idx], trimmed[idx+1:], nil
}
