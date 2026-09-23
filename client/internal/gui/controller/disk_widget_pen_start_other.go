//go:build !(js && wasm)

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
// sequence LiSendPenEvent expects (see forwardPenState). maxX/maxY/
// maxPressure are resolved once per device via platform.PenRangeFor at
// capture start -- different Wacom models sharing the same byte layout
// still report very different logical-max ranges (see PenRangeFor's doc
// comment), so this can't be the package-level PenMaxX/Y/Pressure
// constants shared across every device.
type penCaptureTracker struct {
	inRange bool
	tipDown bool

	maxX, maxY  uint32
	maxPressure uint16
}

// startPenCapture opens native OS-level pen tablet capture (macOS's IOKit
// tap today, see pen_capture_darwin.go) and forwards decoded state to the
// host via the active Moonlight session (forwardPenState) -- the web build
// has neither and instead exports a synthetic USB/IP Wacom tablet from the
// agent (disk_widget_pen_start_wasm.go).
func (dw *DiskWidget) startPenCapture(t platform.PenTabletInfo) (penCaptureHandle, error) {
	maxX, maxY, maxPressure := platform.PenRangeFor(t.PID)
	tracker := &penCaptureTracker{maxX: maxX, maxY: maxY, maxPressure: maxPressure}
	id := t.ID
	return platform.StartPenCapture(t.ID, func(state platform.PenCaptureState) {
		dw.forwardPenState(id, tracker, state)
	})
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

	x := float32(state.X) / float32(tracker.maxX)
	y := float32(state.Y) / float32(tracker.maxY)
	pressure := float32(state.Pressure) / float32(tracker.maxPressure)

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
// stream is connected.
func (dw *DiskWidget) moonlightSender() service.MoonlightInputSender {
	if dw.moonlightProvider == nil {
		return nil
	}
	return dw.moonlightProvider()
}
