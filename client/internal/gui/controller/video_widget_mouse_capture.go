package controller

import "github.com/sirupsen/logrus"

// startMouseCapture installs the platform raw-capture engine (see mouse_
// capture.go). Called from SetMouseInputMode whenever the mode transitions
// into mouseModeCapture. Safe to call repeatedly -- a no-op once already
// running.
func (vw *VideoWidget) startMouseCapture() {
	if vw.captureEngine != nil {
		return
	}
	if vw.parentWindow == nil {
		logrus.Warn("⚠️ [Capture] no parent window yet — cannot start mouse capture")
		return
	}
	engine := newMouseCaptureEngine(vw.GetMoonlightInput)
	if err := engine.Start(vw.parentWindow); err != nil {
		logrus.Warnf("⚠️ [Capture] failed to start native mouse capture: %v — falling back to touchpad mode", err)
		vw.mouseInputMode = mouseModeTouchPad
		return
	}
	vw.captureEngine = engine
	// The native engine's mouse monitor swallows every click before Fyne
	// ever sees it, so if the touchpad widget doesn't already hold canvas
	// focus at this point (e.g. Capture was picked from the footer mouse
	// menu instead of by clicking the video), there is no longer any way
	// for the user to focus it themselves -- and without that focus,
	// desktop.Keyable's KeyDown never fires, so Ctrl+Alt+Shift+M/Z (and
	// every other hotkey) would silently do nothing for the rest of the
	// session. Grab focus explicitly every time Capture starts instead of
	// assuming it's already there.
	if vw.touchpadWrapper != nil {
		vw.touchpadWrapper.requestFocus()
	}
	logrus.Info("✅ [Capture] native mouse capture engine started")
}

// stopMouseCapture tears down the platform raw-capture engine and restores
// the system cursor. Called from SetMouseInputMode whenever the mode
// transitions away from mouseModeCapture.
func (vw *VideoWidget) stopMouseCapture() {
	if vw.captureEngine == nil {
		return
	}
	if err := vw.captureEngine.Stop(); err != nil {
		logrus.Warnf("⚠️ [Capture] error stopping native mouse capture: %v", err)
	}
	vw.captureEngine = nil
	restoreOverlayMouseInput()
	logrus.Info("✅ [Capture] native mouse capture engine stopped")
}
