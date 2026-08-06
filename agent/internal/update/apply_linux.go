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

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
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
