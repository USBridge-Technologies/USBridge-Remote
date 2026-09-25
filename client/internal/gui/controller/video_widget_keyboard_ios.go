//go:build ios

package controller

import (
	"time"

	"usbridge-client/internal/gui/graphics"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func platformSetupKeyboardWindow(_ *graphics.VirtualKeyboard) {}

func (vw *VideoWidget) platformSetSystemIMESticky(on bool) {
	if on == vw.systemIMESticky.Load() {
		if on {
			graphics.SetStickySystemIME(true)
			graphics.SetIMETextHandler(vw.handleNativeIMEText)
			vw.ensureIMEKeyboardTarget()
		}
		return
	}
	vw.systemIMESticky.Store(on)
	vw.ensureVirtualKeyboard()
	if on {
		vw.imeStackArmedAt = time.Now()
		vw.ensureIMEKeyboardTarget()
		// Native UITextField owns the soft keyboard (Android-parity). Do not
		// bounce Fyne Entry focus on video drags — that was closing/reopening
		// the IME and thrashing the Metal clip.
		if vw.virtualKeyboard != nil {
			vw.virtualKeyboard.SetKeepIMEFocus(false)
		}
		graphics.SetIMETextHandler(vw.handleNativeIMEText)
		graphics.SetStickySystemIME(true)
		if vw.touchpadWrapper != nil && vw.parentWindow != nil {
			vw.parentWindow.Canvas().Focus(vw.touchpadWrapper)
		}
		vw.InvalidateOverlayGeometry()
		vw.forceCanvasRefresh.Store(true)
		logrus.Info("⌨️ System IME sticky ON (iOS native UITextField)")
		// Insets clear asynchronously via updateConfig — re-measure the
		// special-keys band and Metal clip after Fyne applies zero top pad.
		time.AfterFunc(80*time.Millisecond, func() {
			fyne.Do(func() {
				if !vw.systemIMESticky.Load() {
					return
				}
				if vw.onKeyboardChromeSync != nil {
					vw.onKeyboardChromeSync()
				} else if vw.onKeyboardStackChanged != nil {
					vw.onKeyboardStackChanged()
				}
				vw.InvalidateOverlayGeometry()
				vw.forceCanvasRefresh.Store(true)
			})
		})
		return
	}
	graphics.SetIMETextHandler(nil)
	graphics.SetStickySystemIME(false)
	if vw.virtualKeyboard != nil {
		vw.virtualKeyboard.SetKeepIMEFocus(false)
		vw.virtualKeyboard.BlurInput()
	}
	setImeExpandHeightDp(0)
	vw.InvalidateOverlayGeometry()
	vw.forceCanvasRefresh.Store(true)
	logrus.Info("⌨️ System IME sticky OFF (iOS)")
}

func (vw *VideoWidget) platformAfterKeyboardViewportSettle() {
	imeOpen := getImeExpandHeightDp() > 100 && imeCropsVideoOverlay()
	vw.bottomAnchorContentVertically = imeOpen
}

func (vw *VideoWidget) platformSyncKeyboardBottomInsetFromIME(imeHeightDp float32) {
	// Fyne already shrinks the iOS canvas for the soft keyboard.
	vw.bottomInset = 0
	vw.keyboardViewportLift = false
	_ = imeHeightDp
}

// refocusStickySystemIME: native UITextField holds first responder; reassert
// it if UIKit dropped it, but never bounce Fyne Entry focus (that flickers).
func (vw *VideoWidget) refocusStickySystemIME() bool {
	if vw == nil || !vw.systemIMESticky.Load() {
		return false
	}
	graphics.ReassertStickySystemIME()
	return false
}
