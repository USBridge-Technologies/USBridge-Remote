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
