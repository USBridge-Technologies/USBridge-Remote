//go:build android || ios

package controller

import (
	"usbridge-client/internal/gui/graphics"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (fd *FullscreenDialog) platformInitWindow() {
	logrus.Info("🔍 Mobile: using the main window for fullscreen mode")
	fd.fullscreenWindow = fd.parent
	fd.originalContent = fd.parent.Content().(*fyne.Container)
	fd.originalTitle = fd.parent.Title()
	fd.fullscreenWindow.SetTitle("")
	// SetFullScreen is called in platformShow() after SetContent(), so that
	// Android doesn't flash the old content (tabs, panel) before transitioning to Vulkan.
}

func (fd *FullscreenDialog) platformSetupUI() {
	if fd.virtualKeyboard == nil {
		return
	}

	// Register as the recipient of native Android IME events (KeyboardBridge → JNI → Go)
	fd.virtualKeyboard.RegisterAsIMETarget()

	// When the Android IME opens/closes, update the fullscreen window layout.
	fd.virtualKeyboard.SetOnIMEChanged(func(imeHeightDp float32) {
		fyne.Do(func() {
			if fd.ui != nil {
				// Compute the total inset: virtual keyboard buttons + system IME/NavBar
				var inset float32
				if fd.ui.KeyboardLayout.Visible() {
					// If the virtual keyboard is shown, its height already includes the system imeSpacer
					inset = fd.ui.KeyboardLayout.MinSize().Height
				} else {
					// If hidden — keep only the system inset (e.g. NavBar)
					inset = graphics.GetLastIMEH()
				}

				logrus.Infof("⌨️ [IME] Change detected. Setting bottom inset to %.1f (imeH=%.1f)", inset, imeHeightDp)
				if fd.videoWidget != nil {
					fd.videoWidget.SetBottomInset(inset)
				}

				// Force-refresh the whole tree
				size := fd.fullscreenWindow.Canvas().Size()
				fd.ui.VideoWithKeyboard.Resize(size)
				fd.ui.VideoWithKeyboard.Refresh()
				fd.virtualKeyboard.UpdatePosition(size)
			}
		})
	})
}

func (fd *FullscreenDialog) platformShow() {
	// SetFullScreen here, not in platformInitWindow — the content has already been swapped,
	// so Android immediately draws the new (transparent) content without flashing the old UI.
	fd.fullscreenWindow.SetFullScreen(true)
	fd.fullscreenWindow.Show()
	// Unfocus any widgets (e.g. TouchpadWrapper) that might trigger the IME keyboard automatically.
	if canvas := fd.fullscreenWindow.Canvas(); canvas != nil {
		canvas.Unfocus()
	}
}

func (fd *FullscreenDialog) platformExit() {
	logrus.Info("🔍 Mobile: restoring the main window's original content")
	fd.parent.SetFullScreen(false)
	if fd.originalContent != nil {
		fd.parent.SetContent(fd.originalContent)
		fd.parent.SetTitle(fd.originalTitle)
		logrus.Info("✅ Original content restored")
	}
}
