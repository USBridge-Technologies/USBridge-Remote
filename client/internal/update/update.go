//go:build !noselfupdate

// This file is the real self-update implementation. It's compiled out
// entirely (see update_disabled.go) when built with `-tags noselfupdate` —
// the Android "market" flavor (Play Store / F-Droid / any other channel
// that must not fetch or install executable code on its own) uses that tag
// via client/scripts/build_android_gradle.sh's default (no self-update
// unless the build explicitly opts in with WITH_SELF_UPDATE=1, which is
// what the GitHub-Releases "direct" flavor passes). See docs/AUTO_UPDATE.md.
package update

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/sirupsen/logrus"
)

// checkTimeout bounds the manifest fetch+verify step so a slow/unreachable
// GitHub never meaningfully delays startup — this check runs unconditionally
// on every launch, so it must fail fast and safe, never block.
const checkTimeout = 8 * time.Second

// downloadTimeout bounds the (much larger) artifact download, only entered
// once a genuinely newer, signature-verified version was found and the
// user (or, for a headless launch, policy) has agreed to install it.
const downloadTimeout = 5 * time.Minute

// platformKey identifies this build in the manifest's "platforms" map.
func platformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// Check runs USBridge's mandatory startup update check: fetch and verify
// the signed manifest, and report a newer version if one names an artifact
// for this platform. It returns nil whenever there is nothing actionable
// to do — already up to date, offline, GitHub unreachable, bad signature,
// or a manifest with no artifact for this platform — logging the reason at
// Debug/Warn as appropriate. Callers must treat a nil return as "nothing to
// do" and continue startup normally; this never blocks longer than
// checkTimeout.
func Check(ctx context.Context, currentVersion string) *Manifest {
	if runtime.GOOS == "ios" {
		// Apple's App Store is the only sanctioned update path on iOS —
		// sideloaded binary replacement isn't possible there anyway, and
		// the signed manifest never carries an iOS artifact, so skip the
		// network round-trip entirely rather than logging a confusing
		// "no artifact for this platform" warning on every launch.
		return nil
	}

	log := logrus.WithField("component", "update")

	checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	client := &http.Client{Timeout: checkTimeout}
	manifest, err := fetchManifest(checkCtx, client)
	if err != nil {
		log.WithError(err).Debug("update check skipped (offline, GitHub unreachable, or no signed manifest yet)")
		return nil
	}

	if !isNewerVersion(manifest.Version, currentVersion) {
		log.WithFields(logrus.Fields{
			"current": currentVersion,
			"latest":  manifest.Version,
		}).Debug("already up to date")
		return nil
	}

	if _, ok := manifest.Platforms[platformKey()]; !ok {
		log.WithFields(logrus.Fields{
			"latest":    manifest.Version,
			"platform":  platformKey(),
			"platforms": sortedPlatformKeys(manifest.Platforms),
		}).Warn("update available but manifest has no artifact for this platform")
		return nil
	}

	return manifest
}

// DownloadAndApply downloads the platform artifact named in manifest
// (already signature-verified by Check), verifies its SHA-256, and applies
// it — replacing this install and re-launching the new binary. Only call
// this once the update has been approved (either by explicit user consent
// via a confirm dialog, or — for a non-interactive launch — by policy);
// Check itself never applies anything.
//
// onProgress, if non-nil, is called periodically during the download with
// bytes-downloaded/total — see ProgressFunc's doc comment for threading
// rules. Pass nil for a launch path with no UI to report to (e.g. the
// agent's --headless service mode via CheckAndApply below).
//
// A successful apply on desktop platforms (Windows/Linux/macOS) never
// returns — it hands off to a helper/relaunch and calls os.Exit itself. On
// Android it hands off to the system installer UI and returns normally.
func DownloadAndApply(ctx context.Context, manifest *Manifest, onProgress ProgressFunc) error {
	log := logrus.WithField("component", "update")

	asset, ok := manifest.Platforms[platformKey()]
	if !ok {
		return fmt.Errorf("manifest has no artifact for platform %s", platformKey())
	}

	log.WithFields(logrus.Fields{
		"version": manifest.Version,
		"asset":   asset.Asset,
	}).Info("downloading approved update")

	dlCtx, dlCancel := context.WithTimeout(ctx, downloadTimeout)
	defer dlCancel()

	dlClient := &http.Client{Timeout: 0} // context governs the overall deadline; large files stream
	path, err := downloadArtifact(dlCtx, dlClient, releaseAssetURL(asset.Asset), asset.SHA256, onProgress)
	if err != nil {
		return fmt.Errorf("download/verify update: %w", err)
	}

	log.WithField("version", manifest.Version).Info("update verified — applying")
	if err := apply(dlCtx, path, manifest.Version); err != nil {
		return fmt.Errorf("apply update: %w", err)
	}
	return nil
}

// CheckAndApply is Check followed immediately by DownloadAndApply (no
// progress reporting, no user confirmation in between) — for launch paths
// with no GUI to report through (e.g. the agent's --headless service
// mode). Every failure is logged and swallowed so the current,
// already-running version starts up exactly as if this package didn't
// exist.
func CheckAndApply(ctx context.Context, currentVersion string) {
	manifest := Check(ctx, currentVersion)
	if manifest == nil {
		return
	}
	if err := DownloadAndApply(ctx, manifest, nil); err != nil {
		logrus.WithField("component", "update").WithError(err).Error("failed to apply update — staying on current version")
	}
}

// unsupportedPlatformApply is the fallback apply() body for any GOOS/GOARCH
// this package doesn't have a real updater for yet — it's a safe no-op, not
// a panic, since applying an update must never take down a working install.
func unsupportedPlatformApply(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("self-update not implemented for %s/%s", runtime.GOOS, runtime.GOARCH)
}
