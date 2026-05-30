package controller

import (
	"usbridge-client/internal/models"
	"usbridge-client/internal/platform"
	"usbridge-client/internal/service"

	"github.com/sirupsen/logrus"
)

// moonlightProvider is stored on DiskWidget to obtain the active MoonlightInputSender.
// It is set from outside (main window) once a Moonlight-capable video client is available.
type moonlightProvider func() service.MoonlightInputSender

// SetMoonlightProvider wires a provider that returns the active MoonlightInputSender
// when a Moonlight stream is connected (or nil otherwise).
func (dw *DiskWidget) SetMoonlightProvider(fn func() service.MoonlightInputSender) {
	dw.moonlightProvider = fn
}

// syncGamepadCaptures compares the set of currently mounted gamepad drives against
// the set of active captures and starts/stops captures accordingly.
// Must be called from within the Fyne UI goroutine (or any single-threaded context)
// since it reads dw.allDrives and dw.activeCaptures without a lock.
func (dw *DiskWidget) syncGamepadCaptures() {
	if dw.activeCaptures == nil {
		dw.activeCaptures = make(map[string]*platform.GamepadCapture)
	}

	// Build the set of gamepad IDs that should be captured (mounted & has ID).
	wanted := make(map[string]bool)
	for _, drive := range dw.allDrives {
		if drive.IsGamepad && drive.IsMounted && drive.GamepadID != "" {
			wanted[drive.GamepadID] = true
		}
	}

	// Stop captures for devices that are no longer mounted.
	for id, cap := range dw.activeCaptures {
		if !wanted[id] {
			logrus.Infof("🎮 [GAMEPAD] stopping capture for %s", id)
			cap.Stop()
			delete(dw.activeCaptures, id)
		}
	}

	// Start captures for newly mounted devices.
	for id := range wanted {
		if _, ok := dw.activeCaptures[id]; ok {
			continue
		}
		logrus.Infof("🎮 [GAMEPAD] starting capture for %s", id)
		capturedID := id
		cap, err := platform.StartGamepadCapture(id, func(state platform.GamepadCaptureState) {
			dw.forwardGamepadState(capturedID, state)
		})
		if err != nil {
			logrus.Warnf("🎮 [GAMEPAD] capture failed for %s: %v", id, err)
			continue
		}
		dw.activeCaptures[id] = cap
	}

	// Keep a persistent gamepad WebSocket open whenever any capture is active.
	if dw.usbClient != nil {
		if len(dw.activeCaptures) > 0 {
			if err := dw.usbClient.ConnectGamepadWebSocket(); err != nil {
				logrus.Warnf("🎮 [GAMEPAD] WebSocket connect failed: %v", err)
			}
		} else {
			dw.usbClient.DisconnectGamepadWebSocket()
		}
	}
}

// forwardGamepadState sends the decoded gamepad state. Prefers the Moonlight path
// when a stream is active (lower latency); falls back to the direct WebSocket API.
// Called from the capture goroutine (not from the Fyne UI thread).
func (dw *DiskWidget) forwardGamepadState(_ string, state platform.GamepadCaptureState) {
	// Prefer Moonlight when a stream is active.
	if dw.moonlightProvider != nil {
		if sender := dw.moonlightProvider(); sender != nil {
			sender.SendMoonlightControllerEvent(
				0, // controllerNumber
				1, // activeGamepadMask (bit 0 = controller 0 active)
				state.Buttons,
				state.LeftTrigger,
				state.RightTrigger,
				state.LeftX,
				state.LeftY,
				state.RightX,
				state.RightY,
			)
			return
		}
	}
	// Fall back to direct WebSocket API (works without video streaming).
	if dw.usbClient != nil {
		dw.usbClient.SendGamepadReportWS(models.GamepadRequest{
			Buttons:      state.Buttons,
			LeftX:        int8(state.LeftX >> 8),
			LeftY:        int8(state.LeftY >> 8),
			RightX:       int8(state.RightX >> 8),
			RightY:       int8(state.RightY >> 8),
			LeftTrigger:  state.LeftTrigger,
			RightTrigger: state.RightTrigger,
		})
	}
}

// stopAllGamepadCaptures stops every active capture; called on disconnect.
func (dw *DiskWidget) stopAllGamepadCaptures() {
	for id, cap := range dw.activeCaptures {
		logrus.Infof("🎮 [GAMEPAD] stopping capture (disconnect) for %s", id)
		cap.Stop()
		delete(dw.activeCaptures, id)
	}
	if dw.usbClient != nil {
		dw.usbClient.DisconnectGamepadWebSocket()
	}
}
