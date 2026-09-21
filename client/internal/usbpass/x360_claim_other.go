//go:build !windows && !linux

package usbpass

// tryClaimX360 is the synthetic Xbox 360 hook of the claim path. Only Windows
// (XInput, x360_claim_windows.go) and Linux (evdev, x360_claim_linux.go) have a
// gamepad source; macOS and Android have their own TryClaimGousb.
func tryClaimX360(dev *ExportedDevice) (handled bool, err error) {
	return false, nil
}
