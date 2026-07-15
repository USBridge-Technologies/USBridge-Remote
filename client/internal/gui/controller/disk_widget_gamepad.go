package controller

import (
	"sync/atomic"

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

// gamepadLogSeq is used to rate-limit per-frame gamepad debug logs.
var gamepadLogSeq atomic.Uint64

// forwardGamepadState sends the decoded gamepad state via Moonlight.
// Called from the capture goroutine (not from the Fyne UI thread).
func (dw *DiskWidget) forwardGamepadState(id string, state platform.GamepadCaptureState) {
	seq := gamepadLogSeq.Add(1)
	hasInput := state.Buttons != 0 || state.LeftTrigger != 0 || state.RightTrigger != 0 ||
		state.LeftX != 0 || state.LeftY != 0 || state.RightX != 0 || state.RightY != 0
	if hasInput {
		logrus.Infof("🎮 [GAMEPAD] input id=%s buttons=0x%04x lt=%d rt=%d lx=%d ly=%d rx=%d ry=%d",
			id, state.Buttons, state.LeftTrigger, state.RightTrigger,
			state.LeftX>>8, state.LeftY>>8, state.RightX>>8, state.RightY>>8)
	} else if seq%300 == 1 {
		logrus.Debugf("🎮 [GAMEPAD] heartbeat id=%s (idle)", id)
	}

	if dw.moonlightProvider != nil {
		if sender := dw.moonlightProvider(); sender != nil && sender.IsInputActive() {
			if seq%300 == 1 {
				logrus.Infof("🎮 [GAMEPAD] → Moonlight path (seq=%d)", seq)
			}
			sender.SendMoonlightControllerEvent(
				0,
				1,
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
	if seq%300 == 1 {
		logrus.Warnf("🎮 [GAMEPAD] Moonlight not active — gamepad input dropped (seq=%d)", seq)
	}
}

// stopAllGamepadCaptures stops every active capture; called on disconnect.
func (dw *DiskWidget) stopAllGamepadCaptures() {
	for id, cap := range dw.activeCaptures {
		logrus.Infof("🎮 [GAMEPAD] stopping capture (disconnect) for %s", id)
		cap.Stop()
		delete(dw.activeCaptures, id)
	}
}
