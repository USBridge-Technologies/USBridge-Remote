package controller

import (
	"time"
	"usbridge-client/internal/input"

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
				return
			}
		}
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
				return
			}
		}
	}
}

// handleVirtualKeyPress обрабатывает нажатие клавиш виртуальной клавиатуры
func (fd *FullscreenDialog) handleVirtualKeyPress(keyCode int, modifiers int) {
	logrus.Infof("⌨️ Виртуальная клавиатура: получено нажатие клавиши %d с модификаторами %d", keyCode, modifiers)

	if fd.usbClient == nil {
		logrus.Warnf("⌨️ USB клиент не подключен, игнорируем клавишу: %d", keyCode)
		return
	}

	logrus.Infof("⌨️ Отправляем клавишу на удаленную машину: код=%d, модификаторы=%d", keyCode, modifiers)
	fd.sendKeyToRemoteVirtual(keyCode, modifiers)
}

// sendKeyToRemoteVirtual отправляет клавишу на удаленную машину через HID (из виртуальной клавиатуры)
func (fd *FullscreenDialog) sendKeyToRemoteVirtual(keyCode int, modifiers int) {
	logrus.Infof("⌨️ sendKeyToRemoteVirtual: отправка клавиши %d с модификаторами %d", keyCode, modifiers)

	var err error
	if modifiers > 0 {
		err = fd.usbClient.SendCombo(modifiers, keyCode)
		logrus.Infof("⌨️ Отправлена комбинация: модификаторы=%d, клавиша=%d", modifiers, keyCode)
	} else {
		logrus.Infof("⌨️ Отправляем одиночную клавишу: %d", keyCode)
		err = fd.usbClient.SendKey(keyCode)
		logrus.Infof("⌨️ Отправлена клавиша: %d - результат: %v", keyCode, err)
	}

	if err != nil {
		logrus.Errorf("⚠️ Ошибка отправки клавиши: %v", err)
	} else {
		logrus.Infof("✅ Клавиша успешно отправлена: код=%d, модификаторы=%d", keyCode, modifiers)
	}
}

// handleKeyPress обрабатывает нажатие клавиш в полноэкранном режиме.
func (fd *FullscreenDialog) handleKeyPress(event *fyne.KeyEvent) {
	if event == nil {
		return
	}
	logrus.Infof("⌨️ [FS][INPUT][PRESS] key=%q physical=%+v printable=%v modifiers=%d", event.Name, event.Physical, input.IsPrintableKey(event.Name), int(fd.keyboardModifierState.Load()))

	// Handle Back button for Android
	if event.Name == fyne.KeyEscape || event.Name == fyne.KeyF11 || string(event.Name) == "Back" {
		logrus.Info("⌨️ [FS][INPUT][PRESS] leaving fullscreen")
		fd.exitFullscreen()
		return
	}

	if input.IsPrintableKey(event.Name) {
		if isDesktopPrintableKeyFallbackEnabled() && fd.sendKeyToRemote(event, int(fd.keyboardModifierState.Load())) {
			fd.suppressRuneUntilNS.Store(time.Now().Add(desktopPrintableRuneSuppressWindow).UnixNano())
			logrus.Info("⌨️ [FS][INPUT][PRESS] printable key sent via TypedKey fallback")
		}
		return
	}

	if !fd.keyboardEnabled || fd.usbClient == nil {
		logrus.Warnf("⌨️ [FS][INPUT][PRESS] ignored key=%q keyboardEnabled=%v usb=%v", event.Name, fd.keyboardEnabled, fd.usbClient != nil)
		return
	}

	fd.sendKeyToRemote(event, 0)
}

// sendKeyToRemote отправляет клавишу на удаленную машину через HID.
func (fd *FullscreenDialog) sendKeyToRemote(event *fyne.KeyEvent, modifiers int) bool {
	keyCode := input.GetKeyCodeFromPhysical(event.Physical)
	if keyCode == 0 {
		keyCode = input.GetKeyCode(event.Name)
	}
	if keyCode == 0 {
		logrus.Warnf("⌨️ [FS][INPUT][SEND-KEY] unresolved key=%q physical=%+v", event.Name, event.Physical)
		return false
	}
	logrus.Infof("⌨️ [FS][INPUT][SEND-KEY] key=%q hid=%d modifiers=%d", event.Name, keyCode, modifiers)
	var err error
	if modifiers != 0 {
		err = fd.usbClient.SendCombo(modifiers, keyCode)
	} else {
		err = fd.usbClient.SendKey(keyCode)
	}
	if err != nil {
		logrus.Errorf("⚠️ Ошибка отправки клавиши: %v", err)
		return false
	}
	logrus.Infof("⌨️ [FS][INPUT][SEND-KEY] delivered hid=%d modifiers=%d", keyCode, modifiers)
	return true
}

// handleRunePress обрабатывает нажатие символов (буквы, цифры, знаки препинания)
func (fd *FullscreenDialog) handleRunePress(r rune) {
	logrus.Infof("⌨️ [FS][INPUT][RUNE] rune=%q code=%U", string(r), r)

	if !fd.keyboardEnabled || fd.usbClient == nil {
		logrus.Warnf("⌨️ [FS][INPUT][RUNE] ignored keyboardEnabled=%v usb=%v", fd.keyboardEnabled, fd.usbClient != nil)
		return
	}

	if isDesktopPrintableKeyFallbackEnabled() && time.Now().UnixNano() <= fd.suppressRuneUntilNS.Load() {
		logrus.Info("⌨️ [FS][INPUT][RUNE] suppressed duplicate after TypedKey fallback")
		return
	}

	fd.sendRuneToRemote(r)
}

// sendRuneToRemote отправляет символ на удаленную машину через HID
func (fd *FullscreenDialog) sendRuneToRemote(r rune) {
	if r == '\n' || r == '\r' {
		logrus.Info("⌨️ [FS][INPUT][SEND-RUNE] ignored newline")
		return
	}
	keyCode, modifiers := input.GetRuneKeyCodeWithModifiers(r)
	if keyCode == 0 {
		logrus.Warnf("⌨️ [FS][INPUT][SEND-RUNE] unresolved rune=%q code=%U", string(r), r)
		return
	}
	logrus.Infof("⌨️ [FS][INPUT][SEND-RUNE] rune=%q hid=%d modifiers=%d", string(r), keyCode, modifiers)

	var err error
	if modifiers > 0 {
		err = fd.usbClient.SendCombo(modifiers, keyCode)
	} else {
		err = fd.usbClient.SendKey(keyCode)
	}

	if err != nil {
		logrus.Errorf("⚠️ Ошибка отправки символа: %v", err)
	} else {
		logrus.Infof("⌨️ [FS][INPUT][SEND-RUNE] delivered hid=%d modifiers=%d rune=%q", keyCode, modifiers, string(r))
	}
}
