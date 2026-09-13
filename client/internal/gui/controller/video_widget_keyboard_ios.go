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
		vw.virtualKeyboard.BlurInput()
	}
	setImeExpandHeightDp(0)
	vw.InvalidateOverlayGeometry()
	vw.forceCanvasRefresh.Store(true)
}

func (vw *VideoWidget) platformAfterKeyboardViewportSettle() {}
