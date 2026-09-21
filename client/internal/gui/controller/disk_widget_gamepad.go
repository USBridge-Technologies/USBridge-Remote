package controller

import (
	"errors"
	"math"
	"sort"
	"strings"
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

	// The host's rumble goes to whichever pads are being captured.
	dw.rumbleOnce.Do(func() { service.SetRumbleHandler(dw.onHostRumble) })

	// Stop captures for devices that are no longer mounted.
	for id, cap := range dw.activeCaptures {
		if !wanted[id] {
			logrus.Infof("🎮 [GAMEPAD] stopping capture for %s", id)
			cap.Stop()
			platform.StopGamepadRumble(id)
			delete(dw.activeCaptures, id)
			dw.stopTouchpad(id)
			dw.releasePadSlot(id)
		}
	}

	// Start captures for newly mounted devices, in a fixed order so the same
	// pads get the same controller numbers on every run.
	newIDs := make([]string, 0, len(wanted))
	for id := range wanted {
		if _, ok := dw.activeCaptures[id]; !ok {
			newIDs = append(newIDs, id)
		}
	}
	sort.Strings(newIDs)
	for _, id := range newIDs {
		slot, ok := dw.padSlots.assign(id)
		if !ok {
			logrus.Warnf("🎮 [GAMEPAD] not capturing %s: all %d controller slots are in use", id, maxGamepadSlots)
			continue
		}
		logrus.Infof("🎮 [GAMEPAD] starting capture for %s as controller %d", id, slot)
		capturedID := id
		cap, err := platform.StartGamepadCapture(id, func(state platform.GamepadCaptureState) {
			dw.forwardGamepadState(capturedID, state)
		})
		if err != nil {
			logrus.Warnf("🎮 [GAMEPAD] capture failed for %s: %v", id, err)
			dw.padSlots.release(id)
			continue
		}
		dw.activeCaptures[id] = cap
		dw.startTouchpad(id)
	}
}

// startTouchpad turns the pad's touchpad (DualShock 4 layout) into a relative
// mouse for the host, next to the normal pad capture. A pad without one just
// does not get a reader.
func (dw *DiskWidget) startTouchpad(id string) {
	if dw.activeTouchpads == nil {
		dw.activeTouchpads = make(map[string]*platform.TouchpadCapture)
	}
	tp, err := platform.StartGamepadTouchpad(id, dw.forwardTouchpad)
	if err != nil {
		if !errors.Is(err, platform.ErrNoTouchpad) {
			logrus.Warnf("🎮 [GAMEPAD] touchpad of %s unavailable: %v", id, err)
		}
		return
	}
	dw.activeTouchpads[id] = tp
}

// stopTouchpad ends a pad's touchpad reader and releases its click if it was held.
func (dw *DiskWidget) stopTouchpad(id string) {
	tp, ok := dw.activeTouchpads[id]
	if !ok {
		return
	}
	tp.Stop()
	delete(dw.activeTouchpads, id)
	dw.forwardTouchpad(platform.TouchpadEvent{ClickChanged: true, ClickDown: false})
}

// forwardTouchpad sends touchpad motion and clicks to the host as a relative mouse.
// Called from the touchpad reader goroutine.
func (dw *DiskWidget) forwardTouchpad(ev platform.TouchpadEvent) {
	if dw.moonlightProvider == nil {
		return
	}
	sender := dw.moonlightProvider()
	if sender == nil || !sender.IsInputActive() {
		return
	}
	clamp := func(v int) int16 {
		if v > math.MaxInt16 {
			return math.MaxInt16
		}
		if v < math.MinInt16 {
			return math.MinInt16
		}
		return int16(v)
	}
	if ev.DX != 0 || ev.DY != 0 {
		sender.SendMoonlightMouseMove(clamp(ev.DX), clamp(ev.DY))
	}
	if ev.ClickChanged {
		action := service.LiMouseButtonRelease
		if ev.ClickDown {
			action = service.LiMouseButtonPress
		}
		sender.SendMoonlightMouseButton(action, service.LiMouseButtonLeft)
	}
}

// releasePadSlot frees a stopped pad's controller number and tells the host
// the controller is gone: the streamer unplugs its virtual pad when the number
// leaves the active gamepad mask, so a pad that was switched off does not stay
// "connected" (and stuck at its last state) in the game.
func (dw *DiskWidget) releasePadSlot(id string) {
	slot, ok := dw.padSlots.release(id)
	if !ok || dw.moonlightProvider == nil {
		return
	}
	if sender := dw.moonlightProvider(); sender != nil && sender.IsInputActive() {
		sender.SendMoonlightControllerEvent(uint16(slot), dw.padSlots.mask(), 0, 0, 0, 0, 0, 0, 0)
	}
}

// onHostRumble applies the host's rumble request to the pad that holds that
// controller number. It runs on moonlight-common-c's callback thread.
func (dw *DiskWidget) onHostRumble(controller, lowFreq, highFreq uint16) {
	id, ok := dw.padSlots.idAt(int(controller))
	if !ok {
		return
	}
	logrus.Debugf("🎮 [GAMEPAD] host rumble controller=%d low=%d high=%d -> %s", controller, lowFreq, highFreq, id)
	platform.SetGamepadRumble(id, lowFreq, highFreq)
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
			slot, ok := dw.padSlots.slot(id)
			if !ok {
				return // stopped while this sample was in flight
			}
			sender.SendMoonlightControllerEvent(
				uint16(slot),
				dw.padSlots.mask(),
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
		platform.StopGamepadRumble(id)
		delete(dw.activeCaptures, id)
		dw.stopTouchpad(id)
		dw.releasePadSlot(id)
	}
}

// gamepadIdentityMatches reports whether an agent-reported gamepad entry can
// belong to a local pad. Either side lacking a VID/PID leaves it undecided,
// which counts as a match.
func gamepadIdentityMatches(driveVID, drivePID, deviceVID, devicePID string) bool {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.TrimPrefix(s, "0x")
		return strings.TrimLeft(s, "0")
	}
	if norm(driveVID) == "" && norm(drivePID) == "" || norm(deviceVID) == "" && norm(devicePID) == "" {
		return true
	}
	return norm(driveVID) == norm(deviceVID) && norm(drivePID) == norm(devicePID)
}
