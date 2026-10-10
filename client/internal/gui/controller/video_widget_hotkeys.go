package controller

import (
	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/input"
	"usbridge-client/internal/service"
)

// Moonlight's Ctrl+Alt+Shift hotkeys (the official client's set, minus
// the ones with no counterpart here), handled locally instead of being
// typed into the host. The letter is found by physical key position, so
// they work in any keyboard layout. Ctrl+Alt+Shift+F1..F12 are not
// intercepted: they reach the host, where Sunshine switches monitors.
//
//	Q  stop the stream
//	X  toggle fullscreen
//	S  toggle the Net Graph statistics overlay
//	M  toggle the mouse mode -- touchpad <-> absolute on mobile (Capture
//	   isn't offered there); absolute <-> capture on desktop (touchpad
//	   isn't offered there -- see mouseConfigOptions)
//	Z  release Capture mode to absolute specifically (Moonlight's own
//	   mouse-mode hotkey letter) -- a no-op in any other mode, unlike M
//	N  show/hide the host's mouse cursor (the "Show Mouse" setting)
//	V  type the clipboard's text into the host

const (
	hotkeyModsMask  = 1 | 2 | 4 // widget bitmask: Ctrl=1, Shift=2, Alt=4
	hotkeySuperMask = 8
)

// Virtual-key codes of the hotkey letters.
const (
	vkHotkeyQuit         int16 = 0x51 // Q
	vkHotkeyFullscreen   int16 = 0x58 // X
	vkHotkeyStats        int16 = 0x53 // S
	vkHotkeyMouseMode    int16 = 0x4D // M
	vkHotkeyMouseRelease int16 = 0x5A // Z
	vkHotkeyCursor       int16 = 0x4E // N
	vkHotkeyPaste        int16 = 0x56 // V
)

// HotkeyActions are the hotkeys that change main-window state.
type HotkeyActions struct {
	ToggleMouseMode func()
	// ReleaseMouseCapture exits Capture mode back to absolute. It's a no-op
	// when Capture isn't the current mode -- Ctrl+Alt+Shift+Z is a
	// release-only shortcut (Moonlight's own mouse-mode hotkey letter),
	// unlike M's full toggle.
	ReleaseMouseCapture func()
	// ToggleShowMouse flips the persisted Show Mouse setting and applies it
	// through SetShowMouseCursor.
	ToggleShowMouse func()
}

// SetHotkeyActions wires the hotkeys that need the main window.
func (vw *VideoWidget) SetHotkeyActions(a HotkeyActions) { vw.hotkeyActions = a }

func hotkeyVK(event *fyne.KeyEvent) int16 {
	if vk := input.GetVKCodeFromScanCode(event.Physical.ScanCode); vk != 0 {
		return vk
	}
	return input.GetVKCode(event.Name)
}

// handleHotkeyDown runs a hotkey and reports whether the keystroke was
// consumed (not sent to the host).
func (vw *VideoWidget) handleHotkeyDown(event *fyne.KeyEvent) bool {
	mods := vw.currentHIDModifiers()
	if mods&hotkeyModsMask != hotkeyModsMask || mods&hotkeySuperMask != 0 {
		return false
	}
	vk := hotkeyVK(event)
	switch vk {
	case vkHotkeyQuit:
		logrus.Info("⌨️ [hotkey] Ctrl+Alt+Shift+Q: stopping the stream")
		fyne.Do(func() { vw.ExitFullscreenIfNeeded() })
		vw.StopVideoAsync()
	case vkHotkeyFullscreen:
		fyne.Do(func() {
			if !vw.ExitFullscreenIfNeeded() {
				vw.ShowFullscreen()
			}
		})
	case vkHotkeyStats:
		service.SetNetGraphEnabled(!service.NetGraphEnabled())
	case vkHotkeyMouseMode:
		if fn := vw.hotkeyActions.ToggleMouseMode; fn != nil {
			fyne.Do(fn)
		}
	case vkHotkeyMouseRelease:
		if fn := vw.hotkeyActions.ReleaseMouseCapture; fn != nil {
			fyne.Do(fn)
		}
	case vkHotkeyCursor:
		// The host acts on this exact combination itself (Sunshine's
		// apply_shortcut, RustShine the same), so let N through and only
		// record the new state -- sending our own combo too would toggle
		// it back.
		vw.hostCursorShown = !vw.hostCursorShown
		if fn := vw.hotkeyActions.ToggleShowMouse; fn != nil {
			fyne.Do(fn)
		}
		return false
	case vkHotkeyPaste:
		vw.typeClipboardText()
	default:
		return false
	}
	if vw.hotkeysHeld == nil {
		vw.hotkeysHeld = map[int16]bool{}
	}
	vw.hotkeysHeld[vk] = true
	return true
}

// consumeHotkeyUp swallows the release of a key whose press was a hotkey.
func (vw *VideoWidget) consumeHotkeyUp(event *fyne.KeyEvent) bool {
	vk := hotkeyVK(event)
	if !vw.hotkeysHeld[vk] {
		return false
	}
	delete(vw.hotkeysHeld, vk)
	return true
}

// typeClipboardText types the clipboard's text into the host through the
// same per-character path as text-mode typing.
func (vw *VideoWidget) typeClipboardText() {
	if vw.parentWindow == nil {
		return
	}
	// Read without blocking Fyne's goroutine: the browser build can only
	// read the clipboard asynchronously (see view.ReadClipboard).
	view.ReadClipboard(vw.parentWindow, vw.typeText)
}

func (vw *VideoWidget) typeText(text string) {
	if text == "" {
		return
	}
	const maxPaste = 4096
	runes := []rune(text)
	if len(runes) > maxPaste {
		runes = runes[:maxPaste]
	}
	logrus.Infof("⌨️ [hotkey] Ctrl+Alt+Shift+V: typing %d characters from the clipboard", len(runes))
	for _, r := range runes {
		if r == '\r' {
			continue
		}
		vw.typeRune(r)
	}
}

// syncHostCursor makes the host's cursor match Show Mouse mid-session by
// sending the Ctrl+Alt+Shift+N shortcut when they differ. New sessions get
// it through /launch instead (see beginVideoTrace).
func (vw *VideoWidget) syncHostCursor() {
	if !vw.isStreaming || vw.hostCursorShown == vw.showMouseCursor {
		return
	}
	mi := vw.moonlightInput()
	if mi == nil {
		return
	}
	vw.hostCursorShown = vw.showMouseCursor
	// Sunshine only treats N as the shortcut when the modifier keys
	// themselves are down (its shortcutFlags), not just flagged.
	const (
		vkLControl int16 = 0xA2
		vkLMenu    int16 = 0xA4
		vkLShift   int16 = 0xA0
		modShift         = int8(0x01)
		modCtrl          = int8(0x02)
		modAlt           = int8(0x04)
	)
	vw.enqueueSend(func() {
		mi.SendMoonlightKey(vkLControl, service.LiKeyActionDown, modCtrl)
		mi.SendMoonlightKey(vkLMenu, service.LiKeyActionDown, modCtrl|modAlt)
		mi.SendMoonlightKey(vkLShift, service.LiKeyActionDown, modCtrl|modAlt|modShift)
		mi.SendMoonlightKey(vkHotkeyCursor, service.LiKeyActionDown, modCtrl|modAlt|modShift)
		mi.SendMoonlightKey(vkHotkeyCursor, service.LiKeyActionUp, modCtrl|modAlt|modShift)
		mi.SendMoonlightKey(vkLShift, service.LiKeyActionUp, modCtrl|modAlt)
		mi.SendMoonlightKey(vkLMenu, service.LiKeyActionUp, modCtrl)
		mi.SendMoonlightKey(vkLControl, service.LiKeyActionUp, 0)
	})
	logrus.Infof("🖱️ [cursor] host cursor -> %v", vw.showMouseCursor)
}
