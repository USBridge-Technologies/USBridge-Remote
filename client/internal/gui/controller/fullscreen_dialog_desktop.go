//go:build !android && !ios && !(js && wasm)

package controller

import (
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (fd *FullscreenDialog) platformInitWindow() {
	logrus.Info("🔍 Desktop: creating a new window for fullscreen mode")
	fd.fullscreenWindow = fyne.CurrentApp().NewWindow("")
	// Without SetPadded(false), Fyne insets the fullscreen content (and
	// TouchpadWrapper) by theme.Padding() on all four sides while the native
	// video overlay fills (0,0,W,H), shifting absolute cursor coordinates up
	// and left across the entire screen.
	fd.fullscreenWindow.SetPadded(false)
	fd.fullscreenWindow.SetTitle(i18n.Current.FullscreenWindowTitle)
	fd.fullscreenWindow.SetFullScreen(true)

	fd.fullscreenWindow.SetCloseIntercept(func() {
		logrus.Info("🔍 Intercepted a window close attempt - exiting fullscreen mode")
		fd.exitFullscreen()
	})
	fd.fullscreenWindow.SetOnClosed(func() {
		logrus.Info("🔍 Fullscreen window closed")
	})
}

func (fd *FullscreenDialog) platformSetupUI() {
	// Desktop usually doesn't need special IME handling
}

func (fd *FullscreenDialog) platformShow() {
	fd.fullscreenWindow.Show()
	fd.fullscreenWindow.RequestFocus()

	// Focus the touchpad to capture key events
	if fd.touchpadWrapper != nil {
		fd.fullscreenWindow.Canvas().Focus(fd.touchpadWrapper)
	}
}

// platformExit closes the fullscreen window -- except in the one state
// where doing so would be fatal. Fyne's glfw driver (internal/driver/glfw/
// loop.go's destroyWindow) calls Quit() UNCONDITIONALLY whenever its
// window count reaches zero, regardless of SetMaster/SetCloseIntercept.
// If fd.fullscreenWindow is, at this exact moment, the only window the
// driver still has in its list (the main window should always also be
// there -- see gui.newKeepAliveWindow for the real fix -- but window-list
// bookkeeping bugs are exactly the kind of thing that only shows up once
// in production), Close() silently kills the entire process: no
// SetCloseIntercept consulted, no stream/USB disconnect, no crash report
// (it's a clean exit(0), not a signal). Reproduced in production on
// 2026-10-04: app.log shows this exact fullscreen-exit sequence running
// normally and then just stopping mid-frame; the unified log's
// runningboardd entry for the process read "termination reported by
// launchd (0, 0, 512)" -- exit status 0, no signal, nothing for
// ReportCrash to catch.
func (fd *FullscreenDialog) platformExit() {
	logrus.Info("🔍 Desktop: closing the fullscreen window")
	if fd.fullscreenWindow == nil {
		return
	}
	win := fd.fullscreenWindow
	fd.fullscreenWindow = nil

	if count := len(fyne.CurrentApp().Driver().AllWindows()); count <= 1 {
		logrus.Errorf("⚠️ [Fullscreen] refusing to Close() what looks like the only window Fyne is tracking (count=%d) -- hiding instead so the app doesn't silently quit", count)
		win.Hide()
		return
	}
	win.Close()
}
