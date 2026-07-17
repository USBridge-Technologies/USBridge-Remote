//go:build !android && !ios

package controller

import (
	"usbridge-client/internal/gui/graphics"

	"github.com/sirupsen/logrus"
)

func (vw *VideoWidget) platformRegisterGestureTarget() {
	// Native gesture registration is not needed on desktop
}

func (vw *VideoWidget) platformHandleVirtualKeyboard() {
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

func (vw *VideoWidget) platformShowVirtualKeyboardIfMobile() {
	// Not applicable for desktop, only show by default on mobile
}
