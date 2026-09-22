//go:build js && wasm

package controller

import (
	"usbridge-client/internal/platform"
	"usbridge-client/internal/usbpass"
)

// browserPenCapture stops a browser-sourced USB/IP synthetic Wacom tablet
// export (usbpass.AttachBrowserPen): wraps both the AES-attach session
// teardown and the WebHID oninputreport listener teardown behind one
// Stop(), so syncPenCaptures can treat it exactly like every other
// platform's *platform.PenCapture (see penCaptureHandle) -- same pattern as
// disk_widget_gamepad_start_wasm.go's browserGamepadCapture.
type browserPenCapture struct {
	capture    *platform.BrowserPenCapture
	stopAttach func()
}

func (h *browserPenCapture) Stop() {
	if h.capture != nil {
		h.capture.Stop()
	}
	if h.stopAttach != nil {
		h.stopAttach()
	}
}

// startPenCapture exports the browser's WebHID-granted Wacom tablet as a
// synthetic USB/IP device hosted by the agent (agent/internal/browserusb),
// forwarding raw HID reports rather than decoding them client-side (see
// pen_capture_wasm.go's doc comment): the web build has no Moonlight session
// to send semantic pen events over even if it decoded them itself
// (WebRTCVideoClient.SendMoonlightPenEvent is an unimplemented stub), so
// input has to reach the remote host through the agent's own USB stack the
// same way the browser gamepad path and the native passthrough client's
// real devices both already do.
func (dw *DiskWidget) startPenCapture(t platform.PenTabletInfo) (penCaptureHandle, error) {
	send, stopAttach, err := usbpass.AttachBrowserPen(t.VID, t.PID, t.Name, usbpass.BrowserGamepadAttachOptions{
		AgentBaseURL: dw.usbClient.GetBaseURL(),
		Secret:       dw.usbClient.APISecret(),
	})
	if err != nil {
		return nil, err
	}
	capture, err := platform.StartBrowserPenCapture(t.ID, send)
	if err != nil {
		stopAttach()
		return nil, err
	}
	return &browserPenCapture{capture: capture, stopAttach: stopAttach}, nil
}
