//go:build !windows && !(linux && !android)

package platform

// SetGamepadRumble is a no-op where force feedback is not implemented
// (macOS, mobile, web): the host's rumble is received and dropped.
func SetGamepadRumble(id string, low, high uint16) {}

// StopGamepadRumble is a no-op, see SetGamepadRumble.
func StopGamepadRumble(id string) {}

// SetGamepadLight is a no-op, see SetGamepadRumble.
func SetGamepadLight(id string, r, g, b uint8) {}

// SetGamepadTriggers is a no-op, see SetGamepadRumble.
func SetGamepadTriggers(id string, flags, typeLeft, typeRight uint8, left, right [10]byte) {}

// NoteGamepadHaptics is a no-op, see SetGamepadRumble.
func NoteGamepadHaptics(id string) {}
