//go:build linux && !android

package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"usbridge_agent/internal/autostart"
)

// installTargetPath returns the file to overwrite and re-exec: the real
// on-disk .AppImage, never the path os.Executable() reports while running
// from one.
//
// A running AppImage self-mounts its squashfs payload via FUSE under
// /tmp/.mount_<random>/ and re-execs itself from inside that mountpoint —
// so /proc/self/exe (what os.Executable() reads) resolves to something like
// /tmp/.mount_USBridjHDnKB/usr/bin/usbridge-agent. That path is on a
// read-only FUSE filesystem no matter who owns it or what the containing
// .AppImage file's permissions are, so staging a new binary there always
// fails with a read-only-filesystem error — even though the real
// .AppImage (e.g. ~/Desktop/USBridgeAgent-*.AppImage) is perfectly
// writable. autostart.LaunchTarget already resolves this same $APPIMAGE-vs-
// os.Executable() distinction for the autostart registration path — reuse
// it here instead of a second copy of the same trick, discarding its
// --headless args (this caller needs to preserve the current process's own
// os.Args, not force headless).
func installTargetPath() (string, error) {
	exePath, _, err := autostart.LaunchTarget()
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

	return relaunch(exePath)
}

// relaunch starts exePath with this process's arguments and exits.
func relaunch(exePath string) error {
	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	// New session so the relaunched process survives this one exiting
	// (e.g. if the parent was launched from a terminal that sends SIGHUP
	// to its process group on exit).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if BeforeRelaunch != nil {
		BeforeRelaunch()
	}
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
// pkgInstallDir is where the .deb (scripts/build_deb.sh), the Arch package
// (scripts/build_arch.sh) and the RPM (scripts/build_rpm.sh) install the agent; root-owned, so the
// binary can't be swapped in place.
const pkgInstallDir = "/usr/lib/usbridge-agent"

func init() {
	packageManagedApply = pkgApply
	installIsPackageManaged = func() bool { _, ok := packageManagedExe(); return ok }
}

// packageManagedExe returns this binary's resolved path and whether it sits
// in the root-owned package install dir (never for an AppImage).
func packageManagedExe() (string, bool) {
	if os.Getenv("APPIMAGE") != "" {
		return "", false
	}
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if filepath.Dir(exe) != pkgInstallDir {
		return "", false
	}
	return exe, true
}

// pkgApply updates a .deb, Arch or RPM install through its package manager:
// pkexec runs the package's own apt-upgrade, pacman-upgrade or dnf-upgrade
// script (root-owned, refreshes only the USBridge repository), then the
// upgraded binary is relaunched. The package manager verifies the package
// against the signed repository index, so nothing is downloaded here.
func pkgApply(ctx context.Context, version, tag string) (bool, error) {
	exe, ok := packageManagedExe()
	if !ok {
		return false, nil
	}

	tool := "apt"
	for _, t := range []string{"pacman", "dnf"} {
		if _, err := os.Stat(filepath.Join(pkgInstallDir, t+"-upgrade")); err == nil {
			tool = t
		}
	}
	helper := filepath.Join(pkgInstallDir, tool+"-upgrade")
	args := []string{helper}
	if GetChannel() == ChannelTest {
		// Only the apt helper can read a release's own repository; pacman
		// and dnf only follow the stable one.
		if tool != "apt" || tag == "" {
			return true, fmt.Errorf("the test channel can't update a %s install: use the AppImage or the portable build", tool)
		}
		args = append(args, tag)
	}
	out, err := exec.CommandContext(ctx, "pkexec", args...).CombinedOutput()
	if err != nil {
		return true, fmt.Errorf("%s upgrade (pkexec): %w: %s", tool, err, lastLines(out, 5))
	}
	installed, err := installedVersion(ctx, tool)
	if err != nil {
		return true, fmt.Errorf("read installed version: %w", err)
	}
	if isNewerVersion(version, installed) {
		return true, fmt.Errorf("%s still has %s, not %s yet — try again in a few minutes", tool, installed, version)
	}
	return true, relaunch(exe)
}

// installedVersion returns the installed package's upstream version,
// without the Arch pkgrel ("3.1.39" from pacman's "usbridge-agent 3.1.39-1").
func installedVersion(ctx context.Context, tool string) (string, error) {
	switch tool {
	case "dnf":
		out, err := exec.CommandContext(ctx, "rpm", "-q", "--qf", "%{VERSION}", "usbridge-agent").Output()
		return string(out), err
	case "pacman":
		out, err := exec.CommandContext(ctx, "pacman", "-Q", "usbridge-agent").Output()
		if err != nil {
			return "", err
		}
		return parsePacmanVersion(string(out))
	}
	out, err := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Version}", "usbridge-agent").Output()
	return string(out), err
}

// parsePacmanVersion turns pacman -Q's "<name> <pkgver>-<pkgrel>" into
// pkgver.
func parsePacmanVersion(out string) (string, error) {
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return "", fmt.Errorf("unexpected pacman -Q output %q", out)
	}
	v, _, _ := strings.Cut(fields[1], "-")
	return v, nil
}

func lastLines(b []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

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
