package controller

import (
	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (fd *FullscreenDialog) handleKeyDown(event *fyne.KeyEvent) {
	if event == nil {
		return
	}
	logrus.Infof("⌨️ [FS][INPUT][DOWN] key=%q physical=%+v", event.Name, event.Physical)
	if mask := modifierMaskForKeyName(event.Name); mask != 0 {
		for {
			current := fd.keyboardModifierState.Load()
			next := current | mask
			if fd.keyboardModifierState.CompareAndSwap(current, next) {
				logrus.Infof("⌨️ [FS][INPUT][DOWN] modifiers=%d", next)
				break
			}
		}
	}
	if fd.videoWidget != nil {
		fd.videoWidget.handlePhysicalKeyDown(event)
	}
}

func (fd *FullscreenDialog) handleKeyUp(event *fyne.KeyEvent) {
	if event == nil {
		return
	}
	logrus.Infof("⌨️ [FS][INPUT][UP] key=%q physical=%+v", event.Name, event.Physical)
	if mask := modifierMaskForKeyName(event.Name); mask != 0 {
		for {
			current := fd.keyboardModifierState.Load()
			next := current &^ mask
			if fd.keyboardModifierState.CompareAndSwap(current, next) {
				logrus.Infof("⌨️ [FS][INPUT][UP] modifiers=%d", next)
				break
			}
		}
	}
	if fd.videoWidget != nil {
		fd.videoWidget.handlePhysicalKeyUp(event)
	}
}

// handleKeyPress обрабатывает TypedKey в полноэкранном режиме.
// Всегда подавляем USB-путь; клавиши идут через handleKeyDown/Up → Moonlight.
// TypedKey используется ТОЛЬКО для выхода из полноэкранного режима и как фолбек
// (через canvas.SetOnTypedKey) когда touchpadWrapper ещё не получил фокус.
func (fd *FullscreenDialog) handleKeyPress(event *fyne.KeyEvent) {
	if event == nil {
		return
	}
	if event.Name == fyne.KeyEscape || event.Name == fyne.KeyF11 || string(event.Name) == "Back" {
		logrus.Info("⌨️ [FS][INPUT][PRESS] leaving fullscreen")
		fd.exitFullscreen()
		return
	}
	// Suppress TypedKey when touchpadWrapper has focus: KeyDown/Up handles Moonlight input.
	// When no focus yet (500ms async delay after fullscreen opens), allow fallback via canvas.
	if fd.videoWidget != nil && fd.videoWidget.moonlightInput() != nil {
		if fd.touchpadWrapper != nil && fd.fullscreenWindow != nil &&
			fd.fullscreenWindow.Canvas().Focused() == fd.touchpadWrapper {
			logrus.Debugf("⌨️ [FS][INPUT][PRESS] skip — Moonlight+focus active")
			return
		}
		// No focus yet — forward via the video widget so Moonlight receives the key.
		logrus.Debugf("⌨️ [FS][INPUT][PRESS] Moonlight but no focus — forwarding via VideoWidget")
		if fd.videoWidget != nil {
			fd.videoWidget.handlePhysicalKeyPress(event)
		}
		return
	}
}

// handleRunePress — TypedRune больше не используется для отправки ввода.
func (fd *FullscreenDialog) handleRunePress(_ rune) {}

// handleVirtualKeyPress обрабатывает нажатие клавиш виртуальной клавиатуры через Moonlight.
func (fd *FullscreenDialog) handleVirtualKeyPress(keyCode int, modifiers int) {
	logrus.Infof("⌨️ [FS][VIRTUAL] keyCode=%d modifiers=%d", keyCode, modifiers)
	mi := fd.videoWidget.moonlightInput()
	if mi == nil {
		logrus.Warn("⌨️ [FS][VIRTUAL] Moonlight not active, ignoring virtual key")
		return
	}
	_ = keyCode
	_ = modifiers
}
