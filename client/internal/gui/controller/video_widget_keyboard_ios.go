//go:build ios

package controller

import (
	"time"

	"fyne.io/fyne/v2"
	"usbridge-client/internal/gui/graphics"
)

func platformSetupKeyboardWindow(_ *graphics.VirtualKeyboard) {}

func (vw *VideoWidget) platformSetSystemIMESticky(on bool) {
	was := vw.systemIMESticky.Load()
	vw.systemIMESticky.Store(on)
	vw.ensureVirtualKeyboard()
	if on {
		if vw.virtualKeyboard != nil {
			// Entry focus owns the iOS soft keyboard — keep re-asserting it
			// when video touches would otherwise steal focus and dismiss IME.
			vw.virtualKeyboard.SetKeepIMEFocus(true)
			vw.virtualKeyboard.RegisterAsIMETarget()
			vw.virtualKeyboard.FocusInput()
			// Focus can race layout; re-assert after the soft keyboard animates in.
			time.AfterFunc(200*time.Millisecond, func() {
				fyne.Do(func() {
					if !vw.systemIMESticky.Load() || vw.virtualKeyboard == nil {
						return
					}
					vw.virtualKeyboard.FocusInput()
					vw.InvalidateOverlayGeometry()
					vw.forceCanvasRefresh.Store(true)
				})
			})
		}
		vw.InvalidateOverlayGeometry()
		vw.forceCanvasRefresh.Store(true)
		return
	}
	if was && vw.virtualKeyboard != nil {
		vw.virtualKeyboard.SetKeepIMEFocus(false)
		vw.virtualKeyboard.BlurInput()
	}
	setImeExpandHeightDp(0)
	vw.InvalidateOverlayGeometry()
	vw.forceCanvasRefresh.Store(true)
}

func (vw *VideoWidget) platformAfterKeyboardViewportSettle() {}

// refocusStickySystemIME keeps the soft keyboard Entry focused while sticky.
// TouchpadWrapper gains focus on video drag; bouncing focus back prevents
// iOS from collapsing the system IME until the special-keys dismiss button.
func (vw *VideoWidget) refocusStickySystemIME() bool {
	if vw == nil || vw.virtualKeyboard == nil {
		return false
	}
	vw.virtualKeyboard.FocusInput()
	return true
}
