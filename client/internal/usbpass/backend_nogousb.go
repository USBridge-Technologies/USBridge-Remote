//go:build !usbpass_gousb && !darwin

package usbpass

import "fmt"

// TryClaimGousb is a no-op unless built with -tags usbpass_gousb (needs libusb).
func TryClaimGousb(dev *ExportedDevice) error {
	_ = dev
	return fmt.Errorf("gousb claim disabled (build with -tags usbpass_gousb)")
}
