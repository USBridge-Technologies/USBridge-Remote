//go:build !darwin && !windows && (!linux || android)

package service

// NativeVideoOverlayIsActive always returns false on platforms without a
// native GPU overlay (Android, iOS, and other non-desktop targets).
func NativeVideoOverlayIsActive() bool { return false }
