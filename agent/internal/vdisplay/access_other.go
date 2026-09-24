//go:build !linux && !windows

package vdisplay

// Linux grants a polkit rule for `modprobe vkms` (access_linux.go), Windows
// installs the MttVDD driver (access_windows.go). Elsewhere (macOS's
// CGVirtualDisplay) nothing needs granting.
func AccessGranted() bool { return true }
func GrantAccess() error  { return nil }
func Unload() error       { return nil }
func Revive() error       { return nil }
