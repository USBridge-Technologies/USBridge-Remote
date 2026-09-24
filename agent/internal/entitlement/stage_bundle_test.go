//go:build linux

package entitlement

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"usbridge_agent/internal/streamerlaunch"
)

// fakeRelease serves /v1/download/rustshine (with a manifest signed by a
// throwaway key that bundlePublicKey is pointed at) plus the archive.
func fakeRelease(t *testing.T, version string, binary []byte, withManifest bool) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	prevKey := bundlePublicKey
	bundlePublicKey = func() ed25519.PublicKey { return pub }
	t.Cleanup(func() { bundlePublicKey = prevKey })

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "usbridge-streamer/usbridge-streamer", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg})
	tw.Write(binary)
	tw.Close()
	gz.Close()
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	hexSum := hex.EncodeToString(sum[:])
	manifest, _ := json.Marshal(map[string]any{
		"app": "usbridge-streamer", "version": version,
		"platforms": map[string]any{"linux-x86_64": map[string]string{"asset": "usbridge-streamer-linux-x86_64.tar.gz", "sha256": hexSum}},
	})

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive" {
			w.Write(archive)
			return
		}
		info := DownloadInfo{URL: srv.URL + "/archive", SHA256: hexSum, Version: version}
		if withManifest {
			info.Manifest = base64.StdEncoding.EncodeToString(manifest)
			info.ManifestSig = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, manifest))
		}
		json.NewEncoder(w).Encode(info)
	}))
	t.Cleanup(srv.Close)
	prev := TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() { TestSetBackendBaseURL(prev) })
}

func readStaged(t *testing.T, stateDir string) string {
	t.Helper()
	b, err := os.ReadFile(StagePath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStageRustShineVerified_KeepsSignedBundle(t *testing.T) {
	stateDir := t.TempDir()
	fakeRelease(t, "usbridge-streamer-v1.0.0", []byte("v1"), true)
	var sawBundle string
	err := StageRustShineVerified(context.Background(), stateDir, "tok", nil, func(bundleDir string) error {
		sawBundle = bundleDir
		// Called before commit: the live dir must not have the new build yet.
		if _, err := os.Stat(StagePath(stateDir)); err == nil {
			t.Error("binary already live before verify ran")
		}
		_, err := streamerlaunch.LoadVerified(bundleDir, "linux-x86_64", bundlePublicKey())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawBundle == "" || filepath.Dir(sawBundle) == filepath.Dir(StagePath(stateDir)) {
		t.Fatalf("verify saw %q, want the .next staging bundle", sawBundle)
	}
	if got := readStaged(t, stateDir); got != "v1" {
		t.Fatalf("staged %q", got)
	}
	if !BundlePresent(stateDir) {
		t.Fatal("bundle not kept")
	}
	if StagedVersion(stateDir) != "usbridge-streamer-v1.0.0" {
		t.Fatal("VERSION not written")
	}
	if _, err := os.Stat(filepath.Dir(StagePath(stateDir)) + ".next"); !os.IsNotExist(err) {
		t.Fatal(".next staging dir left behind")
	}
}

func TestStageRustShineVerified_RejectionKeepsCurrentBuild(t *testing.T) {
	stateDir := t.TempDir()
	fakeRelease(t, "usbridge-streamer-v1.0.0", []byte("v1"), true)
	if err := StageRustShine(context.Background(), stateDir, "tok", nil); err != nil {
		t.Fatal(err)
	}
	fakeRelease(t, "usbridge-streamer-v2.0.0", []byte("v2"), true)
	err := StageRustShineVerified(context.Background(), stateDir, "tok", nil, func(string) error {
		return errors.New("launcher says no")
	})
	if err == nil {
		t.Fatal("rejected update reported success")
	}
	if got := readStaged(t, stateDir); got != "v1" {
		t.Fatalf("rejected update replaced the binary: %q", got)
	}
	if StagedVersion(stateDir) != "usbridge-streamer-v1.0.0" {
		t.Fatal("rejected update changed VERSION")
	}
	if !BundlePresent(stateDir) {
		t.Fatal("rejected update dropped the old bundle")
	}
}

func TestStageRustShine_OldBackendWithoutManifest(t *testing.T) {
	// No launcher installed (verify == nil): stage as before.
	stateDir := t.TempDir()
	fakeRelease(t, "usbridge-streamer-v1.0.0", []byte("v1"), false)
	if err := StageRustShine(context.Background(), stateDir, "tok", nil); err != nil {
		t.Fatal(err)
	}
	if readStaged(t, stateDir) != "v1" || BundlePresent(stateDir) {
		t.Fatal("unexpected staged state")
	}

	// Launcher installed: an update it could never verify is refused.
	fakeRelease(t, "usbridge-streamer-v2.0.0", []byte("v2"), false)
	err := StageRustShineVerified(context.Background(), stateDir, "tok", nil, func(string) error { return nil })
	if err == nil {
		t.Fatal("unverifiable update applied while the launcher is installed")
	}
	if readStaged(t, stateDir) != "v1" {
		t.Fatal("refused update replaced the binary")
	}
}

func TestCheckRustShineUpdate_RestagesOnceForMissingBundle(t *testing.T) {
	stateDir := t.TempDir()
	fakeRelease(t, "usbridge-streamer-v1.0.0", []byte("v1"), false)
	if err := StageRustShine(context.Background(), stateDir, "tok", nil); err != nil {
		t.Fatal(err)
	}
	// Same version, backend now passes the manifest through.
	fakeRelease(t, "usbridge-streamer-v1.0.0", []byte("v1"), true)
	need, _, err := CheckRustShineUpdate(context.Background(), stateDir, "tok")
	if err != nil || !need {
		t.Fatalf("need=%v err=%v, want a one-time re-stage", need, err)
	}
	if err := StageRustShine(context.Background(), stateDir, "tok", nil); err != nil {
		t.Fatal(err)
	}
	need, _, err = CheckRustShineUpdate(context.Background(), stateDir, "tok")
	if err != nil || need {
		t.Fatalf("need=%v err=%v after re-stage, want false", need, err)
	}
}
