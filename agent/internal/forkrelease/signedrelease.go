package forkrelease

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// On Linux the agent keeps, next to a downloaded Sunshine or Vibepollo tree,
// the release exactly as it was verified: manifest.json, manifest.json.sig
// and the archive. The KMS launcher (usbridge-streamer-launch --install-tree,
// run as root through a passwordless polkit rule) checks those itself and
// puts the archive's contents in its root-owned tree -- so an update or a
// switch to the other fork needs no new password. It never trusts the
// unpacked user-writable tree, only the signed archive bytes.

// Names inside ReleaseDir; streamerlaunch reads the same ones.
const (
	ReleaseManifestName = "manifest.json"
	ReleaseSigName      = "manifest.json.sig"
)

// ReleaseDir is where treeDir's signed release is kept.
func ReleaseDir(treeDir string) string { return treeDir + ".release" }

// keepsSignedRelease: only the Linux trees go through the launcher.
func keepsSignedRelease() bool { return runtime.GOOS == "linux" }

// stageSignedRelease writes the verified manifest, its signature and the
// archive (copied from the download's temp file) into dir.
func stageSignedRelease(dir string, sm signedManifest, asset, archive string) error {
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, ReleaseManifestName), sm.body, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, ReleaseSigName), sm.sig, 0o644); err != nil {
		return err
	}
	in, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := writeFile(filepath.Join(dir, asset), in, 0o644); err != nil {
		return fmt.Errorf("keep %s: %w", asset, err)
	}
	return nil
}

// commitSignedRelease puts a staged release dir (next) in place of live's.
// Without one (an older download) live's is dropped, so a stale release
// never stands in for the new tree.
func commitSignedRelease(next, treeDir string) {
	live := ReleaseDir(treeDir)
	_ = os.RemoveAll(live)
	if next == "" {
		return
	}
	if err := renameRetry(next, live); err != nil {
		_ = os.RemoveAll(next)
	}
}

// discardSignedRelease drops a staged release dir.
func discardSignedRelease(next string) {
	if next != "" {
		_ = os.RemoveAll(next)
	}
}
