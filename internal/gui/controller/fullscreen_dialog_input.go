package controller

import (
	"usbridge-client/internal/input"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

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
	logrus.Infof("⌨️ Получено нажатие клавиши: %s (физическая: %v)", event.Name, event.Physical)

	switch event.Name {
	case fyne.KeyEscape, fyne.KeyF11:
		logrus.Info("🔍 Нажата клавиша выхода из полноэкранного режима")
		fd.exitFullscreen()
		return
	}

	if input.IsPrintableKey(event.Name) {
		return
	}

	if !fd.keyboardEnabled || fd.usbClient == nil {
		logrus.Warnf("⌨️ Клавиатура не подключена, игнорируем клавишу: %s", event.Name)
		return
	}

	fd.sendKeyToRemote(event)
}

// sendKeyToRemote отправляет клавишу на удаленную машину через HID.
func (fd *FullscreenDialog) sendKeyToRemote(event *fyne.KeyEvent) {
	keyCode := input.GetKeyCodeFromPhysical(event.Physical)
	if keyCode == 0 {
		keyCode = input.GetKeyCode(event.Name)
	}
	if keyCode == 0 {
		logrus.Warnf("⌨️ Неизвестная клавиша: %s", event.Name)
		return
	}
	if err := fd.usbClient.SendKey(keyCode); err != nil {
		logrus.Errorf("⚠️ Ошибка отправки клавиши: %v", err)
	}
}

// handleRunePress обрабатывает нажатие символов (буквы, цифры, знаки препинания)
func (fd *FullscreenDialog) handleRunePress(r rune) {
	logrus.Infof("⌨️ Получен символ: %c (код: %d)", r, r)

	if !fd.keyboardEnabled || fd.usbClient == nil {
		logrus.Warnf("⌨️ Клавиатура не подключена, игнорируем символ: %c", r)
		return
	}

	logrus.Infof("⌨️ Отправляем символ на удаленную машину: %c", r)
	fd.sendRuneToRemote(r)
}

// sendRuneToRemote отправляет символ на удаленную машину через HID
func (fd *FullscreenDialog) sendRuneToRemote(r rune) {
	if r == '\n' || r == '\r' {
		return
	}
	logrus.Infof("⌨️ sendRuneToRemote: обработка символа %c (код: %d)", r, r)

	keyCode, modifiers := input.GetRuneKeyCodeWithModifiers(r)
	if keyCode == 0 {
		logrus.Warnf("⌨️ Неизвестный символ: %c (код: %d)", r, r)
		return
	}

	logrus.Infof("⌨️ Найден код клавиши: %d, модификаторы: %d для символа %c", keyCode, modifiers, r)

	var err error
	if modifiers > 0 {
		logrus.Infof("⌨️ Отправляем символ с модификаторами: %d, модификаторы: %d (%c)", keyCode, modifiers, r)
		err = fd.usbClient.SendCombo(modifiers, keyCode)
		logrus.Infof("⌨️ Отправлен символ с модификаторами: %d, модификаторы: %d (%c) - результат: %v", keyCode, modifiers, r, err)
	} else {
		logrus.Infof("⌨️ Отправляем символ: %d (%c)", keyCode, r)
		err = fd.usbClient.SendKey(keyCode)
		logrus.Infof("⌨️ Отправлен символ: %d (%c) - результат: %v", keyCode, r, err)
	}

	if err != nil {
		logrus.Errorf("⚠️ Ошибка отправки символа: %v", err)
	} else {
		logrus.Infof("✅ Символ успешно отправлен: %c (код: %d, модификаторы: %d)", r, keyCode, modifiers)
	}
}
