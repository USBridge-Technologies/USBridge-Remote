//go:build android

package update

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
)

// InstallAPK, CanRequestInstall, and RequestInstallPermission are wired by
// client/cmd/android/main.go to internal/platform's JNI bridge to
// MainActivity (see MainActivity.kt's "Self-update" section). Left nil
// they're a safe, if useless, no-op — apply() below just reports an error
// rather than crashing.
var (
	// InstallAPK hands an already-downloaded, already-verified APK to the
	// system PackageInstaller directly (F-Droid's approach), returning
	// whether the install intent was launched successfully.
	InstallAPK func(path string) (bool, error)

	// CanRequestInstall reports whether this app currently holds the
	// "install unknown apps" permission (Android 8+) — required before
	// InstallAPK's intent can succeed.
	CanRequestInstall func() (bool, error)

	// RequestInstallPermission opens this app's "Install unknown apps"
	// Settings screen so the user can grant it — there is no runtime
	// permission dialog for this one.
	RequestInstallPermission func() error
)

// apply on Android cannot silently replace the running APK the way the
// desktop platforms do: Android sideloaded apps have no permission to
// install over themselves without going through the OS's own
// PackageInstaller, which is also the one place Android independently
// re-verifies the update (it refuses to install unless the new APK is
// signed with the exact same certificate as the currently-installed app —
// see ANDROID_KEYSTORE_BASE64 in .github/workflows/release-all.yml).
//
// The APK itself was already downloaded and hash-verified against the
// signed manifest by the time apply() runs (see update.go), so this only
// needs to hand it to the installer — via InstallAPK's content:// URI
// intent (the same approach F-Droid uses), not a browser redirect to the
// GitHub release page requiring a second manual download.
func apply(ctx context.Context, artifactPath, version string) error {
	log := logrus.WithField("component", "update")

	if InstallAPK == nil || CanRequestInstall == nil {
		return fmt.Errorf("android install hooks not wired (internal/platform unavailable)")
	}

	canInstall, err := CanRequestInstall()
	if err != nil {
		return fmt.Errorf("check install-unknown-apps permission: %w", err)
	}
	if !canInstall {
		log.Warn("missing \"install unknown apps\" permission — opening Settings; the update wasn't installed, try again after granting it")
		if RequestInstallPermission != nil {
			if err := RequestInstallPermission(); err != nil {
				log.WithError(err).Warn("could not open install-permission settings")
			}
		}
		return fmt.Errorf("missing \"install unknown apps\" permission for this app — grant it in the Settings screen that just opened, then try updating again")
	}

	log.WithField("version", version).Info("update verified — handing off to the system installer")
	ok, err := InstallAPK(artifactPath)
	if err != nil {
		return fmt.Errorf("launch package installer: %w", err)
	}
	if !ok {
		return fmt.Errorf("package installer did not launch")
	}
	// Deliberately not removing artifactPath here: Android's installer
	// reads it asynchronously (via the content:// URI just granted), well
	// after this function returns — deleting it out from under that would
	// be a race. It's a temp/cache file; the OS reclaims it eventually.
	return nil
}
