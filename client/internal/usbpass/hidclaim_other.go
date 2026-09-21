//go:build !windows

package usbpass

// tryClaimHID is the HID bridge hook of the libusb claim path. Only Windows has
// an HID source today (hidbridge_windows.go); macOS has its own TryClaimGousb
// (hidbridge_darwin.go) and on Linux the raw libusb claim already works.
func tryClaimHID(dev *ExportedDevice) (handled bool, err error) {
	return false, nil
}
