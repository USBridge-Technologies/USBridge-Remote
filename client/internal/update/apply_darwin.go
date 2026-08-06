//go:build darwin

package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	installedApp, err := currentAppBundlePath()
	if err != nil {
		return err
	}
	installDir := filepath.Dir(installedApp)

	stagedApp := filepath.Join(installDir, ".usbridge-update-staged.app")
	os.RemoveAll(stagedApp)
	if out, err := exec.CommandContext(ctx, "ditto", newApp, stagedApp).CombinedOutput(); err != nil {
		os.RemoveAll(stagedApp)
		return fmt.Errorf("ditto copy into %s (no write permission?): %w (%s)", installDir, err, strings.TrimSpace(string(out)))
	}

	backupApp := filepath.Join(installDir, ".usbridge-update-previous.app")
	os.RemoveAll(backupApp)
	if err := os.Rename(installedApp, backupApp); err != nil {
		os.RemoveAll(stagedApp)
		return fmt.Errorf("move aside current install: %w", err)
	}
	if err := os.Rename(stagedApp, installedApp); err != nil {
		// Roll back so the app isn't left half-installed.
		os.Rename(backupApp, installedApp)
		os.RemoveAll(stagedApp)
		return fmt.Errorf("install new version: %w", err)
	}
	os.RemoveAll(backupApp)

	if err := exec.Command("open", "-n", installedApp).Start(); err != nil {
		return fmt.Errorf("relaunch updated app: %w", err)
	}

	os.Exit(0)
	return nil // unreachable
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
	marker := "/" + appBundleName + "/"
	idx := strings.Index(exePath, marker)
	if idx < 0 {
		return "", fmt.Errorf("running binary %s is not inside %s (not installed as a normal .app bundle — e.g. running from `go run` or a bare binary), skipping self-update", exePath, appBundleName)
	}
	return exePath[:idx+len(marker)-1], nil
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
