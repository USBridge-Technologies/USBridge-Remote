package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

// fakeRustShineArchive builds a valid .tar.gz containing a single entry
// named base with contents, plus its SHA-256 -- everything
// StageRustShine's real download+verify+extract path needs, without a real
// GitHub release or backend involved. Matches Linux's asset shape
// (extractFromTarGz); this suite only ever runs on Linux/darwin CI, never
// windows, so the .zip path (extractFromZip) isn't exercised here.
func fakeRustShineArchive(t *testing.T, base string, contents []byte) (archive []byte, sha256Hex string) {
	t.Helper()
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: base, Mode: 0o755, Size: int64(len(contents))}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(contents); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	sum := sha256.Sum256(tarBuf.Bytes())
	return tarBuf.Bytes(), hex.EncodeToString(sum[:])
}

// TestEnsureRustShineFresh_NeverStaged_DownloadsImmediately is the direct
// proof of the "no manual Download RustShine click required" behavior: a
// freshly linked supporter (entitled, nothing staged yet) gets RustShine
// downloaded the moment ensureRustShineFresh runs -- exactly what
// applyLinkSuccess now fires in the background right after a link
// succeeds, and what recheckEntitlement fires afterward on every 6h tick
// for as long as nothing's staged yet (e.g. an earlier attempt failed).
func TestEnsureRustShineFresh_NeverStaged_DownloadsImmediately(t *testing.T) {
	a := newTestApp(t, "", "usbent1.doesnt.matter")
	base := filepath.Base(entitlement.StagePath(a.cfg.StateDir))
	archive, sum := fakeRustShineArchive(t, base, []byte("fake gamestream-server binary"))

	// srv declared before assignment so the /v1/download/rustshine handler
	// below can close over it to build an absolute download_url (the real
	// backend always returns an absolute, short-lived signed URL --
	// downloadArchive does a plain http.NewRequestWithContext on whatever
	// URL it's given, so a relative one would fail with "unsupported
	// protocol scheme"). Safe: the handler only reads srv.URL once a
	// request actually arrives, always after srv is assigned below.
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/download/rustshine", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entitlement.DownloadInfo{
			URL:     srv.URL + "/archive.tar.gz",
			SHA256:  sum,
			Version: "gamestream-server-v9.9.9-test",
		})
	})
	mux.HandleFunc("/archive.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	withBackendURL(t, srv.URL)

	if a.rustshineStaged() {
		t.Fatal("test setup bug: rustshine already staged before ensureRustShineFresh ran")
	}
	a.ensureRustShineFresh(context.Background(), a.cfg.EntitlementToken)

	if !a.rustshineStaged() {
		t.Fatal("expected ensureRustShineFresh to download and stage RustShine when nothing was staged yet")
	}
	if !a.entStatus.RustShineStaged {
		t.Error("expected entStatus.RustShineStaged to reflect the new download")
	}
	got, err := os.ReadFile(entitlement.StagePath(a.cfg.StateDir))
	if err != nil {
		t.Fatalf("read staged binary: %v", err)
	}
	if string(got) != "fake gamestream-server binary" {
		t.Errorf("staged binary contents = %q, want the fake archive's payload", got)
	}
}

// TestEnsureRustShineFresh_NoToken_NeverDownloads is the direct proof of
// the other half of "без подписки — только Sunshine": with no entitlement
// token at all (never linked), ensureRustShineFresh must not make any
// network call or touch disk -- pointing withBackendURL at nothing
// reachable would otherwise make that a network-error masking a real bug,
// so this asserts on rustshineStaged() actually staying false rather than
// merely "didn't panic."
func TestEnsureRustShineFresh_NoToken_NeverDownloads(t *testing.T) {
	a := newTestApp(t, "", "")
	a.ensureRustShineFresh(context.Background(), "")
	if a.rustshineStaged() {
		t.Fatal("ensureRustShineFresh staged RustShine with no entitlement token -- should be impossible")
	}
}
