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

func (fd *FullscreenDialog) platformExit() {
	logrus.Info("🔍 Desktop: closing the fullscreen window")
	if fd.fullscreenWindow != nil {
		fd.fullscreenWindow.Close()
		fd.fullscreenWindow = nil
	}
}
