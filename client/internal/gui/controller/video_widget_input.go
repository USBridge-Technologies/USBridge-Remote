package controller

import (
	"image"
	"image/color"
	"math"
	"runtime"
	"strconv"
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

// absLogAt throttles PositionToAbsolute diagnostic output to once per 2 seconds.
var absLogAt time.Time
var inStreamLogAt time.Time

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
			vw.enqueueSend(func() { mi.SendMoonlightKey(vkCode, service.LiKeyActionUp, mods) })
		}
	}
	vw.moonlightHeldVKs = nil
	vw.keyboardModifierState.Store(0)
}

func (vw *VideoWidget) handlePhysicalKeyDown(event *fyne.KeyEvent) {
	if event == nil {
		return
	}
	// Soft IME: characters come only via TypedRune → UTF-8. KeyDown from
	// keyboardTyped's Latin Code would double-insert (VK + rune).
	if vw.IsSystemIMESticky() {
		switch event.Name {
		case fyne.KeyBackspace, fyne.KeyDelete, fyne.KeyReturn, fyne.KeyEnter,
			fyne.KeyTab, fyne.KeyEscape,
			fyne.KeyUp, fyne.KeyDown, fyne.KeyLeft, fyne.KeyRight:
			// keep editing / nav keys; everything else is TypedRune UTF-8
		default:
			return
		}
	}

	logrus.Debugf("⌨️ [INPUT][DOWN] key=%q physical=%+v", event.Name, event.Physical)
	if mask := modifierMaskForKeyName(event.Name); mask != 0 {
		for {
			current := vw.keyboardModifierState.Load()
			next := current | mask
			if vw.keyboardModifierState.CompareAndSwap(current, next) {
				logrus.Debugf("⌨️ [INPUT][DOWN] modifiers=%d", next)
				break
			}
		}
	}
	if isTextRoutedKeystroke(event, vw.currentHIDModifiers()) {
		// Fyne's TypedRune (handlePhysicalRunePress) will deliver this
		// keystroke's actual character; sending the raw VK here too would
		// double it up (once garbled through a layout guess, once correct).
		return
	}
	if mi := vw.moonlightInput(); mi != nil {
		if vkCode := moonlightVKCode(event); vkCode != 0 {
			if vkCode == 0x0D {
				logrus.Infof("⌨️ [INPUT][ENTER] sending VK_RETURN (0x0D) to Moonlight (key=%q, scan=%d)", event.Name, event.Physical.ScanCode)
			}
			mods := widgetToMoonlightModifiers(vw.currentHIDModifiers())
			if vw.moonlightTrackKeyDown(vkCode) {
				// Fyne dropped the preceding KeyUp — key is stuck on the remote.
				// Send a synthetic release so Sunshine/HID clears it before the new press.
				logrus.Warnf("⌨️ [INPUT][DOWN] vk=0x%04X re-press without release — auto-releasing stuck key", uint16(vkCode))
				vw.enqueueSend(func() { mi.SendMoonlightKey(vkCode, service.LiKeyActionUp, mods) })
			}
			vw.enqueueSend(func() { mi.SendMoonlightKey(vkCode, service.LiKeyActionDown, mods) })
		}
	}
}

func (vw *VideoWidget) handlePhysicalKeyUp(event *fyne.KeyEvent) {
	if event == nil {
		return
	}
	if vw.IsSystemIMESticky() {
		switch event.Name {
		case fyne.KeyBackspace, fyne.KeyDelete, fyne.KeyReturn, fyne.KeyEnter,
			fyne.KeyTab, fyne.KeyEscape,
			fyne.KeyUp, fyne.KeyDown, fyne.KeyLeft, fyne.KeyRight:
		default:
			return
		}
	}
	logrus.Debugf("⌨️ [INPUT][UP] key=%q physical=%+v", event.Name, event.Physical)
	if mask := modifierMaskForKeyName(event.Name); mask != 0 {
		for {
			current := vw.keyboardModifierState.Load()
			next := current &^ mask
			if vw.keyboardModifierState.CompareAndSwap(current, next) {
				logrus.Debugf("⌨️ [INPUT][UP] modifiers=%d", next)
				break
			}
		}
	}
	if isTextRoutedKeystroke(event, vw.currentHIDModifiers()) {
		return
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
	logrus.Debugf("⌨️ [INPUT][UP] Moonlight sending vkCode=0x%04X, action=%d, mods=0x%02X", uint16(vkCode), service.LiKeyActionUp, uint8(mods))

	vw.moonlightTrackKeyUp(vkCode)
	vw.enqueueSend(func() { mi.SendMoonlightKey(vkCode, service.LiKeyActionUp, mods) })
}

// isTextRoutedKeystroke reports whether this keystroke should be left to
// Fyne's TypedRune (-> handlePhysicalRunePress -> SendMoonlightUtf8Text)
// instead of being sent as a raw VK code here. True for a plain
// character-producing key (letters/digits/symbols/space, Shift alone
// doesn't disqualify it -- Fyne folds Shift into the resolved rune's case)
// with no Ctrl/Alt/Super held: those are shortcuts, and Fyne doesn't fire
// TypedRune for them anyway, so they must keep going through the VK path.
func isTextRoutedKeystroke(event *fyne.KeyEvent, hidModifiers int) bool {
	// Never route special control keys as text, even if their OS scancode happens to fall
	// into the character scancode ranges (which is the case on macOS).
	if event.Name == fyne.KeyReturn || event.Name == fyne.KeyEnter || event.Name == fyne.KeyTab || event.Name == fyne.KeyBackspace || event.Name == fyne.KeyEscape || event.Name == fyne.KeyUp || event.Name == fyne.KeyDown || event.Name == fyne.KeyLeft || event.Name == fyne.KeyRight || event.Name == fyne.KeyDelete {
		return false
	}

	const ctrlAltSuperMask = 1 | 4 | 8 // widget bitmask: Ctrl=1, Alt=4, Super=8 (Shift=2 excluded on purpose)
	if hidModifiers&ctrlAltSuperMask != 0 {
		return false
	}
	return isCharacterScanCode(event.Physical.ScanCode)
}

// isCharacterScanCode reports whether a scan code is a physical
// character-key position (letter/digit/symbol row), regardless of what
// layout is active -- unlike event.Name (blank/"KeyUnknown" on non-Latin
// layouts) or IsPrintableKey (same problem), scan codes are always reported
// correctly by GLFW for the physical key position pressed. Mirrors the same
// scan-code rows input.GetVKCodeFromScanCode maps.
//
// GLFW's scan code is NOT a single universal number space: on Windows it's
// the raw hardware (PS/2 Set-1) scancode from the WM_KEYDOWN message; on
// Linux (X11 and Wayland alike) it's the platform keycode, which is the
// evdev keycode + 8. The two disagree for exactly the letters this function
// cares about -- e.g. T is 0x14 on Windows but 0x1C on Linux, which
// coincides with the *Windows* PS/2 code for Enter, so treating Linux
// scancodes with the Windows ranges silently reclassified T (and Y/U/I/O/P)
// as non-character keys. Confirmed live: physical T on a Linux client sent
// VK_RETURN to the remote host instead of the letter t (see moonlightVKCode's
// scanCode==0x1C special case below, which then took over).
func isCharacterScanCode(scanCode int) bool {
	if runtime.GOOS == "linux" {
		switch {
		case scanCode >= 0x0A && scanCode <= 0x15: // number row + - =
			return true
		case scanCode >= 0x18 && scanCode <= 0x23: // Q..P [ ]
			return true
		case scanCode >= 0x26 && scanCode <= 0x33: // A..L ; ' ` \
			return true
		case scanCode >= 0x34 && scanCode <= 0x3D: // Z..M , . /
			return true
		case scanCode == 0x41: // Space
			return true
		default:
			return false
		}
	}
	// Windows (and, as before, everything else -- unconfirmed but unchanged).
	switch {
	case scanCode >= 0x02 && scanCode <= 0x0D: // number row + - =
		return true
	case scanCode >= 0x10 && scanCode <= 0x1B: // Q..P [ ]
		return true
	case scanCode >= 0x1E && scanCode <= 0x2B: // A..L ; ' ` \
		return true
	case scanCode >= 0x2C && scanCode <= 0x35: // Z..M , . /
		return true
	case scanCode == 0x39: // Space
		return true
	default:
		return false
	}
}

// isEnterScanCode reports whether a scan code is the physical Return/Enter
// key position, in whichever scan-code space GLFW reports for the current
// OS (see isCharacterScanCode's doc comment). Used only as a fallback for
// when event.Name fails to resolve to "Return"/"Enter" -- e.g. numpad Enter
// on some layouts. Must stay platform-gated: the Windows PS/2 code for
// Enter (0x1C) is the Linux X11/xkb keycode for the letter T.
func isEnterScanCode(scanCode int) bool {
	if runtime.GOOS == "linux" {
		return scanCode == 0x24 || scanCode == 0x68 // Return / KP_Enter (X11 keycodes)
	}
	return scanCode == 0x1C || scanCode == 0x11C // Windows PS/2 Return / extended (numpad) Return
}

// moonlightVKCode resolves the Windows Virtual Key code for a physical key event.
// Primary: look up by Fyne key name (works for all layouts that produce ASCII names).
// Fallback: use the hardware scan code — this handles non-Latin layouts (Russian,
// Ukrainian, etc.) where GLFW returns KeyUnknown as the key name but still reports
// the correct PS/2 scan code for the physical key position.
func moonlightVKCode(event *fyne.KeyEvent) int16 {
	keyStr := strings.ToLower(string(event.Name))
	if keyStr == "return" || keyStr == "enter" || keyStr == "keyenter" || isEnterScanCode(event.Physical.ScanCode) {
		return 0x0D // VK_RETURN
	}
	if vk := input.GetVKCode(event.Name); vk != 0 {
		return vk
	}
	return input.GetVKCodeFromScanCode(event.Physical.ScanCode)
}

// handlePhysicalKeyPress handles TypedKey from the physical keyboard.
// In Moonlight-only mode keys go through handlePhysicalKeyDown/Up.
// TypedKey is suppressed to avoid turbo (OS repeat) — exception: we ignore KeyF11.
func (vw *VideoWidget) handlePhysicalKeyPress(event *fyne.KeyEvent) {
	if event.Name == fyne.KeyF11 {
		return
	}
	if mi := vw.moonlightInput(); mi != nil {
		return // Moonlight: keys go via KeyDown/KeyUp, not TypedKey
	}
}

// handlePhysicalRunePress sends the character Fyne resolved for this
// keystroke (using whatever OS keyboard layout is active on the client).
// handlePhysicalKeyDown/Up skip sending for plain character keys (see
// isCharacterScanCode) precisely so this is the only thing that sends
// printable text, avoiding a double send of the same keystroke.
//
// Prefer a raw HID keycode + Shift bit (via GetRuneKeyCodeWithModifiers)
// over Moonlight's UTF8-text control channel whenever the resolved rune maps
// onto a normal key position -- which covers essentially all ASCII, plus
// Cyrillic folded onto its ЙЦУКЕН/QWERTY physical position. Falls back to
// SendMoonlightUtf8Text only for runes with no such mapping (other
// non-Latin scripts, emoji, ...).
//
// This isn't just a style preference: confirmed live against real Sunshine
// (itsme228/Sunshine fork, i.e. stock LizardByte Sunshine here) on Linux,
// its Unicode-text injection (platform/linux/input/inputtino_keyboard.cpp,
// unicode()) works by holding Shift and "typing" a Ctrl+Shift+U <hex digits>
// sequence -- the standard IBus/GTK Unicode-entry convention. That sequence
// only actually composes into the target character inside apps that
// implement it; anywhere else (most non-GTK apps, games, terminals) the
// keystrokes land literally, WITH SHIFT HELD, for every character in the
// whole run. E.g. digit '1' is U+0031, hex "31" -- typed as Shift+3 Shift+1
// on a US layout, i.e. "#!" instead of "1". A raw keycode+Shift press avoids
// that fragile IME dependency entirely and is understood by every Moonlight
// host (Sunshine, RustShine, GeForce Experience...) the same way, so use it
// whenever available -- EXCEPT on a Windows agent, where Sunshine's unicode()
// uses SendInput+KEYEVENTF_UNICODE (a low-level OS facility, not dependent on
// IME/app support, same reliability class as this VK+Shift path) -- so there's
// nothing broken to work around, and UTF8 text stays the original, simpler
// send for that target. See platform/linux/input/inputtino_keyboard.cpp's
// unicode() (Linux, the IBus hack above) vs. platform/windows/input.cpp's
// unicode() (Windows, SendInput) vs. platform/macos/input.cpp's unicode()
// (macOS -- now also CGEventKeyboardSetUnicodeString, itself just as
// reliable, but the client-side route below is still used there too since
// it doesn't depend on the agent shipping that fix).
func (vw *VideoWidget) handlePhysicalRunePress(r rune) {
	mi := vw.moonlightInput()
	if mi == nil {
		return
	}
	// Sticky soft IME is owned by KeyboardBridge.onIMETextInput — ignore
	// Fyne keyboardTyped runes (Press/Release doubles + composition junk).
	if vw.IsSystemIMESticky() {
		return
	}
	if r > 127 {
		vw.sendSoftIMERune(r)
		return
	}
	if !vw.isWindowsAgent() {
		if hidCode, hidMods := input.GetRuneKeyCodeWithModifiers(r); hidCode != 0 {
			vk := hidKeyToVK(hidCode)
			mods := widgetToMoonlightModifiers(hidMods)
			vw.enqueueSend(func() { mi.SendMoonlightKey(vk, service.LiKeyActionDown, mods) })
			vw.enqueueSend(func() { mi.SendMoonlightKey(vk, service.LiKeyActionUp, mods) })
			return
		}
	}
	vw.enqueueSend(func() { mi.SendMoonlightUtf8Text(string(r)) })
}

// sendSoftIMERune sends one Unicode character to the host, collapsing the
// duplicate TypedRune that Fyne delivers for keyboardTyped Press+Release.
func (vw *VideoWidget) sendSoftIMERune(r rune) {
	if r == 0 {
		return
	}
	now := time.Now()
	vw.softIMEMu.Lock()
	if r == vw.softIMELastRune && now.Sub(vw.softIMELastAt) < 45*time.Millisecond {
		vw.softIMEMu.Unlock()
		return
	}
	vw.softIMELastRune = r
	vw.softIMELastAt = now
	vw.softIMEMu.Unlock()

	mi := vw.moonlightInput()
	if mi == nil {
		return
	}
	vw.enqueueSend(func() { mi.SendMoonlightUtf8Text(string(r)) })
}

// isWindowsAgent reports whether the connected agent's OS (reported via
// SetAgentEnvironment) is Windows. Defaults to false (i.e. treated the same
// as Linux/macOS) when the agent hasn't reported its OS yet, since the
// VK+Shift route in handlePhysicalRunePress is safe there too -- it's only
// Windows that has an equally-reliable alternative worth preserving as-is.
func (vw *VideoWidget) isWindowsAgent() bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(vw.agentOS)), "windows")
}

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

// handleVirtualKeyPress handles virtual keyboard key presses via Moonlight.
// Virtual keyboard buttons use USB HID keycodes; convert to Windows VK codes first.
// A 200 ms per-key cooldown prevents turbo from OS key-repeat or widget duplicate events.
// The server already adds its own 50 ms hold delay for BIOS compatibility, so we send
// KeyDown + KeyUp back-to-back without additional client-side sleep.
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
	mi := vw.moonlightInput()
	if mi == nil {
		return
	}
	moonlightMods := widgetToMoonlightModifiers(modifiers)
	vw.enqueueSend(func() { mi.SendMoonlightKey(vk, service.LiKeyActionDown, moonlightMods) })
	vw.enqueueSend(func() { mi.SendMoonlightKey(vk, service.LiKeyActionUp, moonlightMods) })
}

// startDesktopMousePolling starts the polling goroutine for smooth mouse control.
func (vw *VideoWidget) startDesktopMousePolling() {
	vw.stopDesktopMousePolling()
	vw.mousePollingQuit = make(chan bool)
	vw.startMouseMoveWorker() // ensure relative-move flush goroutine is running
	logrus.Info("🖱️ Starting desktop mouse polling (60 FPS)")
	go vw.processDesktopMousePolling()
}

// stopDesktopMousePolling stops the polling goroutine.
func (vw *VideoWidget) stopDesktopMousePolling() {
	if vw.mousePollingQuit != nil {
		close(vw.mousePollingQuit)
		vw.mousePollingQuit = nil
		logrus.Info("🖱️ Desktop mouse polling stopped")
	}
}

// processDesktopMousePolling handles mouse movement at a fixed rate.
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

// processMouseMovement handles the current mouse movement.
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
		// Only promote to drag once cursor has moved > 5dp from the press point.
		// Without this deadzone, 1-2px mouse jitter during a click sets isDragging,
		// which suppresses MouseUp click detection — especially severe with Raw Input
		// (Windows) or NSTrackingArea (Mac) that deliver every hardware sample.
		ddx := vw.currentMouseX - vw.touchStartX
		ddy := vw.currentMouseY - vw.touchStartY
		if ddx*ddx+ddy*ddy >= 25 { // 5dp radius
			vw.isDragging = true
			logrus.Debugf("🖱️ ✨ Drag/swipe STARTED (desktop touchpad mode, polling)")
		}
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

// isPositionInContentRect reports whether the local widget position (px, py) falls
// within the actual video content area, accounting for letterbox/pillarbox black bars.
// Returns true when the content rect is not yet established (allows clicks through).
func (vw *VideoWidget) isPositionInContentRect(px, py float32) bool {
	x, y, w, h := vw.absolutePictureRect()
	if w <= 0 || h <= 0 {
		return true
	}
	return px >= x && px <= x+w && py >= y && py <= y+h
}

func (vw *VideoWidget) UsesRelativeMouseInput() bool {
	return vw.GetMouseInputMode() == mouseModeTouchPad
}

// GetMouseInputMode returns the pointer device type.
func (vw *VideoWidget) GetMouseInputMode() string {
	if vw.mouseInputMode == "" {
		vw.mouseInputMode = defaultMouseMode()
	}
	return normalizeMouseMode(vw.mouseInputMode)
}

// SetMouseInputMode sets the pointer device type.
func (vw *VideoWidget) SetMouseInputMode(mode string) {
	mode = normalizeMouseMode(mode)
	vw.mouseInputMode = mode
	vw.SetShowMouseCursor(vw.GetShowMouseCursor())
	vw.resetRelativeMoveAccumulator()
	if isVirtualCursorLikeMode(mode) {
		// Centre the virtual cursor and set cursor scale for the current display.
		vw.vcMu.Lock()
		vw.virtualCursorU = 0.5
		vw.virtualCursorV = 0.5
		vw.vcMu.Unlock()
		s := vw.androidCursorScale()
		if s > 0 {
			vw.initAndroidCursorScale(s)
		}
	}
	vw.updateNativeViewportAndCursor()
	vw.logMouseModeState("desired-updated")
}

func (vw *VideoWidget) setObservedMouseMode(mode string) {
	vw.observedMouseMode = normalizeMouseMode(mode)
	vw.logMouseModeState("observed-updated")
}

// GetShowMouseCursor returns the flag for showing the cursor in the captured video.
func (vw *VideoWidget) GetShowMouseCursor() bool {
	return vw.showMouseCursor
}

// SetShowMouseCursor sets the flag for showing the cursor in the captured video.
func (vw *VideoWidget) SetShowMouseCursor(show bool) {
	if vw.showMouseCursor == show {
		return
	}
	vw.showMouseCursor = show
	vw.refreshCursorOverlay()
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

// SendAbsolutePosition sends the absolute position with a small debounce.
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

	if mi := vw.moonlightInput(); mi != nil {
		cnt := vw.statAbsMoonlight.Add(1)
		if cnt == 1 || cnt%100 == 0 {
			logrus.Infof("🖱️ [Mouse] → LiSendMousePositionEvent cnt=%d x=%d y=%d active=%v", cnt, x, y, mi.IsInputActive())
		}
		vw.enqueueSend(func() { mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767) })
		if scroll != 0 {
			vw.enqueueSend(func() { mi.SendMoonlightScroll(int8(scroll)) })
		}
	}
}

// SendAbsoluteEvent sends an atomic absolute event.
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
		moonlightBtn := absoluteButtonToMoonlight(button)
		vw.enqueueSend(func() { mi.SendMoonlightMouseButton(service.LiMouseButtonPress, moonlightBtn) })
		vw.enqueueSend(func() { mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767) })
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
		moonlightBtn := absoluteButtonToMoonlight(button)
		vw.enqueueSend(func() { mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, moonlightBtn) })
		vw.enqueueSend(func() { mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767) })
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
			vw.enqueueSend(func() { mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, service.LiMouseButtonLeft) })
		}
		if vw.absButtons&0x02 != 0 {
			vw.enqueueSend(func() { mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, service.LiMouseButtonRight) })
		}
		if vw.absButtons&0x04 != 0 {
			vw.enqueueSend(func() { mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, service.LiMouseButtonMiddle) })
		}
		vw.absButtons = 0
		vw.enqueueSend(func() { mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767) })
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
		vw.enqueueSend(func() { mi.SendMoonlightMouseButton(service.LiMouseButtonPress, moonlightBtn) })
		vw.enqueueSend(func() { mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767) })
		vw.enqueueSend(func() { mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, moonlightBtn) })
		vw.enqueueSend(func() { mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767) })
		vw.lastAbsX = x
		vw.lastAbsY = y
		return
	}
}

// ForceReleaseStuckMouseButton delegates to TouchpadWrapper's own method of
// the same name -- see its doc comment (video_mouse_handler.go) for why
// this exists as a distinct thing from the ordinary MouseUp/MouseOut path.
// Exported here (not called directly on touchpadWrapper) so wasm-only
// callers outside this package's Fyne-widget machinery (video_gestures_wasm.go's
// global "contextmenu"/"blur" listeners) don't need touchpadWrapper's own
// unexported type.
func (vw *VideoWidget) ForceReleaseStuckMouseButton(reason string) {
	if vw.touchpadWrapper != nil {
		vw.touchpadWrapper.ForceReleaseStuckButton(reason)
	}
}

// CancelTouchDownDelay cancels the deferred touch(down) send.
func (vw *VideoWidget) CancelTouchDownDelay() {
	vw.touchDownDelayMu.Lock()
	defer vw.touchDownDelayMu.Unlock()
	if vw.touchDownDelayTimer != nil {
		vw.touchDownDelayTimer.Stop()
		vw.touchDownDelayTimer = nil
	}
}

// StartTouchDownDelay schedules the touch(down) send.
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

// TryRecordTouchDown records "sending touch(down)".
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

// UpdateTouchpadAndContentRect updates the input area size and the video rectangle.
func (vw *VideoWidget) UpdateTouchpadAndContentRect(w, h float32, frame image.Image) {
	// While standalone VK fullscreen is active, the underlying Fyne video widget
	// still exists (hidden behind the VK overlay) and keeps re-triggering this via
	// its own Resize/Layout/input handlers (TouchpadWrapper.updateTouchpadSize,
	// wrapperLayoutImage, etc.) with its own pre-fullscreen widget size. Force the
	// screen size here so it wins no matter which call site fires.
	if vw.standaloneVKScreenDpW > 0 && vw.standaloneVKScreenDpH > 0 {
		w, h = vw.standaloneVKScreenDpW, vw.standaloneVKScreenDpH
	}
	if w <= 0 || h <= 0 {
		return
	}
	vw.touchpadSizeW = w
	vw.touchpadSizeH = h
	vw.contentRectX = 0
	vw.contentRectY = 0
	vw.contentRectW = w
	vw.contentRectH = h

	availableH := h - vw.bottomInset
	if availableH < 0 {
		availableH = 0
	}

	imgW, imgH := vw.resolveStreamPixelSize(frame)
	if imgW > 0 && imgH > 0 {
		baseW, baseH := aspectFitSize(w, availableH, imgW, imgH)
		vw.baseContentRectW = baseW
		vw.baseContentRectH = baseH
	} else {
		vw.baseContentRectW = w
		vw.baseContentRectH = availableH
	}
	vw.recalculateViewport()
	vw.applyNativeDestToContentRect()
	vw.updateInStreamContentRect()
	// This runs on the UI goroutine on every widget Refresh/Layout — i.e. once
	// per rendered video frame (see touchpadRenderer.Refresh in
	// video_mouse_handler.go) — so an unconditional Info-level log here was a
	// synchronous logcat/file write on every single frame, competing with
	// actual layout work. Debug-gate it like the rest of the per-frame paths.
	logrus.Debugf("[ABS] UpdateTouchpadAndContentRect: touchpad=%.0fx%.0f img=%.0fx%.0f base=%.0fx%.0f standalone=%.0fx%.0f → contentRect=(%.1f,%.1f,%.1f,%.1f)",
		w, h, imgW, imgH, vw.baseContentRectW, vw.baseContentRectH,
		vw.standaloneVKScreenDpW, vw.standaloneVKScreenDpH,
		vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH)
}

// PositionToAbsolute converts coordinates from the input area to absolute coordinates
// Windows-friendly absolute pointer descriptor (0..32767).
func (vw *VideoWidget) PositionToAbsolute(px, py float32) (x, y int) {
	if vw.touchpadSizeW <= 0 || vw.touchpadSizeH <= 0 {
		// Widget not yet sized — return center instead of (0,0) so the cursor
		// doesn't snap to the top-left corner before the first frame arrives.
		return 16383, 16383
	}

	rectX, rectY, rectW, rectH := vw.absolutePictureRect()

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
	// Log at most once per 2 seconds to diagnose coordinate mapping without spamming.
	if now := time.Now(); now.Sub(absLogAt) >= 2*time.Second {
		absLogAt = now
		frameX, frameY, frameW, frameH := vw.getFrameContentRect()
		hostW, hostH := vw.hostDesktopSize()
		logrus.Infof("[ABS] PositionToAbsolute: in=(%.1f,%.1f) touchpad=(%.0f,%.0f) picture=(%.1f,%.1f,%.1f,%.1f) contentRect=(%.1f,%.1f,%.1f,%.1f) host=%.0fx%.0f stream=%.0fx%.0f frameRect=(%.3f,%.3f,%.3f,%.3f) u=%.3f v=%.3f → out=(%d,%d)",
			px, py,
			vw.touchpadSizeW, vw.touchpadSizeH,
			rectX, rectY, rectW, rectH,
			vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH,
			hostW, hostH, vw.lastVideoImgW, vw.lastVideoImgH,
			frameX, frameY, frameW, frameH,
			u, v, x, y)
	}
	return x, y
}

// absolutePictureRect is the on-screen desktop picture in the same dp space as
// pointer events. Starts from the native overlay dest (client letterbox of the
// encoded frame), then applies frameContent (in-stream contain-fit of the host
// monitor into the encode — RustShine only; Sunshine already maps stream space).
func (vw *VideoWidget) absolutePictureRect() (rectX, rectY, rectW, rectH float32) {
	if dx, dy, dw, dh, ok := vw.nativeDestRectDp(); ok {
		rectX, rectY, rectW, rectH = dx, dy, dw, dh
	} else {
		rectX = vw.contentRectX
		rectY = vw.contentRectY
		rectW = vw.contentRectW
		rectH = vw.contentRectH
	}

	frameX, frameY, frameW, frameH := vw.getFrameContentRect()
	if rectW > 0 && rectH > 0 && frameW > 0 && frameH > 0 {
		rectX += rectW * frameX
		rectY += rectH * frameY
		rectW *= frameW
		rectH *= frameH
	}
	return rectX, rectY, rectW, rectH
}

func (vw *VideoWidget) nativeDestRectDp() (x, y, w, h float32, ok bool) {
	dest, ok := service.NativeVideoDestRect()
	if !ok || dest.DW <= 0 || dest.DH <= 0 {
		return 0, 0, 0, 0, false
	}
	scale := vw.nativeOverlayScale()
	if scale <= 0 {
		scale = 1
	}
	ox, oy := vw.nativePointerOriginDp()
	return ox + float32(dest.DX)/scale, oy + float32(dest.DY)/scale, float32(dest.DW) / scale, float32(dest.DH) / scale, true
}

func (vw *VideoWidget) nativeOverlayScale() float32 {
	if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
		if s := vw.parentWindow.Canvas().Scale(); s > 0 {
			return s
		}
	}
	return 1
}

// aspectFitSize is ImageFillContain: the largest srcW×srcH rect that fits in
// viewW×viewH without cropping. Matches Vulkan vk_layout_zoomed_dest at zoom=1
// (letterbox when the stream is wider than the widget, pillarbox when taller).
func aspectFitSize(viewW, viewH, srcW, srcH float32) (baseW, baseH float32) {
	if srcW <= 0 || srcH <= 0 || viewW <= 0 || viewH <= 0 {
		return viewW, viewH
	}
	scale := viewW / srcW
	if viewH/srcH < scale {
		scale = viewH / srcH
	}
	return srcW * scale, srcH * scale
}

// containFitNorm is the normalized inner rect of innerW×innerH contain-fitted
// into containerW×containerH. Used for in-stream letterbox on RustShine: the
// host desktop is fitted into the encode, and 0..32767 maps onto that inner
// rect. Sunshine applies the same contain-fit on the host (touch_port) and
// must receive stream-space coordinates instead.
func containFitNorm(containerW, containerH, innerW, innerH float32) (x, y, w, h float32) {
	if containerW <= 0 || containerH <= 0 || innerW <= 0 || innerH <= 0 {
		return 0, 0, 1, 1
	}
	cAspect := containerW / containerH
	iAspect := innerW / innerH
	const eps = 0.004
	if math.Abs(float64(iAspect-cAspect)) <= float64(cAspect)*eps {
		return 0, 0, 1, 1
	}
	if iAspect > cAspect {
		w = 1
		h = cAspect / iAspect
		y = (1 - h) / 2
		return 0, y, w, h
	}
	h = 1
	w = iAspect / cAspect
	x = (1 - w) / 2
	return x, 0, w, h
}

func parseWxH(s string) (w, h float32, ok bool) {
	inner := strings.TrimSpace(s)
	if open, close := strings.LastIndex(inner, "("), strings.LastIndex(inner, ")"); open >= 0 && close > open {
		inner = strings.TrimSpace(inner[open+1 : close])
	}
	x := strings.IndexByte(inner, 'x')
	if x <= 0 {
		x = strings.IndexByte(inner, 'X')
	}
	if x <= 0 || x >= len(inner)-1 {
		return 0, 0, false
	}
	wi, err1 := strconv.Atoi(strings.TrimSpace(inner[:x]))
	hi, err2 := strconv.Atoi(strings.TrimSpace(inner[x+1:]))
	if err1 != nil || err2 != nil || wi <= 0 || hi <= 0 {
		return 0, 0, false
	}
	return float32(wi), float32(hi), true
}

func hostDesktopSizeFromModes(modes []models.VideoCaptureMode) (w, h float32) {
	best := int64(0)
	for _, m := range modes {
		area := int64(m.Width) * int64(m.Height)
		if area > best {
			best = area
			w, h = float32(m.Width), float32(m.Height)
		}
	}
	return w, h
}

func (vw *VideoWidget) setHostDesktopSize(w, h float32) {
	if w <= 0 || h <= 0 {
		return
	}
	vw.frameMutex.Lock()
	vw.hostDesktopW, vw.hostDesktopH = w, h
	vw.frameMutex.Unlock()
	vw.updateInStreamContentRect()
}

func (vw *VideoWidget) hostDesktopSize() (float32, float32) {
	vw.frameMutex.RLock()
	defer vw.frameMutex.RUnlock()
	return vw.hostDesktopW, vw.hostDesktopH
}

func (vw *VideoWidget) rememberHostDesktopFromInfo(info *models.VideoInfoData) {
	if info == nil {
		return
	}
	w, h := hostDesktopSizeFromModes(info.CaptureModes)
	if info.Width > 0 && info.Height > 0 {
		if area := float32(info.Width) * float32(info.Height); area > w*h {
			w, h = float32(info.Width), float32(info.Height)
		}
	}
	if nw, nh, ok := parseWxH(info.Device); ok && nw*nh > w*h {
		w, h = nw, nh
	}
	vw.setHostDesktopSize(w, h)
}

func (vw *VideoWidget) rememberHostDesktopFromConfig(cfg models.VideoDeviceConfig) {
	if w, h, ok := parseWxH(cfg.DeviceName); ok {
		vw.setHostDesktopSize(w, h)
	}
	if info, ok := cachedCaptureInfo(cfg.DevicePath); ok {
		vw.rememberHostDesktopFromInfo(info)
	}
}

func (vw *VideoWidget) ensureHostDesktopSize() {
	if w, h := vw.hostDesktopSize(); w > 0 && h > 0 {
		return
	}
	captureModesCacheMu.Lock()
	cw, ch := captureHostDesktopW, captureHostDesktopH
	captureModesCacheMu.Unlock()
	if cw > 0 && ch > 0 {
		vw.setHostDesktopSize(cw, ch)
	}
}

func (vw *VideoWidget) applyNativeDestToContentRect() {
	x, y, w, h, ok := vw.nativeDestRectDp()
	if !ok {
		return
	}
	vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH = x, y, w, h
}

func (vw *VideoWidget) updateInStreamContentRect() {
	vw.updateInStreamContentRectWith(vw.lastVideoImgW, vw.lastVideoImgH)
}

func (vw *VideoWidget) updateInStreamContentRectWith(streamW, streamH float32) {
	vw.ensureHostDesktopSize()
	var x, y, w, h float32
	if vw.hostMapsMouseInStreamSpace() {
		// Sunshine's touch_port already contain-fits the desktop into the
		// encode (client_offset / scalar_inv). 0..32767 must stay in stream
		// space — the same mapping Moonlight-qt sends. Cropping to the inner
		// desktop here double-applies that letterbox: the host cursor lags
		// and never reaches the physical edge.
		x, y, w, h = 0, 0, 1, 1
	} else {
		hostW, hostH := vw.hostDesktopSize()
		x, y, w, h = containFitNorm(streamW, streamH, hostW, hostH)
	}
	vw.frameMutex.Lock()
	vw.frameContentX, vw.frameContentY, vw.frameContentW, vw.frameContentH = x, y, w, h
	vw.frameMutex.Unlock()
	if w < 0.999 || h < 0.999 {
		if now := time.Now(); now.Sub(inStreamLogAt) >= 2*time.Second {
			inStreamLogAt = now
			hostW, hostH := vw.hostDesktopSize()
			logrus.Infof("[ABS] in-stream crop: host=%.0fx%.0f stream=%.0fx%.0f frameRect=(%.3f,%.3f,%.3f,%.3f)",
				hostW, hostH, streamW, streamH, x, y, w, h)
		}
	}
}

// SetAgentProtocol records the connected agent's streamer (opensource =
// Sunshine, otherwise RustShine). Absolute mouse mapping differs: Sunshine
// consumes stream-space coordinates, RustShine consumes desktop-space.
func (vw *VideoWidget) SetAgentProtocol(protocol string) {
	p := strings.TrimSpace(protocol)
	if vw.agentProtocol == p {
		return
	}
	vw.agentProtocol = p
	vw.updateInStreamContentRect()
	logrus.Infof("[ABS] agent protocol=%q stream-space mouse=%v", p, vw.hostMapsMouseInStreamSpace())
}

func (vw *VideoWidget) hostMapsMouseInStreamSpace() bool {
	switch strings.ToLower(strings.TrimSpace(vw.agentProtocol)) {
	case "opensource", "open source", "sunshine":
		return true
	default:
		return false
	}
}

func (vw *VideoWidget) refreshAgentProtocol() {
	if vw == nil || vw.usbClient == nil {
		return
	}
	if info, err := vw.usbClient.GetDeviceInfo(); err == nil && info != nil {
		if p := strings.TrimSpace(info.AgentProtocol); p != "" {
			vw.SetAgentProtocol(p)
			return
		}
	}
	if status, err := vw.usbClient.GetStatus(); err == nil && status != nil && status.Data != nil {
		if p := strings.TrimSpace(status.Data.AgentProtocol); p != "" {
			vw.SetAgentProtocol(p)
		}
	}
}

func (vw *VideoWidget) resolveStreamPixelSize(frame image.Image) (imgW, imgH float32) {
	if frame != nil {
		b := frame.Bounds()
		fw, fh := float32(b.Dx()), float32(b.Dy())
		if fw > 0 && fh > 0 {
			vw.lastVideoImgW = fw
			vw.lastVideoImgH = fh
			return fw, fh
		}
	}
	if vw.lastVideoImgW > 0 && vw.lastVideoImgH > 0 {
		return vw.lastVideoImgW, vw.lastVideoImgH
	}
	if nw, nh := service.NativeFrameSize(); nw > 0 && nh > 0 {
		vw.lastVideoImgW = float32(nw)
		vw.lastVideoImgH = float32(nh)
		return vw.lastVideoImgW, vw.lastVideoImgH
	}
	if ms, ok := vw.videoClient.(*service.MoonlightService); ok {
		if sw, sh := ms.StreamPixelSize(); sw > 0 && sh > 0 {
			vw.lastVideoImgW = float32(sw)
			vw.lastVideoImgH = float32(sh)
			return vw.lastVideoImgW, vw.lastVideoImgH
		}
	}
	return 0, 0
}

func (vw *VideoWidget) refreshContentRectFromTouchpad() {
	w, h := vw.touchpadSizeW, vw.touchpadSizeH
	if tw := vw.activeViewportWrapper(); tw != nil {
		if sz := tw.Size(); sz.Width > 0 && sz.Height > 0 {
			w, h = sz.Width, sz.Height
		}
	}
	if w > 0 && h > 0 {
		vw.UpdateTouchpadAndContentRect(w, h, nil)
	}
}

func (vw *VideoWidget) noteStreamPixelSize(w, h float32) {
	if w <= 0 || h <= 0 {
		return
	}
	if vw.lastVideoImgW == w && vw.lastVideoImgH == h {
		return
	}
	vw.lastVideoImgW = w
	vw.lastVideoImgH = h
	vw.refreshContentRectFromTouchpad()
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

	// On native GPU render paths (Vulkan/Metal) the canvas image is cleared to nil,
	// so the touchpad wrapper passes frame=nil to UpdateTouchpadAndContentRect on
	// resize events. lastVideoImgW/H then stays at zero/stale, baseContentRectW
	// defaults to touchpadSizeW, and display-level letterbox bars are never subtracted
	// from absolute mouse coordinates. Sync the real frame dimensions here — where we
	// always have the actual decoded frame — and recalculate the viewport if they changed.
	// Use activeViewportWrapper so we always call UpdateTouchpadAndContentRect with
	// the actual current size of whichever wrapper is active (fullscreen or main).
	newFW, newFH := float32(frameW), float32(frameH)
	fyne.Do(func() {
		if vw.lastVideoImgW != newFW || vw.lastVideoImgH != newFH {
			vw.lastVideoImgW = newFW
			vw.lastVideoImgH = newFH
			// In standalone VK fullscreen the touchpad is sized to the screen, not to
			// the main-window widget. Use the stored screen dp size so PositionToAbsolute
			// always normalises against the full screen, even as video frames arrive.
			if vw.standaloneVKScreenDpW > 0 {
				logrus.Infof("[ABS] updateFrameContentRect: standalone → screen=%.0fx%.0f img=%.0fx%.0f",
					vw.standaloneVKScreenDpW, vw.standaloneVKScreenDpH, newFW, newFH)
				vw.UpdateTouchpadAndContentRect(vw.standaloneVKScreenDpW, vw.standaloneVKScreenDpH, nil)
			} else if tw := vw.activeViewportWrapper(); tw != nil {
				sz := tw.Size()
				logrus.Infof("[ABS] updateFrameContentRect: widget → size=%.0fx%.0f img=%.0fx%.0f",
					sz.Width, sz.Height, newFW, newFH)
				if sz.Width > 0 && sz.Height > 0 {
					var frame image.Image
					if tw.image != nil {
						frame = tw.image.Image
					}
					vw.UpdateTouchpadAndContentRect(sz.Width, sz.Height, frame)
				}
			}
		}
	})

	if vw.moonlightInput() != nil {
		// Do not run the dark-pixel heuristic on desktop capture: a black
		// terminal at the edge looks like a letterbox bar and collapses the
		// mouse rect. Both hosts contain-fit the monitor into the encode when
		// aspects differ. RustShine maps 0..32767 onto the desktop (crop
		// here); Sunshine's touch_port already subtracts those bars.
		vw.updateInStreamContentRectWith(float32(frameW), float32(frameH))
		return
	}

	left := detectDarkInset(frame, bounds, true, true)
	right := detectDarkInset(frame, bounds, true, false)
	top := detectDarkInset(frame, bounds, false, true)
	bottom := detectDarkInset(frame, bounds, false, false)

	// Only allow the crop if the bar is symmetric (±20px) and not too small
	// (noise) and does not take up more than 48% of the side (guards against a fully dark frame).
	// Real letterbox/pillarbox bars are usually 5-45% of the side and can be slightly asymmetric.
	const minMeaningfulCropInsetPx = 2
	const maxCropAsymmetryPx = 5
	maxHInset := frameW * 12 / 25 // 48%
	maxVInset := frameH * 12 / 25 // 48%
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

	logrus.Debugf("Inset: L=%d R=%d T=%d B=%d (w=%d, h=%d)", left, right, top, bottom, frameW, frameH)

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
	limit := bounds.Dx() * 12 / 25 // 48% — matches maxHInset/maxVInset check in updateFrameContentRect
	if !vertical {
		limit = bounds.Dy() * 12 / 25
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
		if darkRatio < 0.90 {
			return offset
		}
	}

	return limit
}

// isNearBlack tolerates software-encoder quantization noise in otherwise
// solid letterbox bars. A hardware KVM's bars decode essentially pure black
// (RGB ~0), but a software-encoded stream over a bandwidth-constrained link
// (e.g. a Tailscale DERP relay) can leave visible macroblock noise in flat
// dark regions — individual samples several times brighter than pure black
// even though the row is still clearly a letterbox bar, not video content.
// A too-strict cutoff here (and detectDarkInset's old 98% dark-ratio
// requirement) made detection silently fail on those streams, so the bars
// got treated as part of the clickable video field and absolute-mouse
// coordinates drifted increasingly off target toward the edges.
func isNearBlack(c color.Color) bool {
	r, g, b, a := c.RGBA()
	if a < 0x2000 {
		return true
	}
	const maxDark = 40 << 8
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

	// Compute the available height for the video (minus the buttons at the bottom)
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
	// Soft-snap near 1x without wiping an explicit pan.
	if scale <= 1.001 {
		scale = 1
		vw.zoomScale = 1
	}

	contentW := baseW * scale
	contentH := baseH * scale

	// panOffset is a delta from the centered position.
	//
	// Overflow (content > view): hard clamp so edges never reveal empty/black.
	// Fit (content ≤ view), including zoomed-but-still-letterboxed: free pan with
	// a min-visible floor. Previously we forced pan=0 on fitting axes whenever
	// zoom>1 — that snapped zoom-after-pan back to center and made post-zoom
	// drag feel broken on portrait (height often still fits after moderate zoom).
	const minVisible = float32(0.3)

	centerX := (vw.touchpadSizeW - contentW) / 2
	if contentW > vw.touchpadSizeW {
		maxPanX := (contentW - vw.touchpadSizeW) / 2
		vw.panOffsetX = clampFloat(vw.panOffsetX, -maxPanX, maxPanX)
	} else {
		minX := -contentW * (1 - minVisible)
		maxX := vw.touchpadSizeW - contentW*minVisible
		contentX := clampFloat(centerX+vw.panOffsetX, minX, maxX)
		vw.panOffsetX = contentX - centerX
	}
	contentX := centerX + vw.panOffsetX

	var contentY float32
	centerY := (availableH - contentH) / 2
	// While the keyboard stack is open, allow extra upward pan so a caret at
	// the remote bottom edge can sit well above the system IME (black gap
	// under the picture is OK — better than typing under the keyboard).
	extraUp := float32(0)
	extraDown := float32(0)
	if vw.keyboardViewportLift {
		extraUp = availableH * keyboardFocusExtraLiftFrac
		if extraUp < keyboardFocusExtraLiftMinDp {
			extraUp = keyboardFocusExtraLiftMinDp
		}
		// Symmetric to extraUp: black above the picture so the top of the remote
		// screen can be panned down into view, same as the gap above the IME.
		extraDown = extraUp
		if r := vw.specialKeysHeaderReserve; r > extraDown {
			extraDown = r
		}
	}
	if vw.bottomAnchorContentVertically && contentH <= availableH {
		// wasm only: keep flush above the IME panel; no free letterbox pan.
		contentY = availableH - contentH
		vw.panOffsetY = 0
	} else if contentH > availableH {
		maxPanY := (contentH - availableH) / 2
		vw.panOffsetY = clampFloat(vw.panOffsetY, -maxPanY-extraUp, maxPanY+extraDown)
		contentY = centerY + vw.panOffsetY
	} else {
		minY := -contentH*(1-minVisible) - extraUp
		maxY := availableH - contentH*minVisible + extraDown
		contentY = clampFloat(centerY+vw.panOffsetY, minY, maxY)
		vw.panOffsetY = contentY - centerY
	}

	vw.contentRectX = contentX
	vw.contentRectY = contentY
	vw.contentRectW = contentW
	vw.contentRectH = contentH

	vw.debugLogViewport("recalc")
}

// snapViewportThresholdFrac is how close (fraction of view width) the release
// pan must be to a left/right flush target before we magnetize.
const snapViewportThresholdFrac = float32(0.03) // 3%

// snapViewportAlignment squares up horizontal edges after a two-finger gesture
// ends — only at 1x zoom, only left/right, never vertical or "center between
// the pillarbox". Zoomed pan must not be touched (cursor-follow / old center
// snap was fighting drag and pulling toward the right edge).
func (vw *VideoWidget) snapViewportAlignment() bool {
	if vw.touchpadSizeW <= 0 || vw.touchpadSizeH <= 0 {
		return false
	}
	if vw.zoomScale > 1.001 {
		return false
	}
	vw.recalculateViewport()

	contentW := vw.contentRectW
	if contentW <= 0 {
		return false
	}
	targets := viewportHorizontalEdgeTargets(vw.touchpadSizeW, contentW)
	if len(targets) == 0 {
		return false
	}
	thresh := vw.touchpadSizeW * snapViewportThresholdFrac
	snapped, ok := snapToNearestOffset(vw.panOffsetX, targets, thresh)
	if !ok {
		return false
	}
	vw.panOffsetX = snapped
	vw.recalculateViewport()
	return true
}

// viewportHorizontalEdgeTargets returns panOffset values that flush the
// content to the left or right of the view. panOffset is relative to center
// (see recalculateViewport). Center-between-edges is intentionally omitted.
func viewportHorizontalEdgeTargets(viewW, contentW float32) []float32 {
	if viewW <= 0 || contentW <= 0 {
		return nil
	}
	// Zoomed overflow is handled by refusing snap when zoom>1; at 1x content
	// should fit. If it somehow overflows, edge flush is ±maxPan — skip to
	// avoid the old "always magnetize to an edge" feel while zoomed.
	if contentW > viewW+0.5 {
		return nil
	}
	center := (viewW - contentW) / 2
	left := -center                    // contentX = 0
	right := viewW - contentW - center // contentX = viewW - contentW
	if almostEqual(left, right) {
		// Full-bleed width: both edges are the same pose (pan = 0).
		return []float32{0}
	}
	return []float32{left, right}
}

func snapToNearestOffset(val float32, targets []float32, thresh float32) (float32, bool) {
	if thresh < 0 {
		thresh = 0
	}
	best := val
	bestDist := thresh + 1
	found := false
	for _, t := range targets {
		d := float32(math.Abs(float64(val - t)))
		if d <= thresh && (!found || d < bestDist) {
			best = t
			bestDist = d
			found = true
		}
	}
	if !found {
		return val, false
	}
	if almostEqual(val, best) {
		return val, false
	}
	return best, true
}

func (vw *VideoWidget) GetViewportRect() (float32, float32, float32, float32) {
	vw.recalculateViewport()
	return vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH
}

func (vw *VideoWidget) applyViewportGesture(scaleFactor, focusX, focusY, panDx, panDy float32) {
	vw.debugLogGesture(scaleFactor, focusX, focusY, panDx, panDy)
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
	// Two-finger is pinch-only now, but per-frame scale is often ~1.005–1.015
	// during a slow pinch. Dropping those under a hard 2% deadzone made zoom
	// stall mid-gesture and then jump. Accumulate sub-threshold factors and
	// apply when the product crosses a small threshold.
	if scaleFactor <= 0 {
		scaleFactor = 1
	}
	const zoomDeadzone = float32(0.01) // 1%
	if vw.zoomScaleResidual <= 0 {
		vw.zoomScaleResidual = 1
	}
	if math.Abs(float64(scaleFactor-1)) < float64(zoomDeadzone) {
		vw.zoomScaleResidual *= scaleFactor
		if math.Abs(float64(vw.zoomScaleResidual-1)) < float64(zoomDeadzone) {
			scaleFactor = 1
		} else {
			scaleFactor = vw.zoomScaleResidual
			vw.zoomScaleResidual = 1
		}
	} else if vw.zoomScaleResidual != 1 {
		scaleFactor *= vw.zoomScaleResidual
		vw.zoomScaleResidual = 1
	}
	if scaleFactor > 0 && scaleFactor != 1 {
		nextZoom *= scaleFactor
	}
	nextZoom = clampFloat(nextZoom, 1, 6)
	vw.zoomScale = nextZoom
	vw.recalculateViewport()

	zoomed := scaleFactor > 0 && !almostEqual(scaleFactor, 1)
	if zoomed {
		availableH := vw.touchpadSizeH - vw.bottomInset
		if availableH < 0 {
			availableH = 0
		}
		// Zoom about the view centre — not the finger focus. Pinch focus Y
		// from Android (activity px → Fyne dp) sits systematically low vs the
		// Vulkan surface (header clearance / chrome), so focus-anchored zoom
		// walked the picture downward as scale grew. Anchoring the point that
		// is currently under the view centre keeps prior pan and avoids the
		// downward drift.
		anchorX := vw.touchpadSizeW / 2
		anchorY := availableH / 2
		u := clampFloat((anchorX-oldX)/oldW, 0, 1)
		v := clampFloat((anchorY-oldY)/oldH, 0, 1)
		newW := vw.contentRectW
		newH := vw.contentRectH
		baseX := (vw.touchpadSizeW - newW) / 2
		baseY := (availableH - newH) / 2
		vw.panOffsetX = anchorX - u*newW - baseX
		vw.panOffsetY = anchorY - v*newH - baseY
	}

	vw.panOffsetX += panDx
	vw.panOffsetY += panDy
	// Any two-finger viewport gesture owns pan/zoom until the user moves the
	// virtual cursor again (blocks cursor-follow from snapping back to center).
	vw.viewportManualControl = true
	vw.recalculateViewport()
}

func (vw *VideoWidget) resetViewport() {
	vw.zoomScale = 1
	vw.zoomScaleResidual = 1
	vw.panOffsetX = 0
	vw.panOffsetY = 0
	vw.viewportManualControl = false
	vw.recalculateViewport()
	vw.updateNativeViewportAndCursor()
}

// resetZoomScaleResidual clears pending sub-deadzone pinch accumulation.
func (vw *VideoWidget) resetZoomScaleResidual() {
	if vw != nil {
		vw.zoomScaleResidual = 1
	}
}

func clampFloat(value, minValue, maxValue float32) float32 {
	return float32(math.Max(float64(minValue), math.Min(float64(maxValue), float64(value))))
}

func almostEqual(a, b float32) bool {
	return math.Abs(float64(a-b)) < 0.001
}

// placeVirtualCursorAtViewCenterLocked writes virtualCursorU/V so the cursor
// sits on whatever remote point is currently under the centre of the view.
// Used when resuming from two-finger pan/zoom (RustDesk-style): move the
// mouse to us, do not yank the picture back to the old mouse side.
// Caller must hold vcMu.
func (vw *VideoWidget) placeVirtualCursorAtViewCenterLocked(minU, maxU, minV, maxV float32) {
	vw.recalculateViewport()
	cw, ch := vw.contentRectW, vw.contentRectH
	if cw <= 0 || ch <= 0 {
		vw.virtualCursorU = clampFloat(0.5, minU, maxU)
		vw.virtualCursorV = clampFloat(0.5, minV, maxV)
		return
	}
	availableH := vw.touchpadSizeH - vw.bottomInset
	if availableH < 0 {
		availableH = 0
	}
	sx := vw.touchpadSizeW / 2
	sy := availableH / 2
	u := (sx - vw.contentRectX) / cw
	v := (sy - vw.contentRectY) / ch
	vw.virtualCursorU = clampFloat(u, minU, maxU)
	vw.virtualCursorV = clampFloat(v, minV, maxV)
}

const (
	keyboardFocusZoom = float32(2)
	// Centre of the visible strip above the IME (not the old upper-third
	// 0.28), so the caret is pushed toward the middle of the remaining screen.
	keyboardFocusYFrac          = float32(0.5)
	keyboardFocusClearanceDp    = float32(24)
	keyboardFocusMinAvailH      = float32(120)
	keyboardFocusExtraLiftFrac  = float32(0.7)
	keyboardFocusExtraLiftMinDp = float32(120)
)

// syncKeyboardBottomInsetFromIME sets bottomInset to the overlap between the
// video container and the system IME so pan/zoom math uses the visible area
// above the keyboard (not the full touchpad, which still extends under the IME).
func (vw *VideoWidget) syncKeyboardBottomInsetFromIME(imeHeightDp float32) {
	const minRealIMEDp = 100
	if vw == nil || imeHeightDp < minRealIMEDp {
		vw.bottomInset = 0
		return
	}
	overlap := imeHeightDp
	if vw.parentWindow != nil && vw.container != nil {
		cs := vw.parentWindow.Canvas().Size()
		pos := vw.videoContainerOrigin()
		sz := vw.container.Size()
		imeTop := cs.Height - imeHeightDp
		videoBottom := pos.Y + sz.Height
		if videoBottom > imeTop {
			overlap = videoBottom - imeTop
		} else {
			overlap = 0
		}
	}
	if overlap < 0 {
		overlap = 0
	}
	// Extra clearance so the caret focus band sits clearly above the IME,
	// not flush against its top edge.
	inset := overlap + keyboardFocusClearanceDp
	maxInset := vw.touchpadSizeH - keyboardFocusMinAvailH
	if vw.touchpadSizeH > 0 && maxInset > 0 && inset > maxInset {
		inset = maxInset
	}
	vw.bottomInset = inset
}

// focusViewportOnVirtualCursorForKeyboard zooms 2× and pans so the virtual
// caret sits in the centre of the visible video area above the IME.
func (vw *VideoWidget) focusViewportOnVirtualCursorForKeyboard() {
	if vw == nil {
		return
	}
	vw.keyboardViewportLift = true
	if tw := vw.activeViewportWrapper(); tw != nil {
		if sz := tw.Size(); sz.Width > 0 && sz.Height > 0 {
			vw.touchpadSizeW = sz.Width
			vw.touchpadSizeH = sz.Height
		}
	}
	vw.viewportManualControl = false

	vw.vcMu.Lock()
	u, v := vw.virtualCursorU, vw.virtualCursorV
	vw.vcMu.Unlock()
	if u <= 0 && v <= 0 {
		u, v = 0.5, 0.5
	}
	u = clampFloat(u, 0, 1)
	v = clampFloat(v, 0, 1)

	availH := vw.touchpadSizeH - vw.bottomInset
	if availH < keyboardFocusMinAvailH {
		availH = vw.touchpadSizeH
		if availH > keyboardFocusMinAvailH*2 {
			vw.bottomInset = availH * 0.35
			availH = vw.touchpadSizeH - vw.bottomInset
		}
	}
	if vw.touchpadSizeW <= 0 || availH <= 0 {
		return
	}

	baseW := vw.baseContentRectW
	baseH := vw.baseContentRectH
	if baseW <= 0 || baseH <= 0 {
		baseW = vw.touchpadSizeW
		baseH = availH
	}

	vw.zoomScale = keyboardFocusZoom
	vw.zoomScaleResidual = 1

	cw := baseW * vw.zoomScale
	ch := baseH * vw.zoomScale
	centerY := (availH - ch) / 2
	idealPanX := cw * (0.5 - u)
	idealPanY := availH*(keyboardFocusYFrac-0.5) + ch*(0.5-v)

	extraUp := availH * keyboardFocusExtraLiftFrac
	if extraUp < keyboardFocusExtraLiftMinDp {
		extraUp = keyboardFocusExtraLiftMinDp
	}
	extraDown := extraUp
	if r := vw.specialKeysHeaderReserve; r > extraDown {
		extraDown = r
	}

	if cw > vw.touchpadSizeW {
		maxPanX := (cw - vw.touchpadSizeW) / 2
		vw.panOffsetX = clampFloat(idealPanX, -maxPanX-extraUp, maxPanX+extraDown)
	} else {
		vw.panOffsetX = idealPanX
	}
	if ch > availH {
		maxPanY := (ch - availH) / 2
		vw.panOffsetY = clampFloat(idealPanY, -maxPanY-extraUp, maxPanY+extraDown)
	} else {
		minY := -ch*0.7 - extraUp
		maxY := availH - ch*0.3 + extraDown
		contentY := clampFloat(centerY+idealPanY, minY, maxY)
		vw.panOffsetY = contentY - centerY
	}

	vw.recalculateViewport()
	vw.updateNativeViewportAndCursor()
	vw.forceCanvasRefresh.Store(true)
	logrus.Infof("⌨️ Keyboard caret focus: uv=(%.2f,%.2f) zoom=%.2f pan=(%.0f,%.0f) inset=%.0f focusY=%.2f",
		u, v, vw.zoomScale, vw.panOffsetX, vw.panOffsetY, vw.bottomInset, keyboardFocusYFrac)
}

func (vw *VideoWidget) scheduleKeyboardViewportSettle() {
	vw.applyImmediateKeyboardViewport()
}

func (vw *VideoWidget) freezeKeyboardLayout() {}

func (vw *VideoWidget) applyKeyboardViewportSettle() {
	vw.applyImmediateKeyboardViewport()
}

// scheduleKeyboardCaretFocus re-runs hard focus after layout/IME settle.
func (vw *VideoWidget) scheduleKeyboardCaretFocus() {
	vw.applyImmediateKeyboardViewport()
}
