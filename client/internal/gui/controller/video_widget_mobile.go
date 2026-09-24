//go:build android || ios

package controller

import (
	"math"
	"sync/atomic"
)

// imeExpandBits stores math.Float32bits(imeHeightDp) atomically. Non-zero
// means the system IME is open and the video should expand above the soft
// keyboard. Special-keys overlay does not set this.
var imeExpandBits atomic.Int32

func setImeExpandHeightDp(h float32) {
	imeExpandBits.Store(int32(math.Float32bits(h)))
}

func getImeExpandHeightDp() float32 {
	return math.Float32frombits(uint32(imeExpandBits.Load()))
}

var lastImeHeightBits atomic.Int32

func rememberImeHeightDp(h float32) {
	if h > 100 {
		lastImeHeightBits.Store(int32(math.Float32bits(h)))
	}
}

func predictedImeHeightDp(canvasH float32) float32 {
	h := math.Float32frombits(uint32(lastImeHeightBits.Load()))
	if h > 100 {
		return h
	}
	if canvasH > 0 {
		return canvasH * 0.42
	}
	return 340
}

func (vw *VideoWidget) applyImmediateKeyboardViewport() {
	if vw == nil {
		return
	}
	open := vw.IsVirtualKeyboardVisible() || vw.IsSystemIMESticky()
	if open {
		if !imeCropsVideoOverlay() {
			// Landscape: system IME floats as a widget; keep video down to
			// the footer only (no IME bottom crop / viewport lift).
			setImeExpandHeightDp(0)
			vw.bottomInset = 0
			vw.keyboardViewportLift = false
			vw.recalculateViewport()
		} else {
			var canvasH float32
			if vw.parentWindow != nil {
				canvasH = vw.parentWindow.Canvas().Size().Height
			}
			h := predictedImeHeightDp(canvasH)
			if cur := getImeExpandHeightDp(); cur > 100 {
				h = cur
			}
			setImeExpandHeightDp(h)
			vw.syncKeyboardBottomInsetFromIME(h)
			vw.focusViewportOnVirtualCursorForKeyboard()
		}
	} else {
		setImeExpandHeightDp(0)
		vw.bottomInset = 0
		vw.keyboardViewportLift = false
		vw.recalculateViewport()
	}
	vw.platformAfterKeyboardViewportSettle()
	vw.InvalidateOverlayGeometry()
	vw.forceCanvasRefresh.Store(true)
}

func (vw *VideoWidget) platformHandleVirtualKeyboard() {
	if vw.IsVirtualKeyboardVisible() {
		vw.hideSpecialKeysOverlay()
		vw.setKeyboardCollapseFABVisible(vw.IsSystemIMESticky())
		return
	}
	vw.showSpecialKeysOverlay()
	vw.setKeyboardCollapseFABVisible(true)
}

func (vw *VideoWidget) platformShowVirtualKeyboardIfMobile() {
	// Compact panel is toggled from the Control footer keyboard button.
}
