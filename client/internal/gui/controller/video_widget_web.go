//go:build js && wasm

package controller

import (
	"usbridge-client/internal/gui/graphics"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// webKeyboardOriginalContent/webKeyboardContentSwapped save what the window
// was showing before the keyboard panel took it over (see
// platformHandleVirtualKeyboard) so it can be restored exactly when the
// panel closes. Package-level, not per-VideoWidget: same one-window
// assumption already made by webTouch/touchOverlayEl/cursorDotEl.
var (
	webKeyboardOriginalContent fyne.CanvasObject
	webKeyboardContentSwapped  bool
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
//
// Also swaps the *window's* content to vw.container itself while the
// panel is open: normally vw.container only occupies the Control tab's
// slot inside AppTabs, below the tab strip and the app's own address-bar
// widget (main_window_layout.go's mainContent = Stack(bg,
// Border(mainAddressBar, Stack(tabsWithTheme, ...)))) -- there is no
// native overlay under wasm (unlike Android's Vulkan SurfaceView, which
// is a separate Activity-level View that can paint over the tab strip
// with zero Fyne layout involvement, see video_widget_android.go's own
// videoCanvasFrame doc comment) to make the video visually extend over
// that chrome the way Android does. Making vw.container -- the same
// Border(video, keyboard-at-bottom) already used for the normal in-tab
// layout -- the window's *entire* content for as long as the panel is
// open removes the tab strip and address-bar widget from the tree
// entirely, so Fyne's own Border layout hands 100% of whatever height the
// browser gives the window above the keyboard panel to the video, with
// no coordinate math needed on our side. Restored to the original content
// verbatim when the panel closes.
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
		if webKeyboardContentSwapped && webKeyboardOriginalContent != nil && vw.parentWindow != nil {
			vw.parentWindow.SetContent(webKeyboardOriginalContent)
			webKeyboardContentSwapped = false
			webKeyboardOriginalContent = nil
		}
		vw.container.Refresh()
		vw.forceCanvasRefresh.Store(true)
		logrus.Info("⌨️ Virtual keyboard hidden (web mode)")
		return
	}

	if vw.parentWindow != nil && !webKeyboardContentSwapped {
		webKeyboardOriginalContent = vw.parentWindow.Content()
		vw.parentWindow.SetContent(vw.container)
		webKeyboardContentSwapped = true
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

// syncKeyboardWindowContent is a self-healing invariant check, called every
// tick from video_gestures_wasm.go's existing 150ms overlay-sync loop
// (alongside syncTouchOverlay/syncCursorDot): "panel visible => window
// shows vw.container", "panel hidden => window shows whatever it showed
// before". platformHandleVirtualKeyboard already applies this immediately
// on the explicit toggle tap; this is the fallback for every other way the
// invariant could go stale -- e.g. anything elsewhere in the app calling
// window.SetContent() while the panel happens to be open (main_window_
// lifecycle.go's showMainContent/showConnectionManager are the only other
// callers today, but this makes the panel's own layout correct regardless
// of whether some future caller does too), or the keyboard's visibility
// changing through a path other than platformHandleVirtualKeyboard.
// Comparing against the exact vw.container/webKeyboardOriginalContent
// reference (not just the boolean) is what makes this self-healing rather
// than just re-trusting the same bookkeeping that could itself have gone
// stale.
func syncKeyboardWindowContent(vw *VideoWidget) {
	if vw == nil || vw.parentWindow == nil || vw.virtualKeyboard == nil || vw.container == nil {
		return
	}
	open := vw.virtualKeyboard.IsVisible()
	current := vw.parentWindow.Content()

	if open {
		if current != vw.container {
			if !webKeyboardContentSwapped || webKeyboardOriginalContent == nil {
				webKeyboardOriginalContent = current
			}
			vw.parentWindow.SetContent(vw.container)
			webKeyboardContentSwapped = true
			vw.RefreshViewportGeometry()
		}
		return
	}

	if webKeyboardContentSwapped && webKeyboardOriginalContent != nil && current == vw.container {
		vw.parentWindow.SetContent(webKeyboardOriginalContent)
		webKeyboardContentSwapped = false
		webKeyboardOriginalContent = nil
	}
}
