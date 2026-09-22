//go:build js && wasm

package controller

import (
	"usbridge-client/internal/platform"
	"usbridge-client/internal/usbpass"
)

// browserGamepadCapture stops a browser-sourced USB/IP synthetic gamepad
// export (usbpass.AttachBrowserGamepad): wraps both the AES-attach session
// teardown and the Gamepad API poller teardown behind one Stop(), so
// syncGamepadCaptures can treat it exactly like every other platform's
// *platform.GamepadCapture (see gamepadCaptureHandle).
type browserGamepadCapture struct {
	poll       *platform.BrowserGamepadCapture
	stopAttach func()
}

func (h *browserGamepadCapture) Stop() {
	if h.poll != nil {
		h.poll.Stop()
	}
	if h.stopAttach != nil {
		h.stopAttach()
	}
}

// startPadCapture exports the browser's Gamepad API state as a synthetic
// USB/IP Xbox 360 controller hosted by the agent (agent/internal/browserusb)
// instead of native OS-level capture + Moonlight forwarding: the web build
// has neither (platform.StartGamepadCapture has no wasm implementation, and
// this path works whether or not a Moonlight stream is even active), so
// input has to reach the remote host through the agent's own USB stack, the
// same way the native passthrough client's real devices do (see
// disk_widget_mount.go's mountUSBPassthrough).
func (dw *DiskWidget) startPadCapture(id string) (gamepadCaptureHandle, error) {
	send, stopAttach, err := usbpass.AttachBrowserGamepad(usbpass.BrowserGamepadAttachOptions{
		AgentBaseURL: dw.usbClient.GetBaseURL(),
		Secret:       dw.usbClient.APISecret(),
	})
	if err != nil {
		return nil, err
	}
	poll := platform.StartBrowserGamepadCapture(send)
	return &browserGamepadCapture{poll: poll, stopAttach: stopAttach}, nil
}
