//go:build linux && !android

package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// installTargetPath returns the file to overwrite and re-exec: the real
// on-disk .AppImage, never the path os.Executable() reports while running
// from one.
//
// A running AppImage self-mounts its squashfs payload via FUSE under
// /tmp/.mount_<random>/ and re-execs itself from inside that mountpoint —
// so /proc/self/exe (what os.Executable() reads) resolves to something like
// /tmp/.mount_USBridjHDnKB/usr/bin/usbridge-client. That path is on a
// read-only FUSE filesystem no matter who owns it or what the containing
// .AppImage file's permissions are, so staging a new binary there always
// fails with a read-only-filesystem error (and, before apply() surfaced
// that error anywhere the GUI could show it, just silently did nothing
// after the download finished) — even though the real .AppImage (e.g.
// ~/Desktop/USBridgeClient-*.AppImage) is perfectly writable. Every
// AppImage runtime sets $APPIMAGE to that real file's absolute path before
// exec'ing in, so prefer it whenever present.
func installTargetPath() (string, error) {
	if p := os.Getenv("APPIMAGE"); p != "" {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return resolved, nil
		}
		return p, nil
	}
	// Not running from an AppImage (e.g. a bare binary during development) —
	// fall back to the previous behavior.
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	return exePath, nil
}

// apply replaces the running AppImage in place and re-execs it.
//
// artifactPath is a single downloaded, already SHA-256-verified AppImage
// file (Linux AppImages are one self-contained ELF, unlike the Windows
// zip/macOS dmg cases — no archive to unpack). The replace is a same-
// directory os.Rename, which is atomic and safe even though the file being
// replaced is the currently-executing binary: Linux only unlinks the old
// directory entry, the kernel keeps the already-running process's own open
// inode alive until it exits, so nothing about the swap can corrupt this
// process mid-flight.
func apply(ctx context.Context, artifactPath, version string) error {
	defer os.Remove(artifactPath)

	exePath, err := installTargetPath()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	installDir := filepath.Dir(exePath)

	staged, err := stageInDir(installDir, artifactPath)
	if err != nil {
		// Most common cause: installDir isn't writable by this user (e.g.
		// the AppImage was placed under a root-owned system path). There's
		// no Linux equivalent of a UAC prompt to fall back on here — log
		// and leave the current install untouched.
		return fmt.Errorf("stage new binary into %s (no write permission?): %w", installDir, err)
	}
	if err := os.Chmod(staged, 0o755); err != nil {
		os.Remove(staged)
		return fmt.Errorf("chmod new binary: %w", err)
	}
	if err := os.Rename(staged, exePath); err != nil {
		os.Remove(staged)
		return fmt.Errorf("replace running binary: %w", err)
	}

	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	// New session so the relaunched process survives this one exiting
	// (e.g. if the parent was launched from a terminal that sends SIGHUP
	// to its process group on exit).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("relaunch updated binary: %w", err)
	}

	os.Exit(0)
	return nil // unreachable
}

// stageInDir copies srcPath's bytes into a new temp file inside dir, so the
// subsequent rename-into-place is a same-filesystem (atomic) operation
// rather than risking a cross-device rename failure against the system
// temp directory the download landed in.
func stageInDir(dir, srcPath string) (string, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	dst, err := os.CreateTemp(dir, ".usbridge-update-*")
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(dst.Name())
		return "", err
	}
	if err := dst.Sync(); err != nil {
		os.Remove(dst.Name())
		return "", err
	}
	return dst.Name(), nil
}
