//go:build !windows

package usbpass

// USBIPDriverInstalled is Windows-only (usbip-win2); elsewhere the driver
// state comes from Status().VhciDriver.
func USBIPDriverInstalled() bool { return false }
