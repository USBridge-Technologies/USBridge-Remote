package controller

import (
	"time"

	"usbridge-client/internal/input"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// handlePhysicalKeyPress обрабатывает нажатия физической клавиатуры.
func (vw *VideoWidget) handlePhysicalKeyPress(event *fyne.KeyEvent) {
	if vw.usbClient == nil {
		return
	}
	if event.Name == fyne.KeyF11 {
		return
	}
	if input.IsPrintableKey(event.Name) {
		return
	}
	go vw.sendPhysicalKeyToRemote(event)
}

// handlePhysicalRunePress обрабатывает ввод символов с физической клавиатуры.
func (vw *VideoWidget) handlePhysicalRunePress(r rune) {
	if vw.usbClient == nil {
		return
	}
	go vw.sendRuneToRemote(r)
}

// sendPhysicalKeyToRemote конвертирует fyne.KeyEvent в HID и отправляет.
func (vw *VideoWidget) sendPhysicalKeyToRemote(event *fyne.KeyEvent) {
	keyCode := input.GetKeyCodeFromPhysical(event.Physical)
	if keyCode == 0 {
		keyCode = input.GetKeyCode(event.Name)
	}
	if keyCode == 0 {
		return
	}
	if err := vw.usbClient.SendKey(keyCode); err != nil {
		logrus.Errorf("⌨️ Failed to send key: %v", err)
	}
}

// sendRuneToRemote отправляет символ на удалённую машину.
func (vw *VideoWidget) sendRuneToRemote(r rune) {
	if r == '\n' || r == '\r' {
		return
	}
	keyCode, modifiers := input.GetRuneKeyCodeWithModifiers(r)
	if keyCode == 0 {
		return
	}
	var err error
	if modifiers > 0 {
		err = vw.usbClient.SendCombo(modifiers, keyCode)
	} else {
		err = vw.usbClient.SendKey(keyCode)
	}
	if err != nil {
		logrus.Errorf("⌨️ Failed to send rune: %v", err)
	}
}

// handleVirtualKeyPress обрабатывает нажатия виртуальной клавиатуры.
func (vw *VideoWidget) handleVirtualKeyPress(keyCode int, modifiers int) {
	logrus.Infof("⌨️ Virtual keyboard: received key %d with modifiers %d", keyCode, modifiers)

	if vw.usbClient == nil {
		logrus.Warnf("⌨️ USB client is not connected, ignoring key: %d", keyCode)
		return
	}

	logrus.Infof("⌨️ Sending key to remote machine: code=%d, modifiers=%d", keyCode, modifiers)
	go vw.sendKeyToRemote(keyCode, modifiers)
}

// sendKeyToRemote отправляет клавишу на удаленную машину через HID.
func (vw *VideoWidget) sendKeyToRemote(keyCode int, modifiers int) {
	logrus.Infof("⌨️ sendKeyToRemote: sending key %d with modifiers %d", keyCode, modifiers)

	var err error
	if modifiers > 0 {
		err = vw.usbClient.SendCombo(modifiers, keyCode)
		logrus.Infof("⌨️ Combination sent: modifiers=%d, key=%d", modifiers, keyCode)
	} else {
		logrus.Infof("⌨️ Sending single key: %d", keyCode)
		err = vw.usbClient.SendKey(keyCode)
		logrus.Infof("⌨️ Key sent: %d, result=%v", keyCode, err)
	}

	if err != nil {
		logrus.Errorf("⚠️ Failed to send key: %v", err)
	} else {
		logrus.Infof("✅ Key sent successfully: code=%d, modifiers=%d", keyCode, modifiers)
	}
}

// startDesktopMousePolling запускает горутину polling для плавного управления мышью.
func (vw *VideoWidget) startDesktopMousePolling() {
	vw.stopDesktopMousePolling()
	vw.mousePollingQuit = make(chan bool)
	logrus.Info("🖱️ Starting desktop mouse polling (60 FPS)")
	go vw.processDesktopMousePolling()
}

// stopDesktopMousePolling останавливает горутину polling.
func (vw *VideoWidget) stopDesktopMousePolling() {
	if vw.mousePollingQuit != nil {
		close(vw.mousePollingQuit)
		vw.mousePollingQuit = nil
		logrus.Info("🖱️ Desktop mouse polling stopped")
	}
}

// processDesktopMousePolling обрабатывает перемещение мыши с фиксированной частотой.
func (vw *VideoWidget) processDesktopMousePolling() {
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-vw.mousePollingQuit:
			return
		case <-ticker.C:
			vw.processMouseMovement()
		}
	}
}

// processMouseMovement обрабатывает текущее перемещение мыши.
func (vw *VideoWidget) processMouseMovement() {
	if !vw.isMouseConnected || vw.dragButton == 0 {
		return
	}

	if vw.GetMouseInputMode() == "touchscreen" || vw.GetMouseInputMode() == "absolute" {
		return
	}

	rawDx := vw.currentMouseX - vw.lastMouseX
	rawDy := vw.currentMouseY - vw.lastMouseY
	if rawDx == 0 && rawDy == 0 {
		return
	}
	vw.lastMouseX = vw.currentMouseX
	vw.lastMouseY = vw.currentMouseY
	if !vw.isDragging {
		vw.isDragging = true
		logrus.Debugf("🖱️ ✨ Drag/swipe STARTED (desktop touchpad mode, polling)")
	}
	const desktopSensitivity = 1.0
	dx, dy := vw.accumulateRelativeMove(rawDx, rawDy, desktopSensitivity)
	if dx == 0 && dy == 0 {
		return
	}
	if err := vw.usbClient.SendMouseMove(dx, dy); err != nil {
		logrus.Errorf("❌ Error sending mouse move: %v", err)
	}
}

func (vw *VideoWidget) accumulateRelativeMove(rawDx, rawDy, sensitivity float32) (int, int) {
	scaledDx := rawDx*sensitivity + vw.relativeRemainderX
	scaledDy := rawDy*sensitivity + vw.relativeRemainderY

	wholeDx := int(scaledDx)
	wholeDy := int(scaledDy)

	vw.relativeRemainderX = scaledDx - float32(wholeDx)
	vw.relativeRemainderY = scaledDy - float32(wholeDy)

	return clamp(wholeDx, -127, 127), clamp(wholeDy, -127, 127)
}

func (vw *VideoWidget) resetRelativeMoveAccumulator() {
	vw.relativeRemainderX = 0
	vw.relativeRemainderY = 0
}

// GetMouseInputMode возвращает тип манипулятора.
func (vw *VideoWidget) GetMouseInputMode() string {
	if vw.mouseInputMode == "" {
		if fyne.CurrentDevice().IsMobile() {
			vw.mouseInputMode = "mouse"
		} else {
			vw.mouseInputMode = "absolute"
		}
	}
	return vw.mouseInputMode
}

// SetMouseInputMode задаёт тип манипулятора.
func (vw *VideoWidget) SetMouseInputMode(mode string) {
	if mode != "mouse" && mode != "touchscreen" && mode != "absolute" {
		mode = "mouse"
	}
	vw.mouseInputMode = mode
	vw.resetRelativeMoveAccumulator()
	logrus.Debugf("🖱️ Pointer mode: %s", mode)
}

// SendAbsolutePosition отправляет абсолютную позицию с небольшим дебаунсом.
func (vw *VideoWidget) SendAbsolutePosition(x, y int, force bool) {
	if vw.usbClient == nil {
		return
	}
	const deadzone = 2
	const minInterval = 8 * time.Millisecond

	dx := x - vw.lastAbsX
	if dx < 0 {
		dx = -dx
	}
	dy := y - vw.lastAbsY
	if dy < 0 {
		dy = -dy
	}

	if !force {
		if dx < deadzone && dy < deadzone {
			return
		}
		if !vw.lastAbsSentTime.IsZero() && time.Since(vw.lastAbsSentTime) < minInterval {
			return
		}
	}

	vw.lastAbsX = x
	vw.lastAbsY = y
	vw.lastAbsSentTime = time.Now()
	_ = vw.usbClient.SendTouchPositionOnly(x, y, false)
}

// SetAbsoluteButton обновляет битмаску кнопок для absolute режима.
func (vw *VideoWidget) SetAbsoluteButton(button int, pressed bool) {
	var bit uint8
	switch button {
	case 1:
		bit = 0x01
	case 2:
		bit = 0x02
	case 3:
		bit = 0x04
	default:
		return
	}
	if pressed {
		vw.absButtons |= bit
	} else {
		vw.absButtons &^= bit
	}
}

// SendAbsoluteEvent отправляет атомарное абсолютное событие.
func (vw *VideoWidget) SendAbsoluteEvent(x, y int, scroll int, force bool) {
	if vw.usbClient == nil {
		return
	}
	vw.lastAbsX = x
	vw.lastAbsY = y
	vw.lastAbsSentTime = time.Now()
	_ = vw.usbClient.SendAbsoluteEvent(x, y, vw.absButtons, scroll)
}

// CancelTouchDownDelay отменяет отложенную отправку touch(down).
func (vw *VideoWidget) CancelTouchDownDelay() {
	vw.touchDownDelayMu.Lock()
	defer vw.touchDownDelayMu.Unlock()
	if vw.touchDownDelayTimer != nil {
		vw.touchDownDelayTimer.Stop()
		vw.touchDownDelayTimer = nil
	}
}

// StartTouchDownDelay планирует отправку touch(down).
func (vw *VideoWidget) StartTouchDownDelay(x, y int, button int) {
	vw.touchDownDelayMu.Lock()
	if vw.touchDownDelayTimer != nil {
		vw.touchDownDelayTimer.Stop()
		vw.touchDownDelayTimer = nil
	}
	vw.touchDownDelayTimer = time.AfterFunc(120*time.Millisecond, func() {
		vw.touchDownDelayMu.Lock()
		vw.touchDownDelayTimer = nil
		vw.touchDownDelayMu.Unlock()
		vw.touchActive = true
		vw.lastTouchX = x
		vw.lastTouchY = y
		vw.lastTouchDownTime = time.Now()
		if button == 2 {
			_ = vw.usbClient.SendTouchPositionOnly(x, y, true)
		} else {
			_ = vw.usbClient.SendTouch(x, y, true)
		}
	})
	vw.touchDownDelayMu.Unlock()
}

// TryRecordTouchDown записывает «отправляем touch(down)».
func (vw *VideoWidget) TryRecordTouchDown(x, y int) bool {
	const samePointRadius = 5
	vw.touchDedupMu.Lock()
	defer vw.touchDedupMu.Unlock()
	dx := x - vw.lastTouchX
	if dx < 0 {
		dx = -dx
	}
	dy := y - vw.lastTouchY
	if dy < 0 {
		dy = -dy
	}
	if dx <= samePointRadius && dy <= samePointRadius && time.Since(vw.lastTouchDownTime) < 120*time.Millisecond {
		return false
	}
	vw.lastTouchDownTime = time.Now()
	vw.lastTouchX = x
	vw.lastTouchY = y
	return true
}

// UpdateTouchpadAndContentRect обновляет размер области ввода и прямоугольник видео.
func (vw *VideoWidget) UpdateTouchpadAndContentRect(w, h float32) {
	if w <= 0 || h <= 0 {
		return
	}
	vw.touchpadSizeW = w
	vw.touchpadSizeH = h
	vw.contentRectX = 0
	vw.contentRectY = 0
	vw.contentRectW = w
	vw.contentRectH = h
	if vw.videoCanvas != nil && vw.videoCanvas.Image != nil {
		b := vw.videoCanvas.Image.Bounds()
		imgW := float32(b.Dx())
		imgH := float32(b.Dy())
		if imgW > 0 && imgH > 0 {
			scale := w / imgW
			if h/imgH < scale {
				scale = h / imgH
			}
			renderW := imgW * scale
			renderH := imgH * scale
			vw.contentRectX = (w - renderW) / 2
			vw.contentRectY = (h - renderH) / 2
			vw.contentRectW = renderW
			vw.contentRectH = renderH
		}
	}
}

// PositionToAbsolute переводит координаты из области ввода в абсолютные 0..4095.
func (vw *VideoWidget) PositionToAbsolute(px, py float32) (x, y int) {
	if vw.touchpadSizeW <= 0 || vw.touchpadSizeH <= 0 {
		return 0, 0
	}
	var u, v float32
	if vw.contentRectW > 0 && vw.contentRectH > 0 {
		u = (px - vw.contentRectX) / vw.contentRectW
		v = (py - vw.contentRectY) / vw.contentRectH
	} else {
		u = px / vw.touchpadSizeW
		v = py / vw.touchpadSizeH
	}
	if u < 0 {
		u = 0
	} else if u > 1 {
		u = 1
	}
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	x = int(u * 4095)
	y = int(v * 4095)
	if x > 4095 {
		x = 4095
	}
	if y > 4095 {
		y = 4095
	}
	return x, y
}
