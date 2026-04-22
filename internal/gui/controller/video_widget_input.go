package controller

import (
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"usbridge-client/internal/input"
	"usbridge-client/internal/models"

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
	vw.sendPhysicalKeyToRemote(event)
}

// handlePhysicalRunePress обрабатывает ввод символов с физической клавиатуры.
func (vw *VideoWidget) handlePhysicalRunePress(r rune) {
	if vw.usbClient == nil {
		return
	}
	vw.sendRuneToRemote(r)
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
	vw.sendKeyToRemote(keyCode, modifiers)
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

	if !vw.IsTouchPadInputMode() {
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
	vw.enqueueMouseMove(dx, dy)
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

func (vw *VideoWidget) IsTouchPadInputMode() bool {
	return vw.GetMouseInputMode() == mouseModeTouchPad
}

func (vw *VideoWidget) IsAbsoluteLikeInputMode() bool {
	return vw.GetMouseInputMode() == mouseModeAbsolute
}

func (vw *VideoWidget) UsesRelativeMouseInput() bool {
	return vw.GetMouseInputMode() == mouseModeTouchPad
}

// GetMouseInputMode возвращает тип манипулятора.
func (vw *VideoWidget) GetMouseInputMode() string {
	if vw.mouseInputMode == "" {
		vw.mouseInputMode = defaultMouseMode()
	}
	return normalizeMouseMode(vw.mouseInputMode)
}

// SetMouseInputMode задаёт тип манипулятора.
func (vw *VideoWidget) SetMouseInputMode(mode string) {
	mode = normalizeMouseMode(mode)
	vw.mouseInputMode = mode
	vw.resetRelativeMoveAccumulator()
	vw.logMouseModeState("desired-updated")
}

func (vw *VideoWidget) setObservedMouseMode(mode string) {
	vw.observedMouseMode = normalizeMouseMode(mode)
	vw.logMouseModeState("observed-updated")
}

// GetShowMouseCursor возвращает флаг отображения курсора в захваченном видео.
func (vw *VideoWidget) GetShowMouseCursor() bool {
	return vw.showMouseCursor
}

// SetShowMouseCursor задаёт флаг отображения курсора в захваченном видео.
// Если видео активно, перезапускает поток, чтобы сервер применил новое значение ShowMouse.
func (vw *VideoWidget) SetShowMouseCursor(show bool) {
	if vw.showMouseCursor == show {
		return
	}
	vw.showMouseCursor = show
	vw.refreshCursorOverlay()
	if vw.isStreaming {
		vw.videoOpMu.Lock()
		vw.videoRestartPending = true
		vw.videoOpMu.Unlock()
		vw.scheduleVideoReconcile("show-mouse-cursor-changed")
	}
}

func (vw *VideoWidget) SetAgentEnvironment(agentOS, agentDisplay string) {
	vw.agentOS = strings.TrimSpace(agentOS)
	vw.agentDisplay = strings.TrimSpace(agentDisplay)
	vw.refreshCursorOverlay()
}

func (vw *VideoWidget) UsesWaylandCursorOverlay() bool {
	osName := strings.ToLower(strings.TrimSpace(vw.agentOS))
	display := strings.ToLower(strings.TrimSpace(vw.agentDisplay))
	return strings.Contains(osName, "linux") && (strings.Contains(osName, "wayland") || display == "wayland")
}

func (vw *VideoWidget) ShouldRenderCursorOverlay() bool {
	return vw.showMouseCursor && vw.isMouseConnected && vw.UsesWaylandCursorOverlay()
}

func (vw *VideoWidget) UpdateCursorOverlayPointer(x, y float32, visible bool) {
	vw.cursorOverlayX = x
	vw.cursorOverlayY = y
	vw.cursorOverlayShown = visible
	vw.refreshCursorOverlay()
}

// UpdateCursorOverlayFromLocalInput updates the local preview cursor only when it
// is not expected to come from the remote side (for example Wayland cursor metadata).
func (vw *VideoWidget) UpdateCursorOverlayFromLocalInput(x, y float32, visible bool) {
	if vw.ShouldRenderCursorOverlay() && vw.IsTouchPadInputMode() {
		return
	}
	vw.UpdateCursorOverlayPointer(x, y, visible)
}

func (vw *VideoWidget) handleRemoteCursorUpdate(state models.CursorState) {
	should := vw.ShouldRenderCursorOverlay()
	logrus.Debugf("[cursor-overlay] recv: vis=%v x=%.0f y=%.0f size=%dx%d src=%s shouldRender=%v agentOS=%q agentDisplay=%q",
		state.Visible, state.X, state.Y, state.Width, state.Height, state.Source,
		should, vw.agentOS, vw.agentDisplay)
	if !should {
		return
	}
	fyne.Do(func() {
		vw.updateRemoteCursorOverlay(state)
	})
}

func (vw *VideoWidget) updateRemoteCursorOverlay(state models.CursorState) {
	if !state.Visible || state.Width <= 0 || state.Height <= 0 {
		logrus.Debugf("[cursor-overlay] hidden: vis=%v size=%dx%d", state.Visible, state.Width, state.Height)
		vw.UpdateCursorOverlayPointer(0, 0, false)
		return
	}

	x, y, w, h := vw.GetViewportRect()
	if w <= 0 || h <= 0 {
		logrus.Warnf("[cursor-overlay] viewport not ready: x=%.0f y=%.0f w=%.0f h=%.0f", x, y, w, h)
		vw.UpdateCursorOverlayPointer(0, 0, false)
		return
	}

	denomW := float64(state.Width - 1)
	denomH := float64(state.Height - 1)
	if denomW <= 0 {
		denomW = 1
	}
	if denomH <= 0 {
		denomH = 1
	}

	localX := x + float32((state.X/denomW)*float64(w))
	localY := y + float32((state.Y/denomH)*float64(h))
	logrus.Debugf("[cursor-overlay] draw: remote=(%.0f,%.0f)/(%dx%d) -> local=(%.1f,%.1f) viewport=(%.0f,%.0f,%.0f,%.0f)",
		state.X, state.Y, state.Width, state.Height, localX, localY, x, y, w, h)
	vw.UpdateCursorOverlayPointer(localX, localY, true)
}

func (vw *VideoWidget) refreshCursorOverlay() {
	if vw.touchpadWrapper != nil {
		vw.touchpadWrapper.UpdateCursorOverlay()
	}
}

func (vw *VideoWidget) logMouseModeState(reason string) {
	desired := normalizeMouseMode(vw.mouseInputMode)
	observed := normalizeMouseMode(vw.observedMouseMode)
	diag := reason + "|desired=" + desired + "|observed=" + observed
	if diag == vw.lastMouseModeDiag {
		return
	}
	vw.lastMouseModeDiag = diag
	if observed != "" && observed != desired {
		logrus.Warnf("🖱️ Pointer mode mismatch (%s): desired=%s observed=%s", reason, desired, observed)
		return
	}
	logrus.Infof("🖱️ Pointer mode state (%s): desired=%s observed=%s", reason, desired, observed)
}

// SendAbsolutePosition отправляет абсолютную позицию с небольшим дебаунсом.
func (vw *VideoWidget) SendAbsolutePosition(x, y int, force bool) {
	if vw.usbClient == nil {
		return
	}
	vw.absSendMu.Lock()
	defer vw.absSendMu.Unlock()
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
	vw.sendAbsoluteEventLocked(x, y, 0)
}

func (vw *VideoWidget) updateAbsoluteButtonLocked(button int, pressed bool) {
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

func (vw *VideoWidget) sendAbsoluteEventLocked(x, y int, scroll int) {
	if vw.usbClient == nil {
		return
	}
	vw.lastAbsX = x
	vw.lastAbsY = y
	vw.lastAbsSentTime = time.Now()
	_ = vw.usbClient.SendAbsoluteEvent(x, y, vw.absButtons, scroll)
}

// SendAbsoluteEvent отправляет атомарное абсолютное событие.
func (vw *VideoWidget) SendAbsoluteEvent(x, y int, scroll int, force bool) {
	if vw.usbClient == nil {
		return
	}
	vw.absSendMu.Lock()
	defer vw.absSendMu.Unlock()
	vw.sendAbsoluteEventLocked(x, y, scroll)
}

func (vw *VideoWidget) PressAbsoluteButton(button int, x, y int) {
	if vw.usbClient == nil {
		return
	}
	vw.absSendMu.Lock()
	defer vw.absSendMu.Unlock()
	vw.updateAbsoluteButtonLocked(button, true)
	vw.sendAbsoluteEventLocked(x, y, 0)
}

func (vw *VideoWidget) ReleaseAbsoluteButton(button int, x, y int) {
	if vw.usbClient == nil {
		return
	}
	vw.absSendMu.Lock()
	defer vw.absSendMu.Unlock()
	vw.updateAbsoluteButtonLocked(button, false)
	vw.sendAbsoluteEventLocked(x, y, 0)
}

func (vw *VideoWidget) ReleaseAllAbsoluteButtons(x, y int) {
	if vw.usbClient == nil {
		return
	}
	vw.absSendMu.Lock()
	defer vw.absSendMu.Unlock()
	vw.absButtons = 0
	vw.sendAbsoluteEventLocked(x, y, 0)
}

func (vw *VideoWidget) ClickAbsoluteButton(button int, x, y int) {
	if vw.usbClient == nil {
		return
	}
	vw.absSendMu.Lock()
	defer vw.absSendMu.Unlock()
	vw.updateAbsoluteButtonLocked(button, true)
	vw.sendAbsoluteEventLocked(x, y, 0)
	vw.updateAbsoluteButtonLocked(button, false)
	vw.sendAbsoluteEventLocked(x, y, 0)
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
			vw.enqueueTouchPositionOnly(x, y, true)
		} else {
			vw.enqueueTouch(x, y, true)
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
func (vw *VideoWidget) UpdateTouchpadAndContentRect(w, h float32, frame image.Image) {
	if w <= 0 || h <= 0 {
		return
	}
	vw.touchpadSizeW = w
	vw.touchpadSizeH = h
	vw.contentRectX = 0
	vw.contentRectY = 0
	vw.contentRectW = w
	vw.contentRectH = h
	vw.baseContentRectW = w
	vw.baseContentRectH = h
	if frame != nil {
		b := frame.Bounds()
		imgW := float32(b.Dx())
		imgH := float32(b.Dy())
		if imgW > 0 && imgH > 0 {
			scale := w / imgW
			if h/imgH < scale {
				scale = h / imgH
			}
			renderW := imgW * scale
			renderH := imgH * scale
			vw.baseContentRectW = renderW
			vw.baseContentRectH = renderH
		}
	}
	vw.recalculateViewport()
}

// PositionToAbsolute переводит координаты из области ввода в абсолютные координаты
// Windows-friendly absolute pointer descriptor (0..32767).
func (vw *VideoWidget) PositionToAbsolute(px, py float32) (x, y int) {
	if vw.touchpadSizeW <= 0 || vw.touchpadSizeH <= 0 {
		return 0, 0
	}

	rectX := vw.contentRectX
	rectY := vw.contentRectY
	rectW := vw.contentRectW
	rectH := vw.contentRectH

	// Для absolute/tablet режима не применяем дополнительный auto-crop по самому
	// кадру. Иначе даже небольшой ложный inset создаёт вторую "мини-область"
	// в левом верхнем углу, где координаты повторно растягиваются на весь экран.
	if !vw.IsAbsoluteLikeInputMode() {
		frameX, frameY, frameW, frameH := vw.getFrameContentRect()
		if rectW > 0 && rectH > 0 && frameW > 0 && frameH > 0 {
			rectX += rectW * frameX
			rectY += rectH * frameY
			rectW *= frameW
			rectH *= frameH
		}
	}

	var u, v float32
	if rectW > 0 && rectH > 0 {
		u = (px - rectX) / rectW
		v = (py - rectY) / rectH
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
	const absolutePointerMax = 32767
	x = int(math.Round(float64(u * absolutePointerMax)))
	y = int(math.Round(float64(v * absolutePointerMax)))
	if x > absolutePointerMax {
		x = absolutePointerMax
	}
	if y > absolutePointerMax {
		y = absolutePointerMax
	}
	return x, y
}

func (vw *VideoWidget) getFrameContentRect() (float32, float32, float32, float32) {
	vw.frameMutex.RLock()
	defer vw.frameMutex.RUnlock()

	if vw.frameContentW <= 0 || vw.frameContentH <= 0 {
		return 0, 0, 1, 1
	}
	return vw.frameContentX, vw.frameContentY, vw.frameContentW, vw.frameContentH
}

func (vw *VideoWidget) updateFrameContentRect(frame image.Image) {
	bounds := frame.Bounds()
	frameW := bounds.Dx()
	frameH := bounds.Dy()
	if frameW <= 0 || frameH <= 0 {
		return
	}

	left := detectDarkInset(frame, bounds, true, true)
	right := detectDarkInset(frame, bounds, true, false)
	top := detectDarkInset(frame, bounds, false, true)
	bottom := detectDarkInset(frame, bounds, false, false)

	// Защита от ложного детекта: если "чёрная рамка" слишком большая хотя бы
	// с одной стороны, считаем что это уже не служебный inset, и не кропаем
	// кадр вообще. Иначе именно такой ложный crop может создать второе
	// "мини-поле" в абсолютном режиме.
	const maxAutoCropInsetPx = 20
	const minMeaningfulCropInsetPx = 2
	const maxCropAsymmetryPx = 2
	if left > maxAutoCropInsetPx || right > maxAutoCropInsetPx || top > maxAutoCropInsetPx || bottom > maxAutoCropInsetPx {
		left, right, top, bottom = 0, 0, 0, 0
	}

	// Разрешаем crop только если он выглядит как небольшая симметричная
	// рамка. Односторонний или сильно асимметричный inset чаще всего ложный
	// и даёт "дублирующее мини-поле" в углу.
	if left < minMeaningfulCropInsetPx || right < minMeaningfulCropInsetPx || absInt(left-right) > maxCropAsymmetryPx {
		left, right = 0, 0
	}
	if top < minMeaningfulCropInsetPx || bottom < minMeaningfulCropInsetPx || absInt(top-bottom) > maxCropAsymmetryPx {
		top, bottom = 0, 0
	}

	if left+right >= frameW-4 {
		left, right = 0, 0
	}
	if top+bottom >= frameH-4 {
		top, bottom = 0, 0
	}

	contentX := float32(left) / float32(frameW)
	contentY := float32(top) / float32(frameH)
	contentW := float32(frameW-left-right) / float32(frameW)
	contentH := float32(frameH-top-bottom) / float32(frameH)

	if contentW <= 0 || contentH <= 0 {
		contentX, contentY, contentW, contentH = 0, 0, 1, 1
	}

	vw.frameMutex.Lock()
	vw.frameContentX = contentX
	vw.frameContentY = contentY
	vw.frameContentW = contentW
	vw.frameContentH = contentH
	vw.frameMutex.Unlock()
}

func detectDarkInset(img image.Image, bounds image.Rectangle, vertical bool, fromStart bool) int {
	limit := bounds.Dx() / 3
	if !vertical {
		limit = bounds.Dy() / 3
	}
	if limit < 0 {
		limit = 0
	}

	maxSamples := 96
	for offset := 0; offset < limit; offset++ {
		darkSamples := 0
		totalSamples := 0

		if vertical {
			step := maxInt(1, bounds.Dy()/maxSamples)
			x := bounds.Min.X + offset
			if !fromStart {
				x = bounds.Max.X - 1 - offset
			}
			for y := bounds.Min.Y; y < bounds.Max.Y; y += step {
				totalSamples++
				if isNearBlack(img.At(x, y)) {
					darkSamples++
				}
			}
		} else {
			step := maxInt(1, bounds.Dx()/maxSamples)
			y := bounds.Min.Y + offset
			if !fromStart {
				y = bounds.Max.Y - 1 - offset
			}
			for x := bounds.Min.X; x < bounds.Max.X; x += step {
				totalSamples++
				if isNearBlack(img.At(x, y)) {
					darkSamples++
				}
			}
		}

		if totalSamples == 0 {
			break
		}

		darkRatio := float32(darkSamples) / float32(totalSamples)
		if darkRatio < 0.98 {
			return offset
		}
	}

	return limit
}

func isNearBlack(c color.Color) bool {
	r, g, b, a := c.RGBA()
	if a < 0x2000 {
		return true
	}
	const maxDark = 24 << 8
	return r <= maxDark && g <= maxDark && b <= maxDark
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (vw *VideoWidget) recalculateViewport() {
	if vw.touchpadSizeW <= 0 || vw.touchpadSizeH <= 0 {
		return
	}

	baseW := vw.baseContentRectW
	baseH := vw.baseContentRectH
	if baseW <= 0 || baseH <= 0 {
		baseW = vw.touchpadSizeW
		baseH = vw.touchpadSizeH
	}

	scale := vw.zoomScale
	if scale < 1 {
		scale = 1
	}

	contentW := baseW * scale
	contentH := baseH * scale
	contentX := (vw.touchpadSizeW - contentW) / 2
	contentY := (vw.touchpadSizeH - contentH) / 2

	if contentW > vw.touchpadSizeW {
		maxPanX := (contentW - vw.touchpadSizeW) / 2
		vw.panOffsetX = clampFloat(vw.panOffsetX, -maxPanX, maxPanX)
		contentX += vw.panOffsetX
	} else {
		vw.panOffsetX = 0
	}

	if contentH > vw.touchpadSizeH {
		maxPanY := (contentH - vw.touchpadSizeH) / 2
		vw.panOffsetY = clampFloat(vw.panOffsetY, -maxPanY, maxPanY)
		contentY += vw.panOffsetY
	} else {
		vw.panOffsetY = 0
	}

	if contentW <= vw.touchpadSizeW && contentH <= vw.touchpadSizeH && scale <= 1.001 {
		scale = 1
		vw.zoomScale = 1
		vw.panOffsetX = 0
		vw.panOffsetY = 0
		contentW = baseW
		contentH = baseH
		contentX = (vw.touchpadSizeW - contentW) / 2
		contentY = (vw.touchpadSizeH - contentH) / 2
	}

	vw.contentRectX = contentX
	vw.contentRectY = contentY
	vw.contentRectW = contentW
	vw.contentRectH = contentH
}

func (vw *VideoWidget) GetViewportRect() (float32, float32, float32, float32) {
	vw.recalculateViewport()
	return vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH
}

func (vw *VideoWidget) applyViewportGesture(scaleFactor, focusX, focusY, panDx, panDy float32) {
	if vw.touchpadSizeW <= 0 || vw.touchpadSizeH <= 0 {
		return
	}

	oldX, oldY, oldW, oldH := vw.GetViewportRect()
	if oldW <= 0 || oldH <= 0 {
		return
	}

	nextZoom := vw.zoomScale
	if nextZoom < 1 {
		nextZoom = 1
	}
	if scaleFactor <= 0 || math.Abs(float64(scaleFactor-1)) < 0.02 {
		scaleFactor = 1
	}
	if scaleFactor > 0 && !almostEqual(scaleFactor, 1) {
		nextZoom *= scaleFactor
	}
	nextZoom = clampFloat(nextZoom, 1, 6)
	vw.zoomScale = nextZoom
	vw.recalculateViewport()

	if scaleFactor > 0 && !almostEqual(scaleFactor, 1) {
		localFocusX := clampFloat(focusX, 0, vw.touchpadSizeW)
		localFocusY := clampFloat(focusY, 0, vw.touchpadSizeH)
		u := clampFloat((localFocusX-oldX)/oldW, 0, 1)
		v := clampFloat((localFocusY-oldY)/oldH, 0, 1)

		newW := vw.contentRectW
		newH := vw.contentRectH
		if newW > vw.touchpadSizeW {
			baseX := (vw.touchpadSizeW - newW) / 2
			vw.panOffsetX = localFocusX - u*newW - baseX
		}
		if newH > vw.touchpadSizeH {
			baseY := (vw.touchpadSizeH - newH) / 2
			vw.panOffsetY = localFocusY - v*newH - baseY
		}
	}

	vw.panOffsetX += panDx
	vw.panOffsetY += panDy
	vw.recalculateViewport()
}

func (vw *VideoWidget) resetViewport() {
	vw.zoomScale = 1
	vw.panOffsetX = 0
	vw.panOffsetY = 0
	vw.recalculateViewport()
}

func clampFloat(value, minValue, maxValue float32) float32 {
	return float32(math.Max(float64(minValue), math.Min(float64(maxValue), float64(value))))
}

func almostEqual(a, b float32) bool {
	return math.Abs(float64(a-b)) < 0.001
}
