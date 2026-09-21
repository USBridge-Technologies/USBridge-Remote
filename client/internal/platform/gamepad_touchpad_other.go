//go:build !windows

package platform

// StartGamepadTouchpad is only implemented on Windows (it reads the pad's HID
// input report). Linux exposes a DualShock 4 touchpad as its own evdev device
// and macOS reports it through IOKit; neither is wired up.
func StartGamepadTouchpad(deviceID string, onEvent func(TouchpadEvent)) (*TouchpadCapture, error) {
	return nil, ErrNoTouchpad
}
