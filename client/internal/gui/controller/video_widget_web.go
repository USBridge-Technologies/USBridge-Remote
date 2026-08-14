//go:build js && wasm

package controller

import (
	"usbridge-client/internal/gui/graphics"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// webKeyboardOriginalContent/webKeyboardContentSwapped save what the window
// was showing before the real IME took it over (see
// syncKeyboardWindowContent) so it can be restored exactly when the IME
// closes. Package-level, not per-VideoWidget: same one-window assumption
// already made by webTouch/touchOverlayEl/cursorDotEl.
var (
	webKeyboardOriginalContent fyne.CanvasObject
	webKeyboardContentSwapped  bool
)

// webRestCanvasHeight tracks the largest canvas height observed while the
// real IME was NOT considered open -- i.e. the window's normal, at-rest
// height (itself already live-tracking the browser's own address-bar
// show/hide via web/index.html's 100dvh canvas sizing, so this baseline
// moves with that on its own). Comparing the *current* canvas height
// against this running max is how syncKeyboardWindowContent tells "the
// real browser IME is currently extended" apart from "the address bar
// just changed" -- see that function's own doc comment for the threshold.
var webRestCanvasHeight float32

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
// This function only manages the *panel's* own visibility. The full-
// window takeover (giving video the tab strip/address-bar's own space
// too, matching native Android's Vulkan SurfaceView overlay) is handled
// separately by syncKeyboardWindowContent, gated on the real IME being
// extended -- not on this panel's own visibility, since opening the panel
// via its toggle button doesn't by itself open the real IME (only tapping
// an actual text field does), and the two can close in either order.
func (vw *VideoWidget) platformHandleVirtualKeyboard() {
	// Desktop browser: no on-screen panel at all, matching the native
	// desktop build's own UX (video_widget_desktop.go) instead of the
	// mobile-style docked panel below. Real typing already reaches the app
	// without it -- Fyne's wasm driver auto-focuses the hidden #dummyEntry
	// input whenever the canvas itself gets focus, independent of this
	// panel's visibility (see fyne's internal/driver/glfw/device_wasm.go
	// connectKeyboard/handleKeyboard) -- so a physical keyboard just works
	// exactly like it does everywhere else. Desktop's own
	// platformHandleVirtualKeyboard opens a *separate OS window*
	// (ShowInSeparateWindow) for its equivalent on-screen keyboard, which a
	// single browser tab can't do (see this file's top doc comment) -- the
	// honest equivalent here isn't approximating that with the mobile
	// panel, it's simply not needing one.
	//
	// fyne.CurrentDevice().IsMobile() under wasm is real User-Agent
	// sniffing (Android|iPhone|iPad|iPod), not a build-time guess -- see
	// that function in fyne's glfw/device_wasm.go -- so this correctly
	// tells a phone's browser apart from a laptop's even though both run
	// the exact same wasm binary.
	if !fyne.CurrentDevice().IsMobile() {
		logrus.Debug("⌨️ Virtual keyboard toggle ignored on desktop browser -- physical keyboard already works")
		return
	}
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

// realIMEOpenThresholdDp mirrors Android's own onIMEHeightChanged
// threshold (minRealIMEDp = 100): a shrink smaller than this is normal
// address-bar/rounding noise, not a real keyboard.
const realIMEOpenThresholdDp = 100

// isRealIMEOpen reports whether the browser's actual on-screen keyboard is
// currently extended, purely from the Go side: web/index.html shrinks the
// canvas element's own CSS height to window.visualViewport.height whenever
// it decides the real IME is covering the page, and restores it to 100dvh
// (which already tracks the address bar's own show/hide) when it isn't --
// so a canvas height that has dropped well below its own recent at-rest
// maximum (webRestCanvasHeight) means the real IME is open, and anything
// smaller than that means the address bar is just doing its normal thing.
// No JS->Go bridge call needed: the canvas size Fyne already observes on
// every resize is signal enough.
func isRealIMEOpen(vw *VideoWidget) bool {
	if vw.parentWindow == nil {
		return false
	}
	h := vw.parentWindow.Canvas().Size().Height
	if h <= 0 {
		return false
	}
	if webRestCanvasHeight <= 0 || h > webRestCanvasHeight {
		webRestCanvasHeight = h
	}
	return webRestCanvasHeight-h > realIMEOpenThresholdDp
}

// syncKeyboardWindowContent is the self-healing invariant that gives video
// the whole window (tab strip, app's own address-bar widget, and all)
// while -- and only while -- the real browser IME is extended, matching
// what native Android does via its own Vulkan SurfaceView overlay (see
// video_widget_android.go's videoCanvasFrame doc comment for how that
// overlay can paint over the tab strip with zero Fyne layout involvement;
// wasm has no such overlay, so the only way to get video the same space
// is to remove everything else from the window's content tree while the
// IME needs that space). Runs every tick from video_gestures_wasm.go's
// existing 150ms overlay-sync loop (alongside syncTouchOverlay/
// syncCursorDot) rather than once at some single trigger point, so it
// self-heals regardless of what caused window content to drift from the
// correct state -- comparing the window's *actual current* content
// against vw.container/webKeyboardOriginalContent by reference is what
// makes this self-healing rather than trusting one-shot bookkeeping that
// could go stale.
//
// An earlier version of this was shipped, then reverted after appearing
// to badly regress the layout (video pinned at a tiny stale size with a
// large dead area below it after the IME closed). Root-caused since:
// that breakage was actually web/index.html's canvas.style.height getting
// deleted entirely on IME close (removeProperty('height') has no
// "previous value" to fall back to -- see that file's own fix), which
// collapsed the *whole canvas*, not something specific to this swap. With
// that CSS bug fixed, this is safe to re-enable.
func syncKeyboardWindowContent(vw *VideoWidget) {
	if vw == nil || vw.parentWindow == nil || vw.container == nil {
		return
	}
	imeOpen := isRealIMEOpen(vw)
	current := vw.parentWindow.Content()
	// See bottomAnchorContentVertically's own doc comment (video_widget.go)
	// -- only actually relevant while the IME is genuinely open or the
	// window content is still in the post-swap arrangement (between IME
	// open and the swap-back below finishing); previously this was set
	// unconditionally on every 150ms tick regardless of IME state, which
	// meant it stayed true for the entire session even with no keyboard
	// ever involved. Since a pinch-zoomed video's own base size rarely
	// grows tall enough to hit recalculateViewport's *other* (overflow)
	// anchoring, that left this the only anchoring rule actually in play
	// during a pinch -- confirmed live as the real cause of a "video jumps
	// to the bottom when zooming" report that had nothing to do with the
	// keyboard at all.
	vw.bottomAnchorContentVertically = imeOpen || webKeyboardContentSwapped

	if imeOpen {
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
		vw.RefreshViewportGeometry()
	}
}
