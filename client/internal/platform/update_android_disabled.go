//go:build android && noselfupdate

// Counterpart to update_android.go for the Android "market" flavor
// (Play Store / F-Droid / any channel that must not fetch or install
// executable code on its own — see client/internal/update/update_disabled.go).
// This build tag excludes the cgo/JNI file entirely, so the compiled binary
// has no PackageInstaller-calling code path at all, not just an unreachable
// one; these pure-Go stubs exist only so client/cmd/android/main.go's
// wiring keeps compiling unchanged.
package platform

func InstallAPK(path string) (bool, error) {
	return false, nil
}

func CanRequestPackageInstalls() (bool, error) {
	return false, nil
}

func RequestInstallPermission() error {
	return nil
}
