//go:build linux

package entitlement

// Live update-flow test against a REAL signed rust-shine release and the
// REAL installed usbridge-streamer-launch -- skipped unless
// USBRIDGE_LIVE_BUNDLE points at a release bundle (see
// cmd/usbridge_streamer_launch/launcher_test.go for how to fetch one) and
// the launcher is installed. Never prompts.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"usbridge_agent/internal/streamerlaunch"
)

func liveServer(t *testing.T, dir string, archive []byte) {
	t.Helper()
	manifest, _ := os.ReadFile(filepath.Join(dir, streamerlaunch.ManifestName))
	sig, _ := os.ReadFile(filepath.Join(dir, streamerlaunch.SigName))
	var m struct{ Version string }
	json.Unmarshal(manifest, &m)
	sum := sha256.Sum256(archive)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive" {
			w.Write(archive)
			return
		}
		json.NewEncoder(w).Encode(DownloadInfo{URL: srv.URL + "/archive", SHA256: hex.EncodeToString(sum[:]), Version: m.Version,
			Manifest: base64.StdEncoding.EncodeToString(manifest), ManifestSig: strings.TrimSpace(string(sig))})
	}))
	t.Cleanup(srv.Close)
	prev := TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() { TestSetBackendBaseURL(prev) })
}

func installedLauncherVerify(bundle string) error {
	out, err := exec.Command(streamerlaunch.InstallPath, "--verify", bundle).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "cap=1") {
		return errors.New("launcher refused: " + strings.TrimSpace(string(out)))
	}
	return nil
}

func TestLive_UpdateGatedByInstalledLauncher(t *testing.T) {
	dir := os.Getenv("USBRIDGE_LIVE_BUNDLE")
	if dir == "" {
		t.Skip("set USBRIDGE_LIVE_BUNDLE")
	}
	if err := streamerlaunch.CheckRootOwned(streamerlaunch.InstallPath); err != nil {
		t.Skipf("no installed launcher: %v", err)
	}
	real, err := os.ReadFile(filepath.Join(dir, streamerlaunch.ArchiveName))
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	liveServer(t, dir, real)
	if err := StageRustShineVerified(context.Background(), stateDir, "tok", nil, installedLauncherVerify); err != nil {
		t.Fatalf("real signed release refused: %v", err)
	}
	before, _ := os.ReadFile(StagePath(stateDir))

	// A compromised backend vouching (via the download SHA-256) for an
	// archive the signed manifest doesn't cover.
	evil := append([]byte{}, real...)
	evil[len(evil)-1] ^= 0xff
	liveServer(t, dir, evil)
	if err := StageRustShineVerified(context.Background(), stateDir, "tok", nil, installedLauncherVerify); err == nil {
		t.Fatal("tampered update applied")
	}
	after, _ := os.ReadFile(StagePath(stateDir))
	if string(before) != string(after) || !BundlePresent(stateDir) {
		t.Fatal("tampered update changed the staged build")
	}
}
