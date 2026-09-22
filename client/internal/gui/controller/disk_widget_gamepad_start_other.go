//go:build !(js && wasm)

package controller

import "usbridge-client/internal/platform"

// startPadCapture opens native OS-level gamepad capture and forwards state to
// the host via the active Moonlight session (see forwardGamepadState) --
// exactly how gamepad passthrough has always worked on every platform except
// the web build, which has no OS-level HID access at all and instead exports
// a synthetic USB/IP device from the agent
// (disk_widget_gamepad_start_wasm.go).
func (dw *DiskWidget) startPadCapture(id string) (gamepadCaptureHandle, error) {
	return platform.StartGamepadCapture(id, func(state platform.GamepadCaptureState) {
		dw.forwardGamepadState(id, state)
	})
}
