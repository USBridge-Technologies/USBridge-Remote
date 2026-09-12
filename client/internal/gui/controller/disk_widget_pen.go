package controller

import (
	"sync/atomic"

	"usbridge-client/internal/platform"
	"usbridge-client/internal/service"

	"github.com/sirupsen/logrus"
)

// LI_TOOL_TYPE_*, LI_PEN_BUTTON_*, LI_TOUCH_EVENT_*, LI_TILT_UNKNOWN,
// LI_ROT_UNKNOWN -- mirrors Limelight.h exactly (LiSendPenEvent's own
// parameter contract), duplicated here rather than imported since these are
// C #defines with no cgo-free Go binding.
const (
	liToolTypePen    = 0x01
	liToolTypeEraser = 0x02

	liPenButtonSecondary = 0x02 // mapped from the tablet's lower barrel button
	liPenButtonTertiary  = 0x04 // mapped from the tablet's upper barrel button

	liTouchEventHover     = 0x00
	liTouchEventDown      = 0x01
	liTouchEventUp        = 0x02
	liTouchEventMove      = 0x03
	liTouchEventCancelAll = 0x07

	liTiltUnknown = 0xFF
	liRotUnknown  = 0xFFFF
)

// penCaptureTracker holds the per-device state needed to turn a stream of
// absolute PenCaptureState samples into the edge-triggered DOWN/UP/HOVER/MOVE
// sequence LiSendPenEvent expects (see forwardPenState).
type penCaptureTracker struct {
	inRange bool
	tipDown bool
}

// syncPenCaptures compares currently connected Wacom-family pen tablets
// against the set of active captures and starts/stops captures accordingly.
// Unlike syncGamepadCaptures, there is no "mount" step here — a pen tablet
// forwards the instant it's plugged in and a Moonlight session is active,
// the same way built-in keyboard/mouse capture already works.
func (dw *DiskWidget) syncPenCaptures() {
	if dw.activePenCaptures == nil {
		dw.activePenCaptures = make(map[string]*platform.PenCapture)
	}

	tablets := platform.ListPenTablets()
	wanted := make(map[string]bool, len(tablets))
	for _, t := range tablets {
		wanted[t.ID] = true
	}

	for id, cap := range dw.activePenCaptures {
		if !wanted[id] {
			logrus.Infof("🖊️ [PEN] stopping capture for %s", id)
			cap.Stop()
			delete(dw.activePenCaptures, id)
		}
	}

	for _, t := range tablets {
		if _, ok := dw.activePenCaptures[t.ID]; ok {
			continue
		}
		logrus.Infof("🖊️ [PEN] starting capture for %s (%s, vid=%04x pid=%04x)", t.ID, t.Name, t.VID, t.PID)
		tracker := &penCaptureTracker{}
		capturedID := t.ID
		cap, err := platform.StartPenCapture(t.ID, func(state platform.PenCaptureState) {
			dw.forwardPenState(capturedID, tracker, state)
		})
		if err != nil {
			logrus.Warnf("🖊️ [PEN] capture failed for %s: %v", t.ID, err)
			continue
		}
		dw.activePenCaptures[t.ID] = cap
	}
}

var penLogSeq atomic.Uint64

// forwardPenState converts one absolute PenCaptureState sample into a
// LiSendPenEvent call, deriving the DOWN/MOVE/UP/HOVER event type from the
// previous sample's contact/proximity state (tracker). Called from the
// capture's own goroutine, not the Fyne UI thread.
func (dw *DiskWidget) forwardPenState(id string, tracker *penCaptureTracker, state platform.PenCaptureState) {
	sender := dw.moonlightSender()
	if sender == nil || !sender.IsInputActive() {
		if seq := penLogSeq.Add(1); seq%300 == 1 {
			logrus.Warnf("🖊️ [PEN] Moonlight not active — pen input dropped id=%s", id)
		}
		return
	}

	var eventType uint8
	switch {
	case tracker.inRange && !state.InRange:
		eventType = liTouchEventCancelAll
	case state.TipSwitch && !tracker.tipDown:
		eventType = liTouchEventDown
	case !state.TipSwitch && tracker.tipDown:
		eventType = liTouchEventUp
	case state.TipSwitch:
		eventType = liTouchEventMove
	default:
		eventType = liTouchEventHover
	}
	tracker.inRange = state.InRange
	tracker.tipDown = state.TipSwitch

	toolType := uint8(liToolTypePen)
	if state.Eraser {
		toolType = liToolTypeEraser
	}

	var penButtons uint8
	if state.Button1 {
		penButtons |= liPenButtonSecondary
	}
	if state.Button2 {
		penButtons |= liPenButtonTertiary
	}

	x := float32(state.X) / float32(platform.PenMaxX)
	y := float32(state.Y) / float32(platform.PenMaxY)
	pressure := float32(state.Pressure) / float32(platform.PenMaxPressure)

	tilt := uint8(liTiltUnknown)
	if state.TiltX != 0 || state.TiltY != 0 {
		mag := combinedTiltDegrees(state.TiltX, state.TiltY)
		if mag > 90 {
			mag = 90
		}
		tilt = mag
	}

	rotation := uint16(liRotUnknown)
	if state.Rotation != 0 {
		rotation = uint16(state.Rotation)
	}

	sender.SendMoonlightPenEvent(eventType, toolType, penButtons, x, y, pressure, rotation, tilt)
}

// combinedTiltDegrees combines Wacom's separate TiltX/TiltY (each already in
// degrees off vertical) into the single 0..90 magnitude LiSendPenEvent's
// `tilt` parameter carries — Moonlight's wire protocol has no separate X/Y
// tilt axes, only the barrel-rotation-independent overall lean angle a real
// Sunshine host would get from a Wintab-style API.
func combinedTiltDegrees(tiltX, tiltY int8) uint8 {
	x := float64(tiltX)
	y := float64(tiltY)
	mag := x*x + y*y
	// Integer sqrt via a small loop would be overkill; degrees are small
	// enough (<=90) that float64 math.Sqrt precision is a non-issue.
	root := isqrt(mag)
	if root > 90 {
		root = 90
	}
	return uint8(root)
}

func isqrt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	// Newton's method, a handful of iterations is plenty for our <=180^2 range.
	x := v
	for i := 0; i < 8; i++ {
		x = 0.5 * (x + v/x)
	}
	return x
}

// moonlightSender returns the active MoonlightInputSender, or nil if no
// stream is connected. Shared with gamepad forwarding's inline check.
func (dw *DiskWidget) moonlightSender() service.MoonlightInputSender {
	if dw.moonlightProvider == nil {
		return nil
	}
	return dw.moonlightProvider()
}

// stopAllPenCaptures stops every active pen capture; called on disconnect.
func (dw *DiskWidget) stopAllPenCaptures() {
	for id, cap := range dw.activePenCaptures {
		logrus.Infof("🖊️ [PEN] stopping capture (disconnect) for %s", id)
		cap.Stop()
		delete(dw.activePenCaptures, id)
	}
}
