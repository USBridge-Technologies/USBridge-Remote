//go:build !android && !ios

package controller

import (
	"usbridge-client/internal/gui/graphics"

	"github.com/sirupsen/logrus"
)

func (vw *VideoWidget) platformRegisterGestureTarget() {
	// На десктопе не требуется регистрация нативных жестов
}

func (vw *VideoWidget) platformHandleVirtualKeyboard() {
	if vw.virtualKeyboard == nil {
		if vw.parentWindow == nil {
			logrus.Warn("⚠️ Parent window is not set")
			return
		}
		vw.virtualKeyboard = graphics.NewVirtualKeyboard(vw.parentWindow, vw.handleVirtualKeyPress, vw.handlePhysicalRunePress)
		vw.virtualKeyboard.SetOnTextTyped(func(text string) {
			if vw.usbClient != nil {
				if err := vw.usbClient.SendText(text); err != nil {
					logrus.Errorf("⚠️ Ошибка отправки текста: %v", err)
				}
			}
		})
	}

	if vw.virtualKeyboard.IsVisible() {
		vw.virtualKeyboard.Hide()
		logrus.Info("⌨️ Virtual keyboard hidden (desktop mode)")
	} else {
		vw.virtualKeyboard.ShowInSeparateWindow()
		logrus.Info("⌨️ Virtual keyboard shown in a separate window (desktop mode)")
	}
}
