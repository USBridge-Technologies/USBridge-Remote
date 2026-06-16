//go:build android || ios

package controller

import (
	"sync"
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/service"

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
		// IME runes go through onRuneTyped → runeToMoonlightVK → LiSendKeyboardEvent.
		// This uses the same Sunshine evdev → bridgeKeyboard → /dev/hid_k path that
		// works for physical keyboards on Mac/desktop. LiSendUtf8TextEvent is NOT used
		// because the server reads only from the "Keyboard passthrough" evdev device and
		// has no handler for the CTRL_CHANNEL_UTF8 control message.
		vw.virtualKeyboard = graphics.NewVirtualKeyboard(vw.parentWindow, vw.handleVirtualKeyPress, func(r rune) {
			vk, mods := runeToMoonlightVK(r)
			if vk == 0 {
				logrus.Debugf("⌨️ [IME] rune %q (U+%04X): no VK mapping, skipped", r, r)
				return
			}
			mi := vw.moonlightInput()
			if mi == nil || !mi.IsInputActive() {
				return
			}
			mi.SendMoonlightKey(vk, service.LiKeyActionDown, mods)
			mi.SendMoonlightKey(vk, service.LiKeyActionUp, mods)
		})
		
		// Когда Android IME открывается/закрывается, расширяем Vulkan вверх под вкладки.
		vw.virtualKeyboard.SetOnIMEChanged(func(imeHeightDp float32) {
			fyne.Do(func() {
				kl := vw.virtualKeyboard.GetKeyboardLayout()
				newH := kl.MinSize().Height
				canvasW := vw.parentWindow.Canvas().Size().Width
				kl.Resize(fyne.NewSize(canvasW, newH))
				vw.contentContainer.Objects = []fyne.CanvasObject{kl}
				vw.contentContainer.Resize(kl.Size())
				vw.container.Refresh()
				// Set expand flag AFTER layout so videoCanvasFrame reads the updated contentContainer size.
				vw.onIMEHeightChanged(imeHeightDp)
			})
		})
	}

	if vw.virtualKeyboard.IsVisible() {
		vw.virtualKeyboard.Hide()
		vw.contentContainer.Hide()
		vw.container.Refresh()
		// Vulkan rect will be restored on the next render tick via updateMetalVideoFrame.
		vw.forceCanvasRefresh.Store(true)
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
		// Shrink Vulkan SurfaceView above the keyboard on the next render tick.
		vw.forceCanvasRefresh.Store(true)
		logrus.Info("⌨️ Virtual keyboard shown with Android IME")
	}
}
