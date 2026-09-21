//go:build !windows

package usbpass

// RestoreLocalInput is a no-op away from Windows: only Windows' bridge switches a
// tablet's local input off while it is exported (hidlocal_windows.go).
func RestoreLocalInput() {}
