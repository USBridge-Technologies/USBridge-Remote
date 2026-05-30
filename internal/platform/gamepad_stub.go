//go:build !darwin && !(linux && !android) && !windows

package platform

// GamepadDevice describes a system gamepad.
type GamepadDevice struct {
	ID   string
	Name string
}

// EnumerateGamepads returns all gamepads currently connected to the system.
// Non-macOS platforms return empty; gamepad capture goes through Moonlight.
func EnumerateGamepads() []GamepadDevice {
	return nil
}
