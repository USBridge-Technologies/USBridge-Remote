package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/entitlement"
)

// withBackendURL points the entitlement package's backend base URL at url
// for the duration of the test, restoring the real one after -- see
// pubkey.go's doc comment on why that's a var and not a const.
func withBackendURL(t *testing.T, url string) {
	t.Helper()
	orig := entitlement.TestSetBackendBaseURL(url)
	t.Cleanup(func() { entitlement.TestSetBackendBaseURL(orig) })
}

// newTestApp builds a minimal *App directly (not via New(), which starts
// real OS-level services -- sockets, tailscale, capture) with just enough
// wired up for recheckEntitlement/downgradeToSunshine to run safely:
// streamKind stays "" (never "rustshine"), so downgradeToSunshine never
// touches a.stream, and cfgPath/StateDir point at a scratch temp dir so
// SaveConfig/WriteTokenFile have somewhere real to write.
func newTestApp(t *testing.T, refreshToken, entitlementToken string) *App {
	t.Helper()
	dir := t.TempDir()
	return &App{
		cfgPath: filepath.Join(dir, "config.yaml"),
		cfg: config.Config{
			StateDir:             dir,
			ProviderRefreshToken: refreshToken,
			EntitlementToken:     entitlementToken,
		},
	}
}

// TestRecheckEntitlement_NotEntitled_DowngradesToSunshine is the direct
// proof that a lapsed Patreon membership (backend says not_entitled) turns
// RustShine access off -- the actual "does cancelling the subscription
// really disable it" behavior, exercised against a fake backend instead of
// a real Patreon account and a real wait.
func TestRecheckEntitlement_NotEntitled_DowngradesToSunshine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "not_entitled", "reason": "declined_patron"})
	}))
	defer srv.Close()
	withBackendURL(t, srv.URL)

	a := newTestApp(t, "some-refresh-token", "usbent1.doesnt.matter")
	a.recheckEntitlement(context.Background())

	if a.cfg.EntitlementToken != "" || a.cfg.ProviderRefreshToken != "" {
		t.Fatalf("expected downgradeToSunshine to clear cached tokens, got EntitlementToken=%q ProviderRefreshToken=%q",
			a.cfg.EntitlementToken, a.cfg.ProviderRefreshToken)
	}
	if a.entStatus.Linked {
		t.Error("expected entStatus.Linked=false after a not_entitled backend response")
	}
}

// TestRecheckEntitlement_BackendUnreachable_FallsBackToLocalExpiry proves
// the offline-grace bound is actually enforced continuously, not just at
// the next restart: if the backend can't be reached AND the cached token
// is already invalid (past its own exp, in production -- a malformed
// placeholder is equivalent here since the fallback only checks that
// entitlement.Verify errors, not why; the *why* is covered by
// token_test.go's real-signature expiry tests), recheckEntitlement must
// still downgrade using nothing but the locally cached token.
func TestRecheckEntitlement_BackendUnreachable_FallsBackToLocalExpiry(t *testing.T) {
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachable.Close() // closed before use: connections to it refuse immediately, no timeout to wait out
	withBackendURL(t, unreachable.URL)

	a := newTestApp(t, "some-refresh-token", "usbent1.not-a-real-token.at-all")
	a.recheckEntitlement(context.Background())

	if a.cfg.EntitlementToken != "" || a.cfg.ProviderRefreshToken != "" {
		t.Fatalf("expected the local-expiry fallback to downgrade despite the backend being unreachable, got EntitlementToken=%q ProviderRefreshToken=%q",
			a.cfg.EntitlementToken, a.cfg.ProviderRefreshToken)
	}
}

// TestRecheckEntitlement_NoRefreshToken_IsANoOp confirms recheckEntitlement
// doesn't downgrade (or do anything) for an app that was never linked --
// e.g. a fresh install that's still on the free Sunshine tier.
func TestRecheckEntitlement_NoRefreshToken_IsANoOp(t *testing.T) {
	a := newTestApp(t, "", "")
	a.recheckEntitlement(context.Background())
	if a.cfg.EntitlementToken != "" || a.entStatus.Linked {
		t.Error("recheckEntitlement should be a no-op with no saved provider refresh token")
	}
}

// Explicitly out of scope here (same as token_test.go's own stated
// limitation): a "backend returns a fresh valid token, agent applies it"
// integration test, since a genuine acceptable signature needs the real
// backend private key, which by design never exists in this repo. That
// path was verified once, manually, against the live deployed backend
// during development (a real token accepted identically by this package's
// Verify and rust-shine's license::verify_token) -- see git history around
// the entitlement feature's introduction.
