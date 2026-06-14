package controller

import (
	"image"
	"image/color"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"

	"usbridge-client/internal/input"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// vkeyMu / vkeyLastPress prevent virtual-keyboard turbo: if the same HID key
// is sent again within 200 ms (OS key-repeat or widget duplicate events),
// the repeat is silently dropped.
var (
	vkeyMu        sync.Mutex
	vkeyLastPress = map[int]time.Time{}
)

const desktopPrintableRuneSuppressWindow = 75 * time.Millisecond

func modifierMaskForKeyName(keyName fyne.KeyName) int32 {
	switch keyName {
	case fyne.KeyName("LeftControl"), fyne.KeyName("RightControl"):
		return 1
	case fyne.KeyName("LeftShift"), fyne.KeyName("RightShift"):
		return 2
	case fyne.KeyName("LeftAlt"), fyne.KeyName("RightAlt"):
		return 4
	case fyne.KeyName("LeftSuper"), fyne.KeyName("RightSuper"):
		return 8
	default:
		return 0
	}
}

func isDesktopPrintableKeyFallbackEnabled() bool {
	return runtime.GOOS != "android" && runtime.GOOS != "ios"
}

// moonlightInput returns the MoonlightInputSender when a Moonlight stream is active,
// or nil to fall through to the WebSocket HID path.
func (vw *VideoWidget) moonlightInput() service.MoonlightInputSender {
	if vw.videoClient == nil || !vw.videoClient.IsConnected() {
		return nil
	}
	mi, _ := vw.videoClient.(service.MoonlightInputSender)
	return mi
}

// GetMoonlightInput returns the active MoonlightInputSender, or nil if unavailable.
// Intended for external consumers such as the gamepad capture controller.
func (vw *VideoWidget) GetMoonlightInput() service.MoonlightInputSender {
	return vw.moonlightInput()
}

// widgetToMoonlightModifiers converts the widget modifier bitmask
// (Ctrl=1, Shift=2, Alt=4, Super=8) to Moonlight's bitmask
// (MODIFIER_SHIFT=0x01, MODIFIER_CTRL=0x02, MODIFIER_ALT=0x04, MODIFIER_META=0x08).
func widgetToMoonlightModifiers(widgetMods int) int8 {
	var mods int8
	if widgetMods&1 != 0 {
		mods |= 0x02
	}
	if widgetMods&2 != 0 {
		mods |= 0x01
	}
	if widgetMods&4 != 0 {
		mods |= 0x04
	}
	if widgetMods&8 != 0 {
		mods |= 0x08
	}
	return mods
}

func (vw *VideoWidget) currentHIDModifiers() int {
	return int(vw.keyboardModifierState.Load())
}

// moonlightTrackKeyDown records vkCode as held. Returns true if the key was
// already tracked as held — meaning Fyne dropped the preceding KeyUp event.
func (vw *VideoWidget) moonlightTrackKeyDown(vkCode int16) (wasAlreadyHeld bool) {
	vw.moonlightKeyMu.Lock()
	defer vw.moonlightKeyMu.Unlock()
	if vw.moonlightHeldVKs == nil {
		vw.moonlightHeldVKs = make(map[int16]bool)
	}
	wasAlreadyHeld = vw.moonlightHeldVKs[vkCode]
	vw.moonlightHeldVKs[vkCode] = true
	return
}

// moonlightTrackKeyUp removes vkCode from the held set.
func (vw *VideoWidget) moonlightTrackKeyUp(vkCode int16) {
	vw.moonlightKeyMu.Lock()
	defer vw.moonlightKeyMu.Unlock()
	delete(vw.moonlightHeldVKs, vkCode)
}

// releaseAllMoonlightKeys sends synthetic KeyUp for every key currently tracked as held,
// then clears the held set and resets modifier state. Call on window focus loss or stream
// stop to prevent stuck keys from continuing to repeat on the remote host.
func (vw *VideoWidget) releaseAllMoonlightKeys() {
	mi := vw.moonlightInput()
	vw.moonlightKeyMu.Lock()
	defer vw.moonlightKeyMu.Unlock()
	if len(vw.moonlightHeldVKs) == 0 {
		return
	}
	logrus.Infof("⌨️ [INPUT] releasing %d stuck held key(s) (focus lost or stream stopped)", len(vw.moonlightHeldVKs))
	if mi != nil {
		mods := widgetToMoonlightModifiers(int(vw.keyboardModifierState.Load()))
		for vkCode := range vw.moonlightHeldVKs {
			mi.SendMoonlightKey(vkCode, service.LiKeyActionUp, mods)
		}
	}
	vw.moonlightHeldVKs = nil
	vw.keyboardModifierState.Store(0)
}

func (vw *VideoWidget) handlePhysicalKeyDown(event *fyne.KeyEvent) {
	if event == nil {
		return
	}
	logrus.Infof("⌨️ [INPUT][DOWN] key=%q physical=%+v", event.Name, event.Physical)
	if mask := modifierMaskForKeyName(event.Name); mask != 0 {
		for {
			current := vw.keyboardModifierState.Load()
			next := current | mask
			if vw.keyboardModifierState.CompareAndSwap(current, next) {
				logrus.Infof("⌨️ [INPUT][DOWN] modifiers=%d", next)
				break
			}
		}
	}
	if mi := vw.moonlightInput(); mi != nil {
		if vkCode := moonlightVKCode(event); vkCode != 0 {
			if vw.moonlightTrackKeyDown(vkCode) {
				// Fyne dropped the preceding KeyUp — key is stuck on the remote.
				// Send a synthetic release so Sunshine/HID clears it before the new press.
				logrus.Warnf("⌨️ [INPUT][DOWN] vk=0x%04X re-press without release — auto-releasing stuck key", uint16(vkCode))
				mi.SendMoonlightKey(vkCode, service.LiKeyActionUp, widgetToMoonlightModifiers(vw.currentHIDModifiers()))
			}
			mi.SendMoonlightKey(vkCode, service.LiKeyActionDown, widgetToMoonlightModifiers(vw.currentHIDModifiers()))
		}
	}
}

func (vw *VideoWidget) handlePhysicalKeyUp(event *fyne.KeyEvent) {
	if event == nil {
		return
	}
	logrus.Infof("⌨️ [INPUT][UP] key=%q physical=%+v", event.Name, event.Physical)
	if mask := modifierMaskForKeyName(event.Name); mask != 0 {
		for {
			current := vw.keyboardModifierState.Load()
			next := current &^ mask
			if vw.keyboardModifierState.CompareAndSwap(current, next) {
				logrus.Infof("⌨️ [INPUT][UP] modifiers=%d", next)
				break
			}
		}
	}
	mi := vw.moonlightInput()
	if mi == nil {
		logrus.Warnf("⌨️ [INPUT][UP] MoonlightInputSender is nil! Not sending key.")
		return
	}
	vkCode := moonlightVKCode(event)
	if vkCode == 0 {
		logrus.Warnf("⌨️ [INPUT][UP] vkCode resolved to 0 for key=%q! Not sending.", event.Name)
		return
	}
	
	mods := widgetToMoonlightModifiers(vw.currentHIDModifiers())
	logrus.Infof("⌨️ [INPUT][UP] Moonlight sending vkCode=0x%04X, action=%d, mods=0x%02X", uint16(vkCode), service.LiKeyActionUp, uint8(mods))

	vw.moonlightTrackKeyUp(vkCode)
	mi.SendMoonlightKey(vkCode, service.LiKeyActionUp, mods)
}

// moonlightVKCode resolves the Windows Virtual Key code for a physical key event.
// Primary: look up by Fyne key name (works for all layouts that produce ASCII names).
// Fallback: use the hardware scan code — this handles non-Latin layouts (Russian,
// Ukrainian, etc.) where GLFW returns KeyUnknown as the key name but still reports
// the correct PS/2 scan code for the physical key position.
func moonlightVKCode(event *fyne.KeyEvent) int16 {
	if vk := input.GetVKCode(event.Name); vk != 0 {
		return vk
	}
	return input.GetVKCodeFromScanCode(event.Physical.ScanCode)
}

// handlePhysicalKeyPress обрабатывает TypedKey физической клавиатуры.
// В Moonlight-only режиме клавиши идут через handlePhysicalKeyDown/Up.
// TypedKey подавляется чтобы избежать turbo (OS repeat) — исключение: KeyF11 игнорируем.
func (vw *VideoWidget) handlePhysicalKeyPress(event *fyne.KeyEvent) {
	if event.Name == fyne.KeyF11 {
		return
	}
	if mi := vw.moonlightInput(); mi != nil {
		return // Moonlight: keys go via KeyDown/KeyUp, not TypedKey
	}
}

// handlePhysicalRunePress — rune events не используются в Moonlight-only режиме.
func (vw *VideoWidget) handlePhysicalRunePress(_ rune) {}

// hidKeyToVK converts a USB HID Usage Page 0x07 keycode (used by the virtual
// keyboard layout) to a Windows Virtual Key code expected by LiSendKeyboardEvent.
// Physical keyboards on desktop go through Fyne's KeyDown with proper VK codes;
// this conversion is needed for virtual keyboard buttons on Android.
func hidKeyToVK(hid int) int16 {
	switch {
	// Letters A-Z: HID 4-29 → VK 0x41-0x5A
	case hid >= 4 && hid <= 29:
		return int16(0x41 + (hid - 4))
	// Digits 1-9: HID 30-38 → VK 0x31-0x39
	case hid >= 30 && hid <= 38:
		return int16(0x31 + (hid - 30))
	// F1-F12: HID 58-69 → VK 0x70-0x7B
	case hid >= 58 && hid <= 69:
		return int16(0x70 + (hid - 58))
	}
	switch hid {
	case 39:
		return 0x30 // 0 → VK_0
	case 40:
		return 0x0D // Enter → VK_RETURN
	case 41:
		return 0x1B // Escape → VK_ESCAPE
	case 42:
		return 0x08 // Backspace → VK_BACK
	case 43:
		return 0x09 // Tab → VK_TAB
	case 44:
		return 0x20 // Space → VK_SPACE
	case 45:
		return 0xBD // - → VK_OEM_MINUS
	case 46:
		return 0xBB // = → VK_OEM_PLUS
	case 47:
		return 0xDB // [ → VK_OEM_4
	case 48:
		return 0xDD // ] → VK_OEM_6
	case 49:
		return 0xDC // \ → VK_OEM_5
	case 51:
		return 0xBA // ; → VK_OEM_1
	case 52:
		return 0xDE // ' → VK_OEM_7
	case 53:
		return 0xC0 // ` → VK_OEM_3
	case 54:
		return 0xBC // , → VK_OEM_COMMA
	case 55:
		return 0xBE // . → VK_OEM_PERIOD
	case 56:
		return 0xBF // / → VK_OEM_2
	case 57:
		return 0x14 // Caps Lock → VK_CAPITAL
	case 74:
		return 0x24 // Home → VK_HOME
	case 75:
		return 0x21 // Page Up → VK_PRIOR
	case 76:
		return 0x2E // Delete → VK_DELETE
	case 77:
		return 0x23 // End → VK_END
	case 78:
		return 0x22 // Page Down → VK_NEXT
	case 79:
		return 0x27 // → Right Arrow → VK_RIGHT
	case 80:
		return 0x25 // ← Left Arrow → VK_LEFT
	case 81:
		return 0x28 // ↓ Down Arrow → VK_DOWN
	case 82:
		return 0x26 // ↑ Up Arrow → VK_UP
	// Modifiers
	case 224:
		return 0xA2 // Left Ctrl → VK_LCONTROL
	case 225:
		return 0xA0 // Left Shift → VK_LSHIFT
	case 226:
		return 0xA4 // Left Alt → VK_LMENU
	case 227:
		return 0x5B // Left GUI/Win → VK_LWIN
	case 228:
		return 0xA3 // Right Ctrl → VK_RCONTROL
	case 229:
		return 0xA1 // Right Shift → VK_RSHIFT
	case 230:
		return 0xA5 // Right Alt → VK_RMENU
	case 231:
		return 0x5C // Right GUI/Win → VK_RWIN
	case 232:
		return 0x5D // Application/Menu → VK_APPS
	default:
		return int16(hid)
	}
}

// handleVirtualKeyPress обрабатывает нажатия виртуальной клавиатуры через Moonlight.
// Virtual keyboard buttons use USB HID keycodes; convert to Windows VK codes first.
// A 100 ms per-key cooldown prevents turbo from OS key-repeat or widget duplicate events.
// KeyDown is held for 50 ms before sending KeyUp (server BIOS minimum hold requirement).
func (vw *VideoWidget) handleVirtualKeyPress(keyCode int, modifiers int) {
	vkeyMu.Lock()
	if time.Since(vkeyLastPress[keyCode]) < 200*time.Millisecond {
		vkeyMu.Unlock()
		return
	}
	vkeyLastPress[keyCode] = time.Now()
	vkeyMu.Unlock()

	vk := hidKeyToVK(keyCode)
	logrus.Infof("⌨️ Virtual keyboard: hid=%d vk=0x%02X modifiers=%d (Moonlight only)", keyCode, uint16(vk), modifiers)
	go func() {
		mi := vw.moonlightInput()
		if mi == nil {
			return
		}
		moonlightMods := widgetToMoonlightModifiers(modifiers)
		mi.SendMoonlightKey(vk, service.LiKeyActionDown, moonlightMods)
		time.Sleep(50 * time.Millisecond)
		mi.SendMoonlightKey(vk, service.LiKeyActionUp, moonlightMods)
	}()
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
	fyne.Do(func() {
		vw.refreshCursorOverlay()
	})
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
	vw.lastAbsX = x
	vw.lastAbsY = y
	vw.lastAbsSentTime = time.Now()

	// Forward via Moonlight (LiSendMousePositionEvent) when stream is active.
	// This feeds /dev/hid_t via bridgeAbsMouse EV_ABS path on the server.
	// Must not also write via WebSocket — two simultaneous writers to /dev/hid_t
	// corrupt HID reports and Windows stops responding to the cursor.
	if mi := vw.moonlightInput(); mi != nil && mi.IsInputActive() {
		prev := vw.statAbsMoonlight.Load()
		vw.statAbsMoonlight.Add(1)
		if prev == 0 {
			logrus.Info("🖱️ [Mouse] absolute path → Moonlight LiSendMousePositionEvent (switched)")
		}
		mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767)
		if scroll != 0 {
			mi.SendMoonlightScroll(int8(scroll))
		}
		return // Moonlight is active — do NOT also send via WebSocket
	}

}

// SendAbsoluteEvent отправляет атомарное абсолютное событие.
func (vw *VideoWidget) SendAbsoluteEvent(x, y int, scroll int, force bool) {
	vw.absSendMu.Lock()
	defer vw.absSendMu.Unlock()
	vw.sendAbsoluteEventLocked(x, y, scroll)
}

// absoluteButtonToMoonlight maps our HID button numbers to Moonlight button constants.
// HID: 1=left, 2=right, 3=middle. Moonlight: Left=1, Middle=2, Right=3.
func absoluteButtonToMoonlight(button int) int {
	if button == 2 {
		return service.LiMouseButtonRight
	}
	return button
}

func (vw *VideoWidget) PressAbsoluteButton(button int, x, y int) {
	vw.absSendMu.Lock()
	defer vw.absSendMu.Unlock()
	vw.updateAbsoluteButtonLocked(button, true)
	if mi := vw.moonlightInput(); mi != nil && mi.IsInputActive() {
		// Send button BEFORE position so the EV_SYN from the position event carries
		// the updated button state. If position comes first, the button EV_SYN has
		// no EV_ABS data and bridgeAbsMouse discards it (hasX/hasY both false).
		mi.SendMoonlightMouseButton(service.LiMouseButtonPress, absoluteButtonToMoonlight(button))
		mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767)
		vw.lastAbsX = x
		vw.lastAbsY = y
		return
	}
}

func (vw *VideoWidget) ReleaseAbsoluteButton(button int, x, y int) {
	vw.absSendMu.Lock()
	defer vw.absSendMu.Unlock()
	vw.updateAbsoluteButtonLocked(button, false)
	if mi := vw.moonlightInput(); mi != nil && mi.IsInputActive() {
		mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, absoluteButtonToMoonlight(button))
		mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767)
		vw.lastAbsX = x
		vw.lastAbsY = y
		return
	}
}

func (vw *VideoWidget) ReleaseAllAbsoluteButtons(x, y int) {
	vw.absSendMu.Lock()
	defer vw.absSendMu.Unlock()
	if mi := vw.moonlightInput(); mi != nil && mi.IsInputActive() {
		if vw.absButtons&0x01 != 0 {
			mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, service.LiMouseButtonLeft)
		}
		if vw.absButtons&0x02 != 0 {
			mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, service.LiMouseButtonRight)
		}
		if vw.absButtons&0x04 != 0 {
			mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, service.LiMouseButtonMiddle)
		}
		vw.absButtons = 0
		mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767)
		vw.lastAbsX = x
		vw.lastAbsY = y
		return
	}
	vw.absButtons = 0
}

func (vw *VideoWidget) ClickAbsoluteButton(button int, x, y int) {
	vw.absSendMu.Lock()
	defer vw.absSendMu.Unlock()
	if mi := vw.moonlightInput(); mi != nil && mi.IsInputActive() {
		moonlightBtn := absoluteButtonToMoonlight(button)
		// Press then position (so position EV_SYN carries button=pressed),
		// then release then position (EV_SYN carries button=released).
		mi.SendMoonlightMouseButton(service.LiMouseButtonPress, moonlightBtn)
		mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767)
		mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, moonlightBtn)
		mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767)
		vw.lastAbsX = x
		vw.lastAbsY = y
		return
	}
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

	// Determine the pixel dimensions of the video stream.
	// When frame != nil (Fyne canvas path), read directly from the image.
	// When frame == nil (Metal active — canvas cleared), reuse the last known dimensions
	// so that aspect-ratio correction and black-bar detection remain accurate after resize.
	imgW, imgH := vw.lastVideoImgW, vw.lastVideoImgH
	if frame != nil {
		b := frame.Bounds()
		fw, fh := float32(b.Dx()), float32(b.Dy())
		if fw > 0 && fh > 0 {
			imgW, imgH = fw, fh
			vw.lastVideoImgW = fw
			vw.lastVideoImgH = fh
		}
	}
	// Fall back to configured dimensions if we have never seen a frame.
	if imgW <= 0 || imgH <= 0 {
		if vw.videoClient != nil {
			if cfg := vw.videoClient.GetConfig(); cfg != nil && cfg.VideoWidth > 0 {
				imgW = float32(cfg.VideoWidth)
				imgH = float32(cfg.VideoHeight)
			}
		}
	}

	if imgW > 0 && imgH > 0 {
		scale := w / imgW
		if h/imgH < scale {
			scale = h / imgH
		}
		vw.baseContentRectW = imgW * scale
		vw.baseContentRectH = imgH * scale
	} else {
		vw.baseContentRectW = w
		vw.baseContentRectH = h
	}
	vw.recalculateViewport()
}

// PositionToAbsolute переводит координаты из области ввода в абсолютные координаты
// Windows-friendly absolute pointer descriptor (0..32767).
func (vw *VideoWidget) PositionToAbsolute(px, py float32) (x, y int) {
	if vw.touchpadSizeW <= 0 || vw.touchpadSizeH <= 0 {
		// Widget not yet sized — return center instead of (0,0) so the cursor
		// doesn't snap to the top-left corner before the first frame arrives.
		return 16383, 16383
	}

	rectX := vw.contentRectX
	rectY := vw.contentRectY
	rectW := vw.contentRectW
	rectH := vw.contentRectH

	// Применяем смещение по обнаруженным letterbox/pillarbox рамкам внутри кадра.
	// Только симметричные рамки (±2px) применяются, что защищает от ложных детектов.
	frameX, frameY, frameW, frameH := vw.getFrameContentRect()
	if rectW > 0 && rectH > 0 && frameW > 0 && frameH > 0 {
		rectX += rectW * frameX
		rectY += rectH * frameY
		rectW *= frameW
		rectH *= frameH
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

	// Разрешаем crop только если рамка симметрична (±2px) и не слишком мала
	// (шум) и не занимает больше 40% стороны (защита от полностью тёмного кадра).
	// Реальные letterbox/pillarbox рамки — обычно 5–25% стороны и строго симметричны.
	const minMeaningfulCropInsetPx = 2
	const maxCropAsymmetryPx = 2
	maxHInset := frameW * 2 / 5
	maxVInset := frameH * 2 / 5
	if left > maxHInset || right > maxHInset || left < minMeaningfulCropInsetPx || right < minMeaningfulCropInsetPx || absInt(left-right) > maxCropAsymmetryPx {
		left, right = 0, 0
	}
	if top > maxVInset || bottom > maxVInset || top < minMeaningfulCropInsetPx || bottom < minMeaningfulCropInsetPx || absInt(top-bottom) > maxCropAsymmetryPx {
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

	// Вычисляем доступную высоту для видео (за вычетом кнопок снизу)
	availableH := vw.touchpadSizeH - vw.bottomInset
	if availableH < 0 {
		availableH = 0
	}
	baseW := vw.baseContentRectW
	baseH := vw.baseContentRectH
	if baseW <= 0 || baseH <= 0 {
		baseW = vw.touchpadSizeW
		baseH = availableH
	}

	scale := vw.zoomScale
	if scale < 1 {
		scale = 1
	}

	contentW := baseW * scale
	contentH := baseH * scale

	// Центрируем горизонтально относительно всего экрана
	contentX := (vw.touchpadSizeW - contentW) / 2

	// Логика вертикального позиционирования:
	var contentY float32
	if contentH > availableH {
		// Видео больше доступной области.
		// Базовая позиция: низ видео совпадает с низом доступной области.
		contentY = availableH - contentH

		// Разрешаем перетаскивание в обоих направлениях:
		//   panOffsetY < 0: видео идёт вверх (зазор между видео и клавиатурой)
		//   panOffsetY > 0: видео идёт вниз (верхняя часть видео входит в поле зрения)
		// Максимум вниз: верх видео на уровне верха экрана (contentY = 0)
		maxPanY := contentH - availableH
		if vw.panOffsetY > maxPanY {
			vw.panOffsetY = maxPanY
		}
		contentY += vw.panOffsetY
	} else {
		// Видео меньше доступной области - центрируем его в ней
		contentY = (availableH - contentH) / 2
		vw.panOffsetY = 0
	}

	if contentW > vw.touchpadSizeW {
		maxPanX := (contentW - vw.touchpadSizeW) / 2
		vw.panOffsetX = clampFloat(vw.panOffsetX, -maxPanX, maxPanX)
		contentX += vw.panOffsetX
	} else {
		vw.panOffsetX = 0
	}

	if scale <= 1.001 && vw.bottomInset == 0 {
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
