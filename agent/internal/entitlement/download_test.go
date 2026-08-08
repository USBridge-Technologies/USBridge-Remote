package entitlement

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// downloadInfoServer starts an httptest.Server that answers
// /v1/download/rustshine with a fixed DownloadInfo JSON body, and points
// backendBaseURL at it for the duration of the test (see
// TestSetBackendBaseURL's doc comment).
func downloadInfoServer(t *testing.T, info DownloadInfo) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(info)
	}))
	t.Cleanup(srv.Close)
	prev := TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() { TestSetBackendBaseURL(prev) })
}

func TestStagedVersion_EmptyWhenNeverStaged(t *testing.T) {
	stateDir := t.TempDir()
	if got := StagedVersion(stateDir); got != "" {
		t.Fatalf("StagedVersion on a fresh stateDir = %q, want \"\"", got)
	}
}

func TestStagedVersion_RoundTripsWhatWasWritten(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(StagePath(stateDir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedVersionPath(stateDir), []byte("gamestream-server-v0.2.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := StagedVersion(stateDir), "gamestream-server-v0.2.2"; got != want {
		t.Fatalf("StagedVersion = %q, want %q", got, want)
	}
}

func TestCheckRustShineUpdate_NoOpWhenNeverStaged(t *testing.T) {
	stateDir := t.TempDir()
	downloadInfoServer(t, DownloadInfo{Version: "gamestream-server-v0.2.2"})

	needsUpdate, version, err := CheckRustShineUpdate(context.Background(), stateDir, "tok")
	if err != nil {
		t.Fatalf("CheckRustShineUpdate: %v", err)
	}
	if needsUpdate {
		t.Errorf("needsUpdate = true, want false (nothing staged yet -- that's the explicit download button's job)")
	}
	if version != "" {
		t.Errorf("version = %q, want \"\" when not staged", version)
	}
}

// stageFakeBinary drops a placeholder file at StagePath(stateDir), as if a
// prior StageRustShine had run, and records stagedVersion as what it staged
// (mirroring what StageRustShine itself writes after a real download).
func stageFakeBinary(t *testing.T, stateDir, stagedVersion string) {
	t.Helper()
	dest := StagePath(stateDir)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("fake binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if stagedVersion != "" {
		if err := os.WriteFile(stagedVersionPath(stateDir), []byte(stagedVersion), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheckRustShineUpdate_NoUpdateWhenVersionsMatch(t *testing.T) {
	stateDir := t.TempDir()
	stageFakeBinary(t, stateDir, "gamestream-server-v0.2.2")
	downloadInfoServer(t, DownloadInfo{Version: "gamestream-server-v0.2.2"})

	needsUpdate, _, err := CheckRustShineUpdate(context.Background(), stateDir, "tok")
	if err != nil {
		t.Fatalf("CheckRustShineUpdate: %v", err)
	}
	if needsUpdate {
		t.Errorf("needsUpdate = true, want false when the staged and latest versions are identical")
	}
}

// TestCheckRustShineUpdate_UpdateWhenNewerVersionAvailable is the exact
// scenario this whole mechanism exists for: an agent staged RustShine
// before a fix (e.g. the NV_KEYBOARD_PACKET modifiers-byte fix) shipped in
// a later release, and needs to notice that a newer build now exists.
func TestCheckRustShineUpdate_UpdateWhenNewerVersionAvailable(t *testing.T) {
	stateDir := t.TempDir()
	stageFakeBinary(t, stateDir, "gamestream-server-v0.2.1")
	downloadInfoServer(t, DownloadInfo{Version: "gamestream-server-v0.2.2"})

	needsUpdate, version, err := CheckRustShineUpdate(context.Background(), stateDir, "tok")
	if err != nil {
		t.Fatalf("CheckRustShineUpdate: %v", err)
	}
	if !needsUpdate {
		t.Errorf("needsUpdate = false, want true when a newer version is published")
	}
	if version != "gamestream-server-v0.2.2" {
		t.Errorf("version = %q, want %q", version, "gamestream-server-v0.2.2")
	}
}

// TestCheckRustShineUpdate_UpdateWhenStagedBeforeVersionTracking covers a
// binary staged by an older agent build that predates StagedVersion
// existing at all: StagedVersion reads "" in that case, which must be
// treated as "needs an update" (to be safe) rather than silently matching
// forever.
func TestCheckRustShineUpdate_UpdateWhenStagedBeforeVersionTracking(t *testing.T) {
	stateDir := t.TempDir()
	stageFakeBinary(t, stateDir, "") // no VERSION marker written at all
	downloadInfoServer(t, DownloadInfo{Version: "gamestream-server-v0.2.2"})

	needsUpdate, _, err := CheckRustShineUpdate(context.Background(), stateDir, "tok")
	if err != nil {
		t.Fatalf("CheckRustShineUpdate: %v", err)
	}
	if !needsUpdate {
		t.Errorf("needsUpdate = false, want true when the staged binary has no recorded version at all")
	}
}

func TestCheckRustShineUpdate_DegradedBackendVersionIsNotForcedAsUpdate(t *testing.T) {
	stateDir := t.TempDir()
	stageFakeBinary(t, stateDir, "gamestream-server-v0.2.2")
	downloadInfoServer(t, DownloadInfo{Version: ""}) // backend couldn't resolve a version

	needsUpdate, _, err := CheckRustShineUpdate(context.Background(), stateDir, "tok")
	if err != nil {
		t.Fatalf("CheckRustShineUpdate: %v", err)
	}
	if needsUpdate {
		t.Errorf("needsUpdate = true, want false when the backend reports no version at all")
	}
}
