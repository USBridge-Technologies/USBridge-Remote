//go:build darwin

package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// appBundleName is the on-disk .app name for this binary — used both to
// find *this* running bundle (to know where to install over) and to find
// the freshly downloaded one inside the mounted .dmg.
const appBundleName = "USBridgeClient.app"

// apply mounts the downloaded, already SHA-256-verified .dmg, independently
// re-verifies the new .app's Apple code signature (belt-and-suspenders on
// top of the Ed25519 manifest check — the same signing identity CI already
// requires for release builds, see .github/workflows/*-release.yml), swaps
// it into place next to the currently-running bundle, and relaunches it.
func apply(ctx context.Context, artifactPath, version string) error {
	defer os.Remove(artifactPath)

	mountPoint, err := os.MkdirTemp("", "usbridge-client-update-mount-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mountPoint)

	if out, err := exec.CommandContext(ctx, "hdiutil", "attach", artifactPath,
		"-nobrowse", "-readonly", "-mountpoint", mountPoint).CombinedOutput(); err != nil {
		return fmt.Errorf("hdiutil attach: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	defer exec.Command("hdiutil", "detach", mountPoint, "-quiet").Run()

	newApp := filepath.Join(mountPoint, appBundleName)
	if _, err := os.Stat(newApp); err != nil {
		return fmt.Errorf("no %s found in downloaded disk image: %w", appBundleName, err)
	}

	if err := verifyAppleCodesign(ctx, newApp); err != nil {
		return fmt.Errorf("downloaded update failed Apple code signature verification (not applying): %w", err)
	}

	installedApp, err := installTarget()
	if err != nil {
		return err
	}
	if err := swapIn(ctx, newApp, installedApp); err != nil {
		return err
	}

	if err := exec.Command("open", "-n", installedApp).Start(); err != nil {
		return fmt.Errorf("relaunch updated app: %w", err)
	}

	exitAfterHandoff(func() {
		exec.Command("hdiutil", "detach", mountPoint, "-quiet").Run()
		os.RemoveAll(mountPoint)
		os.Remove(artifactPath)
	})
	return nil // unreachable
}

// installTarget is the bundle path the update replaces: the running bundle
// when its folder is writable, else /Applications/<App>.app. The fallback
// covers an app started straight from the mounted .dmg, or a translocated
// one whose source couldn't be resolved — both read-only, so the update
// moves the app into /Applications rather than failing.
func installTarget() (string, error) {
	installedApp, err := currentAppBundlePath()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(installedApp)
	if !strings.Contains(installedApp, "/AppTranslocation/") && unix.Access(dir, unix.W_OK) == nil {
		return installedApp, nil
	}
	return filepath.Join("/Applications", appBundleName), nil
}

// swapIn copies newApp next to target and renames it over target, keeping
// the old bundle until the new one is in place. When this user can't write
// there (a standard account, a bundle another admin installed), the same
// steps run once more as root behind the system password prompt.
func swapIn(ctx context.Context, newApp, target string) error {
	dir := filepath.Dir(target)
	staged := filepath.Join(dir, ".usbridge-update-staged.app")
	backup := filepath.Join(dir, ".usbridge-update-previous.app")
	if err := swapAsUser(ctx, newApp, target, staged, backup); err == nil {
		return nil
	} else if !isPermissionError(err) {
		return err
	}
	apple := fmt.Sprintf("do shell script %s with administrator privileges",
		appleScriptString(swapScript(newApp, target, staged, backup)))
	if out, err := exec.CommandContext(ctx, "osascript", "-e", apple).CombinedOutput(); err != nil {
		return fmt.Errorf("install into %s as administrator: %w (%s)", dir, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// swapScript is swapAsUser as a shell script, for the root run.
func swapScript(newApp, target, staged, backup string) string {
	return fmt.Sprintf("set -e; rm -rf %[2]s %[3]s; ditto %[1]s %[2]s; "+
		"if [ -e %[4]s ]; then mv %[4]s %[3]s; fi; "+
		"mv %[2]s %[4]s || { [ ! -e %[3]s ] || mv %[3]s %[4]s; exit 1; }; rm -rf %[3]s",
		shellQuote(newApp), shellQuote(staged), shellQuote(backup), shellQuote(target))
}

func swapAsUser(ctx context.Context, newApp, target, staged, backup string) error {
	os.RemoveAll(staged)
	if out, err := exec.CommandContext(ctx, "ditto", newApp, staged).CombinedOutput(); err != nil {
		os.RemoveAll(staged)
		return &os.PathError{Op: "ditto", Path: staged, Err: dittoErr(out, err)}
	}
	os.RemoveAll(backup)
	hadOld := true
	if err := os.Rename(target, backup); err != nil {
		if !os.IsNotExist(err) {
			os.RemoveAll(staged)
			return fmt.Errorf("move aside current install: %w", err)
		}
		hadOld = false
	}
	if err := os.Rename(staged, target); err != nil {
		// Roll back so the app isn't left half-installed.
		if hadOld {
			os.Rename(backup, target)
		}
		os.RemoveAll(staged)
		return fmt.Errorf("install new version: %w", err)
	}
	os.RemoveAll(backup)
	return nil
}

// dittoErr keeps ditto's own message, mapping its permission and
// read-only failures to the errno isPermissionError looks for.
func dittoErr(out []byte, err error) error {
	msg := strings.TrimSpace(string(out))
	switch {
	case strings.Contains(msg, "Permission denied"), strings.Contains(msg, "Operation not permitted"):
		return fmt.Errorf("%w: %s", os.ErrPermission, msg)
	case strings.Contains(msg, "Read-only file system"):
		return fmt.Errorf("%w: %s", unix.EROFS, msg)
	}
	return fmt.Errorf("%w (%s)", err, msg)
}

func isPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, unix.EACCES) ||
		errors.Is(err, unix.EPERM) || errors.Is(err, unix.EROFS)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func appleScriptString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// currentAppBundlePath walks up from the running binary's path
// (.../USBridgeClient.app/Contents/MacOS/usbridge-client) to the .app
// bundle root, rather than assuming a fixed /Applications install location
// — the app may have been dragged anywhere.
func currentAppBundlePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve current executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	exePath = untranslocate(exePath)
	marker := "/" + appBundleName + "/"
	idx := strings.Index(exePath, marker)
	if idx < 0 {
		return "", fmt.Errorf("running binary %s is not inside %s (not installed as a normal .app bundle — e.g. running from `go run` or a bare binary), skipping self-update", exePath, appBundleName)
	}
	return exePath[:idx+len(marker)-1], nil
}

// untranslocate maps a path inside a Gatekeeper App Translocation mount
// back to the real on-disk location. An app that still carries the
// com.apple.quarantine xattr (e.g. downloaded with a browser and copied
// into /Applications without Finder's drag-move clearing it) is launched
// from a read-only nullfs mount at
// /private/var/folders/.../AppTranslocation/<UUID>/d/<App>.app, whose
// source is the original bundle — installing next to the translocated copy
// fails with "Read-only file system". The mount table names the source, so
// no private Security.framework call is needed. Paths outside a
// translocation mount are returned unchanged.
func untranslocate(path string) string {
	const marker = "/AppTranslocation/"
	i := strings.Index(path, marker)
	if i < 0 {
		return path
	}
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil || n <= 0 {
		return path
	}
	mounts := make([]unix.Statfs_t, n)
	if n, err = unix.Getfsstat(mounts, unix.MNT_NOWAIT); err != nil {
		return path
	}
	for _, m := range mounts[:n] {
		on := unix.ByteSliceToString(m.Mntonname[:])
		// The mount point is .../AppTranslocation/<UUID>; the bundle sits
		// under its "d/" subdirectory and the mount's source is the bundle.
		prefix := on + "/d/"
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := path[len(prefix):] // <App>.app/Contents/MacOS/...
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			return unix.ByteSliceToString(m.Mntfromname[:])
		}
		return unix.ByteSliceToString(m.Mntfromname[:]) + rest[slash:]
	}
	return path
}

// verifyAppleCodesign requires the downloaded bundle to carry a valid,
// non-ad-hoc code signature that Gatekeeper itself would accept — the same
// two checks .github/workflows/*-release.yml runs as a hard gate before a
// build is ever published. This is deliberately redundant with the
// Ed25519 manifest signature: it means a compromised update-signing key
// alone still isn't enough to get an unsigned/differently-signed payload
// installed, since it would also have to be signed with USBridge's Apple
// Developer ID certificate.
func verifyAppleCodesign(ctx context.Context, appPath string) error {
	if out, err := exec.CommandContext(ctx, "codesign", "--verify", "--deep", "--strict", appPath).CombinedOutput(); err != nil {
		return fmt.Errorf("codesign --verify: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	out, err := exec.CommandContext(ctx, "spctl", "--assess", "--type", "execute", appPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("spctl --assess: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	_ = out
	return nil
}
