//go:build android || ios

package controller

import (
	"math"
	"strings"
	"sync/atomic"

	"usbridge-client/internal/service"

	"github.com/sirupsen/logrus"
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
			// Android: inset the viewport above the IME. iOS: Fyne already
			// shrinks the canvas — syncKeyboardBottomInsetFromIME would
			// double-count and thrash Metal.
			vw.platformSyncKeyboardBottomInsetFromIME(h)
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

func (vw *VideoWidget) ensureIMEKeyboardTarget() {
	vw.ensureMobileVirtualKeyboard()
	if vw.virtualKeyboard != nil {
		vw.virtualKeyboard.RegisterAsIMETarget()
	}
}

// handleNativeIMEText applies sticky soft-IME diffs from the platform bridge
// (Android KeyboardBridge / iOS UITextField).
func (vw *VideoWidget) handleNativeIMEText(deleteCount int, text string) {
	mi := vw.moonlightInput()
	if mi == nil {
		return
	}
	logrus.Infof("⌨️ [IME-TEXT] del=%d add=%q", deleteCount, text)
	for i := 0; i < deleteCount; i++ {
		vw.enqueueSend(func() {
			mi.SendMoonlightKey(0x08, service.LiKeyActionDown, 0)
			mi.SendMoonlightKey(0x08, service.LiKeyActionUp, 0)
		})
	}
	if text == "" {
		return
	}
	// The IME's Enter comes as a newline (UTF-8 "\n" types nothing):
	// press the same Enter as the special keys row.
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		rest := strings.TrimPrefix(text[i:], "\r")
		vw.handleNativeIMEText(0, text[:i])
		vw.handleVirtualKeyPress(40, 0)
		vw.handleNativeIMEText(0, strings.TrimPrefix(rest, "\n"))
		return
	}
	if vw.TypesAsUSBKeyboard() {
		// A KVM can't type UTF-8 text: press each character's key on the
		// target layout (auto: the IME's language), UTF-8 only for the rest.
		for _, r := range text {
			if !vw.sendRuneViaRemoteLayout(r) {
				s := string(r)
				vw.enqueueSend(func() { mi.SendMoonlightUtf8Text(s) })
			}
		}
		return
	}
	t := text
	vw.enqueueSend(func() { mi.SendMoonlightUtf8Text(t) })
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
