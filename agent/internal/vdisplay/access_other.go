//go:build !linux

package vdisplay

// Linux-only (polkit rule for `modprobe vkms`; see access_linux.go).
// Elsewhere nothing needs granting.
func AccessGranted() bool { return true }
func GrantAccess() error  { return nil }
func Unload() error       { return nil }
func Revive() error       { return nil }
