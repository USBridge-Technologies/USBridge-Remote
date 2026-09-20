//go:build (!linux || android) && !darwin

package usbpass

// LastUSBAccessError is Linux-only (pkexec/udev grant); macOS has its own
// implementation in access_darwin.go for Input Monitoring denials.
func LastUSBAccessError() string { return "" }

// InputMonitoringGranted is always true off macOS -- Input Monitoring is a
// macOS-only TCC permission (see hidbridge_darwin.go's IOHIDDeviceOpen).
func InputMonitoringGranted() bool { return true }

// RequestInputMonitoringAccess is a no-op off macOS.
func RequestInputMonitoringAccess() bool { return true }

// OpenInputMonitoringSettingsPane is a no-op off macOS.
func OpenInputMonitoringSettingsPane() {}

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

// powerCycleUSBPort is Linux-only (sysfs authorized toggle).
func powerCycleUSBPort(busID string) error { return nil }
