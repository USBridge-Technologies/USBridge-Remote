//go:build js && wasm

// Temporary, opt-in (?debug=1 in the page URL) on-screen HUD showing the
// live viewport state during a pinch-zoom gesture -- added to track down a
// "video jumps to the bottom when zooming" report that survived two
// rounds of fixes based on static reading of the code alone. Shows
// directly on the phone's own screen, no devtools/remote-debugging setup
// needed on the user's end. Safe to leave in: completely inert (no DOM
// element created, no per-frame work) unless the query string opts in.
package controller

import (
	"fmt"
	"syscall/js"
)

var (
	debugOverlayEl      js.Value
	debugOverlayChecked bool
	debugOverlayOn      bool
)

func debugOverlayEnabled() bool {
	if debugOverlayChecked {
		return debugOverlayOn
	}
	debugOverlayChecked = true
	loc := js.Global().Get("location")
	if loc.IsUndefined() {
		return false
	}
	search := loc.Get("search").String()
	debugOverlayOn = len(search) > 0 && (contains(search, "debug=1") || contains(search, "debug=viewport"))
	return debugOverlayOn
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// debugLogViewport updates the on-screen HUD with the current viewport
// state. Called from applyViewportGesture (after a pinch update) and
// recalculateViewport (after everything else) so both the gesture math's
// inputs/outputs and the final clamped result are visible.
func (vw *VideoWidget) debugLogViewport(tag string) {
	if !debugOverlayEnabled() {
		return
	}
	if debugOverlayEl.IsUndefined() || debugOverlayEl.IsNull() {
		doc := js.Global().Get("document")
		el := doc.Call("createElement", "div")
		style := el.Get("style")
		style.Set("position", "fixed")
		style.Set("top", "0")
		style.Set("left", "0")
		style.Set("zIndex", "999999")
		style.Set("background", "rgba(0,0,0,0.85)")
		style.Set("color", "#0f0")
		style.Set("font", "10px monospace")
		style.Set("padding", "4px")
		style.Set("whiteSpace", "pre")
		style.Set("pointerEvents", "none")
		style.Set("maxWidth", "100vw")
		doc.Get("body").Call("appendChild", el)
		debugOverlayEl = el
	}
	availableH := vw.touchpadSizeH - vw.bottomInset
	text := fmt.Sprintf(
		"[%s]\ntouchpad=%.0fx%.0f bottomInset=%.0f availableH=%.0f\nbaseContent=%.0fx%.0f zoomScale=%.3f\npanOffset=(%.1f,%.1f)\ncontentRect=(%.1f,%.1f,%.1f,%.1f)\nbottomAnchor=%v",
		tag, vw.touchpadSizeW, vw.touchpadSizeH, vw.bottomInset, availableH,
		vw.baseContentRectW, vw.baseContentRectH, vw.zoomScale,
		vw.panOffsetX, vw.panOffsetY,
		vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH,
		vw.bottomAnchorContentVertically,
	)
	debugOverlayEl.Set("innerText", text)
}

// debugLogGesture prepends the raw gesture-callback inputs (before any
// processing) to whatever debugLogViewport last wrote, so a jump can be
// traced back to "the input itself was wrong" vs "the math misused a
// correct input".
func (vw *VideoWidget) debugLogGesture(scaleFactor, focusX, focusY, panDx, panDy float32) {
	if !debugOverlayEnabled() || debugOverlayEl.IsUndefined() || debugOverlayEl.IsNull() {
		// Ensure the element exists even if recalc hasn't run yet.
		vw.debugLogViewport("init")
	}
	if !debugOverlayEnabled() {
		return
	}
	prefix := fmt.Sprintf("[gesture in] scale=%.4f focus=(%.1f,%.1f) pan=(%.1f,%.1f)\n", scaleFactor, focusX, focusY, panDx, panDy)
	debugOverlayEl.Set("innerText", prefix+debugOverlayEl.Get("innerText").String())
}

// spinnerDebugEl is a second, independent HUD (top-right, separate from
// debugOverlayEl's top-left viewport HUD so the two don't clobber each
// other) showing the connecting-spinner's actual on-screen state -- added
// to track down a "no spinner visible at all on web" report after two
// rounds of fixes (isStreaming-gated DOM overlay, videoWidth gate) that
// should have made it visible but apparently didn't.
var spinnerDebugEl js.Value

// debugLogSpinner updates the spinner HUD. Called from
// showConnectingSpinner/hideConnectingSpinner (video_widget_spinner.go)
// and once per frame-cycle tick, so the *current* live state is always
// on screen, not just a snapshot from whenever Show()/Hide() was last
// called -- if the overlay's own Visible()/Size() disagrees with what
// the calls intended, that's directly visible here.
func (vw *VideoWidget) debugLogSpinner(tag string) {
	if !debugOverlayEnabled() {
		return
	}
	if spinnerDebugEl.IsUndefined() || spinnerDebugEl.IsNull() {
		doc := js.Global().Get("document")
		el := doc.Call("createElement", "div")
		style := el.Get("style")
		style.Set("position", "fixed")
		style.Set("top", "0")
		style.Set("right", "0")
		style.Set("zIndex", "999999")
		style.Set("background", "rgba(0,0,0,0.85)")
		style.Set("color", "#0ff")
		style.Set("font", "10px monospace")
		style.Set("padding", "4px")
		style.Set("whiteSpace", "pre")
		style.Set("pointerEvents", "none")
		style.Set("maxWidth", "50vw")
		doc.Get("body").Call("appendChild", el)
		spinnerDebugEl = el
	}

	overlayVisible := "no-ui"
	overlayPos := ""
	if vw.ui != nil && vw.ui.SpinnerOverlay != nil {
		overlayVisible = fmt.Sprintf("%v", vw.ui.SpinnerOverlay.Visible())
		pos := vw.ui.SpinnerOverlay.Position()
		sz := vw.ui.SpinnerOverlay.Size()
		overlayPos = fmt.Sprintf("pos=(%.0f,%.0f) size=%.0fx%.0f", pos.X, pos.Y, sz.Width, sz.Height)
	}
	wrapperInfo := "nil"
	if tw := vw.activeViewportWrapper(); tw != nil {
		sz := tw.Size()
		wrapperInfo = fmt.Sprintf("%.0fx%.0f visible=%v", sz.Width, sz.Height, tw.Visible())
	}
	containerInfo := "nil"
	if vw.container != nil {
		sz := vw.container.Size()
		containerInfo = fmt.Sprintf("%.0fx%.0f visible=%v", sz.Width, sz.Height, vw.container.Visible())
	}
	text := fmt.Sprintf(
		"[spinner %s]\noverlay visible=%s %s\nwrapper=%s\ncontainer=%s\nisStreaming=%v agentOS=%q",
		tag, overlayVisible, overlayPos, wrapperInfo, containerInfo, vw.isStreaming, vw.agentOS,
	)
	spinnerDebugEl.Set("innerText", text)
}
