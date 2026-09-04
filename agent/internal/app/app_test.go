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
func newTestApp(t *testing.T, entitlementToken string) *App {
	t.Helper()
	dir := t.TempDir()
	return &App{
		cfgPath: filepath.Join(dir, "config.yaml"),
		cfg: config.Config{
			StateDir:         dir,
			EntitlementToken: entitlementToken,
		},
	}
}

// Explicitly out of scope for the tests below (same limitation as
// token_test.go's own stated one): a "cached token is genuinely
// hardware-bound-valid, backend confirms still licensed/revokes it"
// integration test, since a genuinely accepted signature needs the real
// backend private key, which by design never exists in this repo. Both
// tests here deliberately use a syntactically-token-shaped but
// unverifiable EntitlementToken -- recheckEntitlement's OWN local
// VerifyForHardware call rejects it immediately (same as it would reject
// any tampered/foreign-machine token in production), which is enough to
// exercise the "no longer valid locally -> downgrade" path both tests
// actually care about, without ever reaching the network call. That
// specific behavior (a still-locally-valid LICENSE token additionally
// getting network-re-checked, vs. a still-valid TRIAL token not making a
// network call at all) was verified by code review against
// entitlement.VerifyForHardware's own provider branching (see
// token_test.go's TestVerify_RealSignature_TrialProviderAccepted /
// WrongProviderRejected), and once, manually, against the live deployed
// backend during development.

// TestRecheckEntitlement_InvalidCachedToken_DowngradesToSunshine confirms
// recheckEntitlement downgrades to Sunshine and clears the cached token
// when what's cached no longer verifies locally (expired trial, corrupted
// value, or -- in production -- a token some other install's config.yaml
// was copied from, since it would fail the hardware-id check) -- without
// this ever needing to reach the network, this is the same underlying
// guarantee "hardware binding actually gates access" relies on.
func TestRecheckEntitlement_InvalidCachedToken_DowngradesToSunshine(t *testing.T) {
	a := newTestApp(t, "usbent1.doesnt.matter")
	a.recheckEntitlement(context.Background())

	if a.cfg.EntitlementToken != "" {
		t.Fatalf("expected downgradeToSunshine to clear the cached token, got EntitlementToken=%q", a.cfg.EntitlementToken)
	}
	if a.entStatus.Linked {
		t.Error("expected entStatus.Linked=false after an unverifiable cached token")
	}
}

// TestRecheckEntitlement_NoCachedToken_IsANoOp confirms recheckEntitlement
// doesn't downgrade (or do anything, including touching the network) for
// an app that was never trialed/purchased -- e.g. a fresh install that's
// still on the free Sunshine tier.
func TestRecheckEntitlement_NoCachedToken_IsANoOp(t *testing.T) {
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("recheckEntitlement should never make a network call with no cached token")
	}))
	unreachable.Close()
	withBackendURL(t, unreachable.URL)

	a := newTestApp(t, "")
	a.recheckEntitlement(context.Background())
	if a.cfg.EntitlementToken != "" || a.entStatus.Linked {
		t.Error("recheckEntitlement should be a no-op with no cached entitlement token")
	}
}

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
// freshly entitled customer (nothing staged yet) gets RustShine downloaded
// the moment ensureRustShineFresh runs -- exactly what applyIssuedToken now
// fires in the background right after a purchase/trial succeeds, and what
// recheckEntitlement fires afterward on every 6h tick for as long as
// nothing's staged yet (e.g. an earlier attempt failed).
func TestEnsureRustShineFresh_NeverStaged_DownloadsImmediately(t *testing.T) {
	a := newTestApp(t, "usbent1.doesnt.matter")
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
	a := newTestApp(t, "")
	a.ensureRustShineFresh(context.Background(), "")
	if a.rustshineStaged() {
		t.Fatal("ensureRustShineFresh staged RustShine with no entitlement token -- should be impossible")
	}
}

// rustShineTestServer wires up the same two-endpoint fake backend
// fakeRustShineArchive/TestEnsureRustShineFresh_NeverStaged_DownloadsImmediately
// already build ad hoc, but as a reusable helper -- serves version/sum at
// /v1/download/rustshine and archive at /archive.tar.gz, and (via
// archiveHits) lets a test assert the archive endpoint was or wasn't
// actually fetched, which is exactly what distinguishes "already up to
// date, no-op" from "downloaded again" for CheckRustShineUpdateNow.
func rustShineTestServer(t *testing.T, base, version string, contents []byte) (srv *httptest.Server, archiveHits *int) {
	t.Helper()
	archive, sum := fakeRustShineArchive(t, base, contents)
	archiveHits = new(int)
	var s *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/download/rustshine", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entitlement.DownloadInfo{URL: s.URL + "/archive.tar.gz", SHA256: sum, Version: version})
	})
	mux.HandleFunc("/archive.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		*archiveHits++
		_, _ = w.Write(archive)
	})
	s = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s, archiveHits
}

// TestCheckRustShineUpdateNow_NewerVersionAvailable_StagesIt is the direct
// proof of the GUI's "Check for updates" button's happy path: a supporter
// who already has an older RustShine staged clicks it, the backend reports
// a newer version, and the newer binary actually lands on disk -- the same
// outcome checkRustShineUpdate's silent background watchdog produces, just
// triggered on demand instead of waiting for the next interval.
func TestCheckRustShineUpdateNow_NewerVersionAvailable_StagesIt(t *testing.T) {
	a := newTestApp(t, "usbent1.doesnt.matter")
	base := filepath.Base(entitlement.StagePath(a.cfg.StateDir))

	oldSrv, _ := rustShineTestServer(t, base, "gamestream-server-v1.0.0-test", []byte("old binary"))
	withBackendURL(t, oldSrv.URL)
	a.ensureRustShineFresh(context.Background(), a.cfg.EntitlementToken)
	if v := entitlement.StagedVersion(a.cfg.StateDir); v != "gamestream-server-v1.0.0-test" {
		t.Fatalf("test setup bug: staged version = %q, want the v1.0.0 fake", v)
	}

	newSrv, archiveHits := rustShineTestServer(t, base, "gamestream-server-v2.0.0-test", []byte("new binary"))
	withBackendURL(t, newSrv.URL)

	if err := a.CheckRustShineUpdateNow(); err != nil {
		t.Fatalf("CheckRustShineUpdateNow: %v", err)
	}
	if *archiveHits != 1 {
		t.Errorf("archive endpoint hit %d times, want exactly 1", *archiveHits)
	}
	if v := entitlement.StagedVersion(a.cfg.StateDir); v != "gamestream-server-v2.0.0-test" {
		t.Errorf("staged version after update = %q, want the v2.0.0 fake", v)
	}
	got, err := os.ReadFile(entitlement.StagePath(a.cfg.StateDir))
	if err != nil {
		t.Fatalf("read staged binary: %v", err)
	}
	if string(got) != "new binary" {
		t.Errorf("staged binary contents = %q, want the new fake's payload", got)
	}
	if got := a.EntitlementStatus().RustShineVersion; got != "gamestream-server-v2.0.0-test" {
		t.Errorf("EntitlementStatus().RustShineVersion = %q, want the v2.0.0 fake", got)
	}
}

// TestCheckRustShineUpdateNow_AlreadyUpToDate_NeverRedownloads is the
// no-redundant-network-traffic half of the same button: clicking "Check for
// updates" when nothing's actually changed must not re-fetch the archive at
// all (only the cheap metadata call), and must leave the staged binary
// exactly as it was.
func TestCheckRustShineUpdateNow_AlreadyUpToDate_NeverRedownloads(t *testing.T) {
	a := newTestApp(t, "usbent1.doesnt.matter")
	base := filepath.Base(entitlement.StagePath(a.cfg.StateDir))

	srv, archiveHits := rustShineTestServer(t, base, "gamestream-server-v1.0.0-test", []byte("only binary"))
	withBackendURL(t, srv.URL)
	a.ensureRustShineFresh(context.Background(), a.cfg.EntitlementToken)
	if *archiveHits != 1 {
		t.Fatalf("test setup bug: expected exactly 1 archive fetch from the initial download, got %d", *archiveHits)
	}

	if err := a.CheckRustShineUpdateNow(); err != nil {
		t.Fatalf("CheckRustShineUpdateNow: %v", err)
	}
	if *archiveHits != 1 {
		t.Errorf("archive endpoint hit %d times after an up-to-date check, want still 1 (no redundant re-download)", *archiveHits)
	}
}

// TestCheckRustShineUpdateNow_NotEntitled_ReturnsErrorWithoutNetwork proves
// the button can't be used to bypass the entitlement gate: an app that was
// never linked (or whose token no longer verifies) gets a local error
// straight from entitlement.Verify, before any network call could leak
// whether a backend is even reachable.
func TestCheckRustShineUpdateNow_NotEntitled_ReturnsErrorWithoutNetwork(t *testing.T) {
	a := newTestApp(t, "")
	// No withBackendURL call at all -- backendBaseURL stays the real
	// deployed one, so a network call here (if the entitlement check
	// weren't actually local-only) would either hang or hit production.
	if err := a.CheckRustShineUpdateNow(); err == nil {
		t.Fatal("expected CheckRustShineUpdateNow to fail for an app with no entitlement token")
	}
	if a.rustshineStaged() {
		t.Error("CheckRustShineUpdateNow staged RustShine for an unentitled app -- should be impossible")
	}
}

// TestRestartRustShineIfActive_NeverPanicsRegardlessOfStreamState proves
// the hot-swap-in-place call restartRustShineIfActive makes after a
// successful update (see its own doc comment) can never itself crash the
// update flow -- neither the common "Sunshine is active, nothing to do"
// case, nor the degenerate "streamKind says rustshine but a.stream was
// never actually set" case a partially-initialized App (like newTestApp's)
// exercises here, which RestartSunshine's own `if a.stream == nil` guard is
// what actually protects against.
func TestRestartRustShineIfActive_NeverPanicsRegardlessOfStreamState(t *testing.T) {
	sunshineApp := newTestApp(t, "")
	sunshineApp.streamKind = "sunshine"
	sunshineApp.restartRustShineIfActive() // must return, not panic

	rustshineApp := newTestApp(t, "")
	rustshineApp.streamKind = "rustshine" // a.stream stays nil -- see doc comment above
	rustshineApp.restartRustShineIfActive() // must return, not panic
}
