//go:build !darwin && !(linux && !android)

package platform

import "fmt"

// GamepadCaptureState holds the current decoded state of a gamepad.
type GamepadCaptureState struct {
	Buttons                   uint16
	LeftX, LeftY              int16
	RightX, RightY            int16
	LeftTrigger, RightTrigger uint8
}

// GamepadCapture is a no-op handle on non-darwin platforms.
type GamepadCapture struct{}

// StartGamepadCapture is not supported on non-darwin platforms.
func StartGamepadCapture(_ string, _ func(GamepadCaptureState)) (*GamepadCapture, error) {
	return nil, fmt.Errorf("gamepad capture is not supported on this platform")
}

// Stop is a no-op.
func (c *GamepadCapture) Stop() {}
