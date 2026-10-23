//go:build !windows && !(linux && !android)

package platform

// StartGamepadHID is implemented on Windows and Linux, which let a user
// program read a pad's HID input report. macOS reports the touchpad through
// IOKit, which is not wired up.
func StartGamepadHID(deviceID string, onReport func(DS4Report)) (*TouchpadCapture, error) {
	return nil, ErrNoTouchpad
}
