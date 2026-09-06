package api

import (
	"encoding/json"
	"io"
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

// TestMCPProxySurfacesDeviceAuthFailureAsJSON is the regression test for the
// bug where a device-side auth failure reached the MCP client as raw text
// mislabeled application/json. The device's HMAC middleware (see the device
// repo's web/security.go) replies 401 with a plain-text body like
// "Unauthorized: Invalid signature" -- e.g. after clock skew or a stale
// paired key. PostRawWithTimeout used to return that text as a normal
// (err == nil) response, and handle() wrote it straight back with
// Content-Type: application/json, so the MCP client's JSON-RPC parser choked
// on "Unexpected identifier Unauthorized" with nothing to act on. It should
// instead surface as a spec-shaped JSON-RPC error object the client can
// actually parse and log.
func TestMCPProxySurfacesDeviceAuthFailureAsJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized: Invalid signature", http.StatusUnauthorized)
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

	const testPort = 18766
	proxy := &MCPProxy{}
	if err := proxy.Start(testPort, client); err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	defer proxy.Stop()

	proxyURL := "http://127.0.0.1:" + strconv.Itoa(testPort) + "/api/mcp"
	req, _ := http.NewRequest(http.MethodPost, proxyURL, strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"initialize","params":{}}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var parsed struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		t.Fatalf("response body is not valid JSON (the bug this test guards against): %v\nbody: %s", err, bodyBytes)
	}
	if parsed.ID != 7 {
		t.Fatalf("id = %d, want 7 (echoed from the request)", parsed.ID)
	}
	if !strings.Contains(parsed.Error.Message, "Unauthorized") {
		t.Fatalf("error.message = %q, want it to mention the device's Unauthorized reason", parsed.Error.Message)
	}
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
