package controller

import (
	"usbridge-client/internal/service"

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
	if isFullscreenExitKey(event.Name, fd.keyboardModifierState.Load()) {
		logrus.Infof("⌨️ [FS][INPUT][DOWN] %q: leaving fullscreen", event.Name)
		noteFullscreenExitKey()
		fd.exitFullscreen()
		return
	}
	service.HideVideoHint() // any key: the hint has been seen
	if fd.videoWidget != nil {
		fd.videoWidget.handlePhysicalKeyDown(event)
	}
}

// fullscreenExitLearnedPrefKey: set once the user left fullscreen with
// F11 / Ctrl+Esc; the "how to leave" hint is not shown after that.
const fullscreenExitLearnedPrefKey = "fullscreen_exit_hint_learned"

func fullscreenExitLearned() bool {
	app := fyne.CurrentApp()
	return app != nil && app.Preferences().BoolWithFallback(fullscreenExitLearnedPrefKey, false)
}

// noteFullscreenExitKey: the exit key was used -- stop showing the hint.
func noteFullscreenExitKey() {
	service.HideVideoHint()
	if app := fyne.CurrentApp(); app != nil && !fullscreenExitLearned() {
		app.Preferences().SetBool(fullscreenExitLearnedPrefKey, true)
	}
}

// isFullscreenExitKey: F11, or Ctrl+Esc, leaves fullscreen. Esc alone goes
// to the host like any key (it closes dialogs, leaves the host's own
// fullscreen apps...). mods is the widget modifier mask (Ctrl=1).
func isFullscreenExitKey(name fyne.KeyName, mods int32) bool {
	return name == fyne.KeyF11 || (name == fyne.KeyEscape && mods&1 != 0)
}

// isFullscreenExitVK is isFullscreenExitKey for Win32 virtual keys.
func isFullscreenExitVK(vk int, mods int32) bool {
	return vk == 0x7A /* VK_F11 */ || (vk == 0x1B /* VK_ESCAPE */ && mods&1 != 0)
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

// handleKeyPress handles TypedKey in fullscreen mode.
// We always suppress the USB path; keys go through handleKeyDown/Up → Moonlight.
// TypedKey is used ONLY to exit fullscreen mode and as a fallback
// (via canvas.SetOnTypedKey) when touchpadWrapper hasn't received focus yet.
func (fd *FullscreenDialog) handleKeyPress(event *fyne.KeyEvent) {
	if event == nil {
		return
	}
	if isFullscreenExitKey(event.Name, fd.keyboardModifierState.Load()) || string(event.Name) == "Back" {
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

// handleRunePress forwards the OS/layout-resolved character to the server as
// Unicode text, bypassing raw-keycode/layout mapping entirely. See
// VideoWidget.handlePhysicalRunePress's own doc comment for why.
func (fd *FullscreenDialog) handleRunePress(r rune) {
	if fd.videoWidget != nil {
		fd.videoWidget.handlePhysicalRunePress(r)
	}
}

// handleVirtualKeyPress handles virtual keyboard key presses via Moonlight.
func (fd *FullscreenDialog) handleVirtualKeyPress(keyCode int, modifiers int) {
	logrus.Infof("⌨️ [FS][VIRTUAL] keyCode=%d modifiers=%d", keyCode, modifiers)
	mi := fd.videoWidget.moonlightInput()
	if mi == nil {
		logrus.Warn("⌨️ [FS][VIRTUAL] Moonlight not active, ignoring virtual key")
		return
	}
	moonlightMods := widgetToMoonlightModifiers(modifiers)
	mi.SendMoonlightKey(int16(keyCode), service.LiKeyActionDown, moonlightMods)
	mi.SendMoonlightKey(int16(keyCode), service.LiKeyActionUp, moonlightMods)
}
