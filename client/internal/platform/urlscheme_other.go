//go:build (!linux && !windows) || android

package platform

// macOS/iOS declare the scheme in Info.plist, Android
// in AndroidManifest.xml, and the browser has no OS handler to register.

// IsURLSchemeRegistered reports whether usbridge:// links open this app.
func IsURLSchemeRegistered() bool { return true }

// RegisterURLScheme registers usbridge:// for the current user.
func RegisterURLScheme() error { return nil }
