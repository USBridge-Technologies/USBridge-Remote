package controller

import (
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
}

// forwardGamepadState sends the decoded gamepad state through the active Moonlight
// stream. Called from the capture goroutine (not from the Fyne UI thread).
func (dw *DiskWidget) forwardGamepadState(_ string, state platform.GamepadCaptureState) {
	if dw.moonlightProvider == nil {
		return
	}
	sender := dw.moonlightProvider()
	if sender == nil {
		return
	}
	sender.SendMoonlightControllerEvent(
		0,              // controllerNumber
		1,              // activeGamepadMask (bit 0 = controller 0 active)
		state.Buttons,
		state.LeftTrigger,
		state.RightTrigger,
		state.LeftX,
		state.LeftY,
		state.RightX,
		state.RightY,
	)
}

// stopAllGamepadCaptures stops every active capture; called on disconnect.
func (dw *DiskWidget) stopAllGamepadCaptures() {
	for id, cap := range dw.activeCaptures {
		logrus.Infof("🎮 [GAMEPAD] stopping capture (disconnect) for %s", id)
		cap.Stop()
		delete(dw.activeCaptures, id)
	}
}
