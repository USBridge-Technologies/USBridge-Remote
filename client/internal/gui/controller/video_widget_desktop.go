//go:build !android && !ios && !(js && wasm)

package controller

import (
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/view"

	"github.com/sirupsen/logrus"
)

func (vw *VideoWidget) platformRegisterGestureTarget() {
	// Native gesture registration is not needed on desktop
}

func (vw *VideoWidget) platformHandleVirtualKeyboard() {
	if view.IsMobile() {
		vw.toggleEmbeddedVirtualKeyboard()
		return
	}
	if vw.virtualKeyboard == nil {
		if vw.parentWindow == nil {
			logrus.Warn("⚠️ Parent window is not set")
			return
		}
		vw.virtualKeyboard = graphics.NewVirtualKeyboard(vw.parentWindow, vw.handleVirtualKeyPress, vw.handlePhysicalRunePress)
		platformSetupKeyboardWindow(vw.virtualKeyboard)
	}

	if vw.virtualKeyboard.IsVisible() {
		vw.virtualKeyboard.Hide()
		logrus.Info("⌨️ Virtual keyboard hidden (desktop mode)")
	} else {
		vw.virtualKeyboard.ShowInSeparateWindow()
		logrus.Info("⌨️ Virtual keyboard shown in a separate window (desktop mode)")
	}
}

func (vw *VideoWidget) toggleEmbeddedVirtualKeyboard() {
	if vw.IsVirtualKeyboardVisible() {
		vw.hideSpecialKeysOverlay()
		vw.setKeyboardCollapseFABVisible(false)
		return
	}
	vw.showSpecialKeysOverlay()
	vw.setKeyboardCollapseFABVisible(true)
}

func (vw *VideoWidget) platformShowVirtualKeyboardIfMobile() {
	// Not applicable for desktop, only show by default on mobile
}

func (vw *VideoWidget) platformSetSystemIMESticky(on bool) {
	// Desktop / phone-preview: track the flag for footer selected look; no OS IME.
	vw.systemIMESticky.Store(on)
}

func (vw *VideoWidget) refocusStickySystemIME() bool { return false }

func (vw *VideoWidget) platformAfterKeyboardViewportSettle() {}

func (vw *VideoWidget) applyImmediateKeyboardViewport() {
	if vw == nil {
		return
	}
	vw.InvalidateOverlayGeometry()
	vw.forceCanvasRefresh.Store(true)
}
