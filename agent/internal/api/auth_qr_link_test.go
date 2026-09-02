package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAuthQRLinkLoopbackOnly is the regression test for the master-key
// disclosure bug: /api/auth/qr/link used to answer any caller that could
// reach the process's HTTP port, which by default (ListenHost: "0.0.0.0")
// meant anyone on the LAN, or anyone on the Tailscale tailnet (the same
// handler is also served over tsnet — see app.go), could retrieve the
// master key — the sole credential for every other endpoint — with an
// unauthenticated GET. AuthQRLink must now refuse any request that didn't
// arrive over loopback, and any request carrying an Origin header (which
// only browser JS sets), regardless of what interface the *http.Server
// itself is bound to.
func TestAuthQRLinkLoopbackOnly(t *testing.T) {
	srv := NewServerWithAuth(&stubApp{}, []byte("test-master-key"), 0)
	handler := srv.Routes()

	cases := []struct {
		name       string
		remoteAddr string
		origin     string
		wantStatus int
		wantKey    bool
	}{
		{"loopback IPv4, no Origin", "127.0.0.1:54321", "", http.StatusOK, true},
		{"loopback IPv6, no Origin", "[::1]:54321", "", http.StatusOK, true},
		{"LAN address refused", "192.168.1.50:54321", "", http.StatusForbidden, false},
		{"tailnet address refused", "100.64.0.5:54321", "", http.StatusForbidden, false},
		{"loopback but browser JS (Origin set) refused", "127.0.0.1:54321", "http://evil.example", http.StatusForbidden, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/auth/qr/link", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			gotKey := strings.Contains(rec.Body.String(), "stub-master-key")
			if gotKey != tc.wantKey {
				t.Fatalf("response contains master key = %v, want %v (body: %s)", gotKey, tc.wantKey, rec.Body.String())
			}
		})
	}
}
