//go:build !usbpass_gousb && !darwin && !android

package usbpass

import "fmt"

// TryClaimGousb is a no-op unless built with -tags usbpass_gousb (needs libusb),
// except that a HID device can still be bridged without libusb on Windows.
func TryClaimGousb(dev *ExportedDevice) error {
	if handled, err := tryClaimX360(dev); handled {
		return err
	}
	if handled, err := tryClaimHID(dev); handled {
		return err
	}
	return fmt.Errorf("gousb claim disabled (build with -tags usbpass_gousb)")
}
