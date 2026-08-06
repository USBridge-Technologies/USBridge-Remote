//go:build android

package update

import (
	"context"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
)

// apply on Android cannot silently replace the running APK the way the
// desktop platforms do: Android sideloaded apps have no permission to
// install over themselves, and doing so without going through the OS's
// own PackageInstaller would mean giving up the one MITM/tamper defense
// Android already provides for free — the OS refuses to install an update
// APK unless it's signed with the exact same signing certificate as the
// currently-installed app (see ANDROID_KEYSTORE_BASE64 in
// .github/workflows/release-all.yml).
//
// So Android's flow is: verify the signed manifest and the downloaded
// APK's SHA-256 exactly like every other platform (that already happened
// in update.go/download.go by the time apply() runs), then hand off to the
// OS installer UI via URLOpener (wired to the app's own
// fyne.App.OpenURL by client/cmd/android/main.go) so the user completes the
// install through Android's normal, signature-checked flow. This is the
// one platform where "forced" update means "verified and handed to the
// user for one tap" rather than a fully silent swap — there is no
// unprivileged way to do better on stock Android.
func apply(ctx context.Context, artifactPath, version string) error {
	// The APK itself was already downloaded and hash-verified against the
	// signed manifest; we don't need the file further since the browser
	// handles its own (re-)download through the release page.
	os.Remove(artifactPath)

	if URLOpener == nil {
		return fmt.Errorf("no URL opener registered — cannot hand off to the Android installer")
	}

	releasePage := fmt.Sprintf("https://github.com/%s/%s/releases/latest", repoOwner, repoName)
	logrus.WithFields(logrus.Fields{
		"component": "update",
		"version":   version,
	}).Info("update verified — opening release page for install (Android requires the system installer)")

	return URLOpener(releasePage)
}
