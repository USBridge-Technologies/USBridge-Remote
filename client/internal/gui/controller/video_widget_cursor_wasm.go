//go:build js && wasm

// The wasm counterpart of video_widget_android.go's virtual-cursor-follow
// mechanism ("cursor" mouse mode: a local dot the viewport pans to keep
// centred, RustDesk-style, since a touchscreen has no real pointer to move
// on its own). Android renders that dot as part of its native Vulkan
// composite (service.VKVideoAndroidSetViewportAndCursor); there is no
// equivalent native surface to draw into under wasm, so this file draws it
// as a small CSS overlay div instead, kept in sync with
// video_gestures_wasm.go's own overlay-position poll loop (see that file's
// syncTouchOverlay/InitTouchGestureBridge). The panning math itself
// (centerViewportOnVirtualCursor/softClampEdgePan) is pure Go with no
// platform dependency, so it's reused verbatim from video_widget_android.go.
package controller

import (
	"strconv"
	"syscall/js"

	"fyne.io/fyne/v2"
)

// cursorDotEl is the lazily-created cursor overlay div.
var cursorDotEl js.Value

// cursorPointerSVGDataURI embeds the exact same arrow SVG the native
// Android build rasterizes into its Vulkan cursor buffer
// (assets.CursorPointerSVG / cursor-pointer.svg), so the web client's
// cursor matches the native one pixel-for-pixel instead of being an
// arbitrary placeholder dot. viewBox is 18x24; the tip of the arrow sits
// at the SVG's origin corner (~1,1 of the viewBox), which is why the
// overlay div below is positioned with no centering transform -- its
// top-left corner *is* the cursor's hotspot, same as a real OS pointer.
const cursorPointerSVGDataURI = "data:image/svg+xml;utf8," +
	"%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 18 24'%3E" +
	"%3Cpath d='M 1 1 L 1 18 L 5 14 L 8 22 L 11 21 L 8 13 L 14 13 Z' " +
	"fill='%2393c572' stroke='%23000000' stroke-width='1.5' " +
	"stroke-linejoin='round' stroke-linecap='round'/%3E%3C/svg%3E"

// 0.8x scale (down from an earlier 2x that looked oversized on a real
// phone) -- 2.5x smaller than that first attempt.
const cursorDotWidthPx = 14
const cursorDotHeightPx = 19

// updateNativeViewportAndCursor re-centers the viewport on the virtual
// cursor (when in cursor/gyro mode) and refreshes the on-screen dot -- the
// wasm equivalent of the Android/iOS versions of this same method.
func (vw *VideoWidget) updateNativeViewportAndCursor() {
	if isVirtualCursorLikeMode(vw.GetMouseInputMode()) {
		vw.vcMu.Lock()
		targetU := vw.virtualCursorU
		targetV := vw.virtualCursorV
		vw.vcMu.Unlock()
		vw.centerViewportOnVirtualCursor(targetU, targetV)
	}
	syncCursorDot(vw)
	syncVideoOverlay(vw)
}

// centerViewportOnVirtualCursor pans the viewport so the virtual cursor
// stays centred on screen (RustDesk-style follow). Copied from
// video_widget_android.go's implementation -- pure viewport math, no
// Vulkan/native calls, so it needs no changes to work here. Only
// effective when zoom > 1.
func (vw *VideoWidget) centerViewportOnVirtualCursor(u, v float32) {
	if vw.zoomScale <= 1.001 {
		return
	}
	cw := vw.baseContentRectW * vw.zoomScale
	ch := vw.baseContentRectH * vw.zoomScale

	var idealPanX, maxPanX float32
	if cw > vw.touchpadSizeW {
		idealPanX = cw * (0.5 - u)
		maxPanX = (cw - vw.touchpadSizeW) / 2
		zoneX := vw.touchpadSizeW * 0.15
		vw.panOffsetX = softClampEdgePan(idealPanX, -maxPanX, maxPanX, zoneX)
	} else {
		vw.panOffsetX = 0
	}

	availH := vw.touchpadSizeH - vw.bottomInset
	if ch > availH {
		idealPanY := availH/2 - (availH - ch) - v*ch
		maxPanY := ch - availH
		zoneY := availH * 0.15
		vw.panOffsetY = softClampEdgePan(idealPanY, 0, maxPanY, zoneY)
	} else {
		vw.panOffsetY = 0
	}

	vw.recalculateViewport()
}

// androidCursorScale/initAndroidCursorScale exist purely so wasm satisfies
// the same method set video_mouse_handler.go's cross-platform code
// expects; both are genuinely no-ops here since the cursor is a plain CSS
// element, not a rasterized pixel buffer uploaded to a native renderer.
func (vw *VideoWidget) androidCursorScale() int      { return 1 }
func (vw *VideoWidget) initAndroidCursorScale(_ int) {}
func (vw *VideoWidget) onIMEHeightChanged(_ float32) {}
func triggerRmbHaptic()                              {}

// ensureCursorDot lazily creates the dot element the first time it's needed.
func ensureCursorDot() {
	if !cursorDotEl.IsUndefined() && !cursorDotEl.IsNull() {
		return
	}
	doc := js.Global().Get("document")
	body := doc.Get("body")
	if body.IsNull() || body.IsUndefined() {
		return
	}
	el := doc.Call("createElement", "div")
	el.Set("id", "usbridge-virtual-cursor")
	style := el.Get("style")
	style.Set("position", "fixed")
	// left/top stay at 0 permanently; all movement happens via `transform`
	// below. Unlike left/top (which trigger layout on every update),
	// transform is compositor-only -- the browser can animate it smoothly
	// on the GPU, same as Android's Vulkan renderer moving the cursor
	// quad every frame. Combined with the short transition below, this is
	// what actually fixes the jerkiness: syncCursorDot only runs on
	// discrete events (touchmove callbacks, a 150ms poll tick), but the
	// transition interpolates the visual position smoothly between them
	// instead of snapping.
	style.Set("left", "0px")
	style.Set("top", "0px")
	style.Set("width", strconv.Itoa(cursorDotWidthPx)+"px")
	style.Set("height", strconv.Itoa(cursorDotHeightPx)+"px")
	style.Set("backgroundImage", "url(\""+cursorPointerSVGDataURI+"\")")
	style.Set("backgroundSize", "contain")
	style.Set("backgroundRepeat", "no-repeat")
	style.Set("filter", "drop-shadow(0 0 2px rgba(0,0,0,0.7))")
	style.Set("pointerEvents", "none")
	style.Set("zIndex", "11")
	style.Set("display", "none")
	style.Set("willChange", "transform")
	style.Set("transition", "transform 40ms linear")
	body.Call("appendChild", el)
	cursorDotEl = el
}

// syncCursorDot repositions the dot over the current virtual-cursor
// position, or hides it entirely when not in cursor/gyro mode (or not
// streaming). Called from updateNativeViewportAndCursor (every gesture
// update) and from video_gestures_wasm.go's periodic overlay-sync tick, so
// it stays correct both while dragging and at rest.
func syncCursorDot(vw *VideoWidget) {
	ensureCursorDot()
	if cursorDotEl.IsUndefined() || cursorDotEl.IsNull() {
		return
	}
	style := cursorDotEl.Get("style")

	if vw == nil || !vw.IsStreaming() || !isVirtualCursorLikeMode(vw.GetMouseInputMode()) {
		style.Set("display", "none")
		return
	}
	// Same "actually on the Control tab / no dialog up" guard
	// syncTouchOverlay applies (video_gestures_wasm.go) -- this dot itself
	// is pointer-events:none so it was never the thing eating taps, but it
	// has no business being visibly painted over another tab's UI either.
	if videoOverlayForceHidden {
		style.Set("display", "none")
		return
	}
	wrapper := vw.activeViewportWrapper()
	if wrapper == nil || !wrapper.Visible() {
		style.Set("display", "none")
		return
	}
	size := wrapper.Size()
	if size.Width <= 0 || size.Height <= 0 {
		style.Set("display", "none")
		return
	}

	vw.vcMu.Lock()
	u, v := vw.virtualCursorU, vw.virtualCursorV
	vw.vcMu.Unlock()

	localX := vw.contentRectX + u*vw.contentRectW
	localY := vw.contentRectY + v*vw.contentRectH

	abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(wrapper)
	style.Set("transform", "translate("+pxf(abs.X+localX)+","+pxf(abs.Y+localY)+")")
	style.Set("display", "block")
}
