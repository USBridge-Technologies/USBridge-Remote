//go:build js && wasm

package controller

import (
	"usbridge-client/internal/gui/graphics"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// platformHandleVirtualKeyboard shows/hides the on-screen keyboard inside
// vw.contentContainer, bottom-docked below the video -- the same layout
// mechanism video_widget_mobile.go's Android/iOS version uses (see its own
// doc comment), NOT desktop's ShowInSeparateWindow(), which spawns a real
// second fyne.Window. A browser tab can't host a second OS window at all
// (same limitation already worked around for the fullscreen dialog, see
// fullscreen_dialog_mobile.go's build tag) -- confirmed live that using
// desktop's separate-window path under wasm rendered the keyboard as a
// layer stacked at the top of the single canvas, directly over the video,
// instead of appearing docked at the bottom the way a real mobile
// on-screen keyboard does.
//
// Unlike the Android/iOS version, this does NOT call
// RegisterAsIMETarget() or wire SetOnIMEChanged -- there is no native OS
// IME to register with in a browser. Real typing/paste input already goes
// through a separate bridge (ime_bridge_wasm.go, listening on the
// #dummyEntry hidden <input> for the browser's own IME composition
// events) that has nothing to do with this on-screen keyboard's own
// visibility -- this widget is Fyne's own drawn keyboard layout, only
// useful for the explicit "show keyboard" button tap.
func (vw *VideoWidget) platformHandleVirtualKeyboard() {
	if vw.virtualKeyboard == nil {
		if vw.parentWindow == nil {
			logrus.Warn("⚠️ Parent window is not set")
			return
		}
		vw.virtualKeyboard = graphics.NewVirtualKeyboard(vw.parentWindow, vw.handleVirtualKeyPress, vw.handlePhysicalRunePress)
	}

	if vw.virtualKeyboard.IsVisible() {
		vw.virtualKeyboard.Hide()
		vw.contentContainer.Hide()
		vw.container.Refresh()
		vw.forceCanvasRefresh.Store(true)
		logrus.Info("⌨️ Virtual keyboard hidden (web mode)")
		return
	}

	keyboardLayout := vw.virtualKeyboard.GetKeyboardLayout()
	vw.virtualKeyboard.SetVisibleState(true)

	canvasSize := vw.parentWindow.Canvas().Size()
	keyboardLayout.Resize(fyne.NewSize(canvasSize.Width, keyboardLayout.MinSize().Height))
	// Position (0,0) is relative to contentContainer's own origin, which
	// video_widget_ui.go's layout already docks at the bottom of the
	// window (below the video area) -- same as the Android/iOS version,
	// see that file's identical Move call.
	keyboardLayout.Move(fyne.NewPos(0, 0))

	vw.contentContainer.Objects = []fyne.CanvasObject{keyboardLayout}
	vw.contentContainer.Resize(keyboardLayout.Size())
	vw.contentContainer.Show()
	vw.container.Refresh()
	vw.forceCanvasRefresh.Store(true)
	logrus.Info("⌨️ Virtual keyboard shown (web mode)")
}

func (vw *VideoWidget) platformShowVirtualKeyboardIfMobile() {
	if vw.virtualKeyboard == nil || !vw.virtualKeyboard.IsVisible() {
		vw.platformHandleVirtualKeyboard()
	}
}
