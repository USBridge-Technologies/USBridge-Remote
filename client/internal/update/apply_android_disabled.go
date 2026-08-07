//go:build android && noselfupdate

// Counterpart to apply_android.go (see update_disabled.go's package doc
// comment) for the Android "market" flavor. InstallAPK/CanRequestInstall/
// RequestInstallPermission stay declared — with the same names and types —
// purely so client/cmd/android/main.go's wiring
// (`update.InstallAPK = platform.InstallAPK`, ...) keeps compiling
// unchanged regardless of this tag; nothing in this build ever calls them,
// since Check (update_disabled.go) never reports an update to apply.
package update

var (
	InstallAPK               func(path string) (bool, error)
	CanRequestInstall        func() (bool, error)
	RequestInstallPermission func() error
)
