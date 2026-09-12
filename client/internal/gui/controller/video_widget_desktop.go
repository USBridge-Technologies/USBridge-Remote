//go:build !android && !ios && !(js && wasm)

package controller

import (
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
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
	if vw.contentContainer == nil {
		return
	}
	if vw.virtualKeyboard == nil {
		if vw.parentWindow == nil {
			logrus.Warn("⚠️ Parent window is not set")
			return
		}
		vw.virtualKeyboard = graphics.NewVirtualKeyboard(vw.parentWindow, vw.handleVirtualKeyPress, vw.handlePhysicalRunePress)
	}

	if vw.virtualKeyboard.IsVisible() {
		vw.virtualKeyboard.Hide()
		vw.contentContainer.Hide()
		vw.container.Refresh()
		vw.forceCanvasRefresh.Store(true)
		logrus.Info("⌨️ Virtual keyboard hidden (embedded)")
		return
	}

	keyboardLayout := vw.virtualKeyboard.GetKeyboardLayout()
	vw.virtualKeyboard.SetVisibleState(true)
	canvasSize := vw.parentWindow.Canvas().Size()
	keyboardLayout.Resize(fyne.NewSize(canvasSize.Width, keyboardLayout.MinSize().Height))
	keyboardLayout.Move(fyne.NewPos(0, 0))
	vw.contentContainer.Objects = []fyne.CanvasObject{keyboardLayout}
	vw.contentContainer.Resize(keyboardLayout.Size())
	vw.contentContainer.Show()
	vw.container.Refresh()
	vw.forceCanvasRefresh.Store(true)
	logrus.Info("⌨️ Virtual keyboard shown (embedded compact panel)")
}

func (vw *VideoWidget) platformShowVirtualKeyboardIfMobile() {
	// Not applicable for desktop, only show by default on mobile
}
