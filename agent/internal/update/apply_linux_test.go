//go:build linux && !android

package update

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInstallTargetPathPrefersAPPIMAGE is the regression test for the
// "download succeeds, then apply fails with a read-only filesystem error"
// bug: a running AppImage's os.Executable() resolves through /proc/self/exe
// to its own read-only FUSE squashfs mount under /tmp/.mount_<random>/, so
// installTargetPath must prefer $APPIMAGE (the real, writable .AppImage
// file) whenever it's set, exactly like autostart.LaunchTarget already does
// for the autostart-registration path.
func TestInstallTargetPathPrefersAPPIMAGE(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "USBridgeAgent-Linux-x86_64-9.9.9.AppImage")
	if err := os.WriteFile(real, []byte("fake-appimage"), 0o755); err != nil {
		t.Fatalf("write fake AppImage: %v", err)
	}

	// Simulate the mount-point path a running AppImage's os.Executable()
	// would report, to make sure that's not what wins when $APPIMAGE is set.
	t.Setenv("APPIMAGE", real)

	got, err := installTargetPath()
	if err != nil {
		t.Fatalf("installTargetPath: %v", err)
	}
	if got != real {
		t.Fatalf("installTargetPath = %q, want %q (the real .AppImage, not os.Executable()'s mount path)", got, real)
	}
}

// TestInstallTargetPathFallsBackWithoutAPPIMAGE covers the non-AppImage
// case (e.g. a bare binary during development): no $APPIMAGE means fall
// back to the previous os.Executable()-based resolution, which must still
// succeed and return a non-empty, absolute path.
func TestInstallTargetPathFallsBackWithoutAPPIMAGE(t *testing.T) {
	t.Setenv("APPIMAGE", "")
	os.Unsetenv("APPIMAGE")

	got, err := installTargetPath()
	if err != nil {
		t.Fatalf("installTargetPath: %v", err)
	}
	if got == "" || !filepath.IsAbs(got) {
		t.Fatalf("installTargetPath = %q, want a non-empty absolute path", got)
	}
}

// TestStageInDirSameFilesystemRename is the other half of the fix: once
// installTargetPath points at a writable directory, stageInDir must
// actually be able to stage a same-filesystem temp file there ready for the
// atomic rename apply() does next, and the staged bytes must match the
// source exactly.
func TestStageInDirSameFilesystemRename(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "downloaded.AppImage")
	want := []byte("new-version-bytes")
	if err := os.WriteFile(srcPath, want, 0o644); err != nil {
		t.Fatalf("write source artifact: %v", err)
	}

	staged, err := stageInDir(dir, srcPath)
	if err != nil {
		t.Fatalf("stageInDir: %v", err)
	}
	defer os.Remove(staged)

	if filepath.Dir(staged) != dir {
		t.Fatalf("staged file %q not in install dir %q (rename into place would cross filesystems)", staged, dir)
	}
	got, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("read staged file: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("staged content = %q, want %q", got, want)
	}

	// The atomic rename apply() performs after staging must succeed same-
	// filesystem, exactly like it will over the running AppImage.
	final := filepath.Join(dir, "USBridgeAgent.AppImage")
	if err := os.Rename(staged, final); err != nil {
		t.Fatalf("rename staged file into place: %v", err)
	}
	got, err = os.ReadFile(final)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("final content = %q, want %q", got, want)
	}
}

// TestStageInDirFailsOnReadOnlyDir is the negative case this whole fix
// exists to avoid hitting for real: staging into a directory with no write
// permission (standing in for the AppImage's own read-only FUSE mount when
// $APPIMAGE isn't honored) must fail cleanly, not silently corrupt anything.
func TestStageInDirFailsOnReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions don't block writes")
	}

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "downloaded.AppImage")
	if err := os.WriteFile(srcPath, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("write source artifact: %v", err)
	}

	roDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(roDir, 0o555); err != nil {
		t.Fatalf("mkdir read-only dir: %v", err)
	}
	defer os.Chmod(roDir, 0o755) // let t.TempDir() clean it up

	if _, err := stageInDir(roDir, srcPath); err == nil {
		t.Fatal("stageInDir into a read-only directory: want error, got nil")
	}
}
