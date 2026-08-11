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

// cursorDotEl is the lazily-created green dot overlay div.
var cursorDotEl js.Value

const cursorDotSizePx = 16

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
	style.Set("width", strconv.Itoa(cursorDotSizePx)+"px")
	style.Set("height", strconv.Itoa(cursorDotSizePx)+"px")
	style.Set("borderRadius", "50%")
	style.Set("background", "#39ff6a")
	style.Set("border", "2px solid rgba(0,0,0,0.55)")
	style.Set("boxShadow", "0 0 4px rgba(0,0,0,0.6)")
	style.Set("pointerEvents", "none")
	style.Set("zIndex", "11")
	style.Set("display", "none")
	style.Set("transform", "translate(-50%, -50%)")
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
	style.Set("left", pxf(abs.X+localX))
	style.Set("top", pxf(abs.Y+localY))
	style.Set("display", "block")
}
