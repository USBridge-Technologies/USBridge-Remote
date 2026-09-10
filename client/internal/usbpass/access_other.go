//go:build !linux

package usbpass

// LastUSBAccessError is Linux-only (pkexec/udev grant).
func LastUSBAccessError() string { return "" }

// USBAccessGranted is always true off Linux (Windows uses a different path).
func USBAccessGranted(devs []usbDevRef) bool { return true }

type usbDevRef struct {
	BusID  string
	Busnum uint32
	Devnum uint32
}

func usbAccessNeeded(devs []usbDevRef) bool { return false }

// EnsureUSBAccess is a no-op off Linux.
func EnsureUSBAccess(devs []usbDevRef) error { return nil }

// RequestUSBAccess is a no-op off Linux.
func RequestUSBAccess(devs []usbDevRef) bool { return true }
