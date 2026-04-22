//go:build android || ios

package controller

import (
	"sync"
	"usbridge-client/internal/gui/graphics"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

var (
	activeMobileGestureTargetMu sync.RWMutex
	activeMobileGestureTarget   *VideoWidget
)

func activeGestureVideoWidget() *VideoWidget {
	activeMobileGestureTargetMu.RLock()
	defer activeMobileGestureTargetMu.RUnlock()
	return activeMobileGestureTarget
}

func (vw *VideoWidget) platformRegisterGestureTarget() {
	activeMobileGestureTargetMu.Lock()
	activeMobileGestureTarget = vw
	activeMobileGestureTargetMu.Unlock()
}

func (vw *VideoWidget) platformHandleVirtualKeyboard() {
	if vw.virtualKeyboard == nil {
		if vw.parentWindow == nil {
			logrus.Warn("⚠️ Parent window is not set")
			return
		}
		// Используем handlePhysicalRunePress для мобилок, так как маппинг в нем теперь адаптивный
		vw.virtualKeyboard = graphics.NewVirtualKeyboard(vw.parentWindow, vw.handleVirtualKeyPress, vw.handlePhysicalRunePress)
		
		// Когда Android IME открывается/закрывается, обновляем layout:
		vw.virtualKeyboard.SetOnIMEChanged(func(open bool) {
			fyne.Do(func() {
				kl := vw.virtualKeyboard.GetKeyboardLayout()
				newH := kl.MinSize().Height
				canvasW := vw.parentWindow.Canvas().Size().Width
				kl.Resize(fyne.NewSize(canvasW, newH))
				vw.contentContainer.Objects = []fyne.CanvasObject{kl}
				vw.contentContainer.Resize(kl.Size())
				vw.container.Refresh()
			})
		})
	}

	if vw.virtualKeyboard.IsVisible() {
		vw.virtualKeyboard.Hide()
		vw.contentContainer.Hide()
		vw.container.Refresh()
		logrus.Info("⌨️ Virtual keyboard hidden (Android mode)")
	} else {
		// Регистрируем как получателя нативных Android IME-событий
		vw.virtualKeyboard.RegisterAsIMETarget()

		keyboardLayout := vw.virtualKeyboard.GetKeyboardLayout()
		vw.virtualKeyboard.SetVisibleState(true)
		vw.virtualKeyboard.FocusInput() // Принудительный фокус для Android

		canvasSize := vw.parentWindow.Canvas().Size()
		keyboardLayout.Resize(fyne.NewSize(canvasSize.Width, keyboardLayout.MinSize().Height))
		keyboardLayout.Move(fyne.NewPos(0, 0))

		vw.contentContainer.Objects = []fyne.CanvasObject{keyboardLayout}
		vw.contentContainer.Resize(keyboardLayout.Size())
		vw.contentContainer.Show()
		vw.container.Refresh()
		logrus.Info("⌨️ Virtual keyboard shown with Android IME")
	}
}
