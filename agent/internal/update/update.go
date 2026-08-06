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
// update has been approved (by the user via a confirm dialog, or by policy
// for a headless launch with no one to ask).
const downloadTimeout = 5 * time.Minute

// platformKey identifies this build in the manifest's "platforms" map.
func platformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// Check runs USBridge Agent's mandatory startup update check: fetch and
// verify the signed manifest, and report a newer version if one names an
// artifact for this platform. It returns nil whenever there is nothing
// actionable to do — already up to date, offline, GitHub unreachable, bad
// signature, or a manifest with no artifact for this platform — logging the
// reason at Debug/Warn as appropriate. Callers must treat a nil return as
// "nothing to do" and continue startup normally; this never blocks longer
// than checkTimeout.
func Check(ctx context.Context, currentVersion string) *Manifest {
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
// it in place — replacing this install and re-launching the new binary.
// Only call this once the update has been approved; Check itself never
// applies anything. A successful apply never returns — it hands off to a
// helper/relaunch and calls os.Exit itself.
func DownloadAndApply(ctx context.Context, manifest *Manifest) error {
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
	path, err := downloadArtifact(dlCtx, dlClient, releaseAssetURL(asset.Asset), asset.SHA256)
	if err != nil {
		return fmt.Errorf("download/verify update: %w", err)
	}

	log.WithField("version", manifest.Version).Info("update verified — applying")
	if err := apply(dlCtx, path, manifest.Version); err != nil {
		return fmt.Errorf("apply update: %w", err)
	}
	return nil
}

// CheckAndApply is Check followed immediately by DownloadAndApply with no
// user confirmation in between — used only for the agent's --headless
// service launch, which has no GUI to ask through. Every failure is logged
// and swallowed so the current, already-running version starts up exactly
// as if this package didn't exist. Non-headless launches use Check +
// DownloadAndApply directly, gated on a confirm dialog — see
// internal/app.Start and internal/ui's ShowAndRun.
func CheckAndApply(ctx context.Context, currentVersion string) {
	manifest := Check(ctx, currentVersion)
	if manifest == nil {
		return
	}
	if err := DownloadAndApply(ctx, manifest); err != nil {
		logrus.WithField("component", "update").WithError(err).Error("failed to apply update — staying on current version")
	}
}

// unsupportedPlatformApply is the fallback apply() body for any GOOS/GOARCH
// this package doesn't have a real updater for — a safe no-op, not a panic,
// since applying an update must never take down a working install.
func unsupportedPlatformApply(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("self-update not implemented for %s/%s", runtime.GOOS, runtime.GOARCH)
}
