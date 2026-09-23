//go:build js && wasm

package controller

import (
	"syscall/js"

	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
)

// aiVisionCaptureMaxSide caps the longest side of the frame captured for
// in-browser detection -- matches icon_detect's own 640x640 letterboxed
// input (internal/localui/parser_wasm.go's runIconStage), so capturing at,
// say, full 1920x1080 and immediately downscaling in Go would just waste a
// much bigger getImageData()/CopyBytesToGo round trip for no detection-
// quality benefit.
const aiVisionCaptureMaxSide = 960

// aiVisionCaptureCanvas is a lazily-created, reused-forever offscreen
// <canvas> for drawImage+getImageData captures -- never added to the DOM,
// never displayed; #aiVisionCanvas (the one actually on screen) only ever
// receives already-drawn detection boxes via
// service.pushAIVisionOverlayToCanvas.
var aiVisionCaptureCanvas js.Value

func aiVisionOffscreenCanvas() js.Value {
	if aiVisionCaptureCanvas.Truthy() {
		return aiVisionCaptureCanvas
	}
	aiVisionCaptureCanvas = js.Global().Get("document").Call("createElement", "canvas")
	return aiVisionCaptureCanvas
}

// syncAIVisionOverlay keeps #aiVisionCanvas positioned exactly over the
// video content rect -- the same CSS box syncVideoOverlay computes for the
// real <video> element (video_widget_dom_overlay_wasm.go), since this
// canvas sits directly on top of it -- and, while the AI Vision checkbox is
// on, periodically captures a downscaled frame into
// service.ApplyAIVisionOverlay, the same entry point every native
// platform's cgo decode callback drives per frame (see that function's own
// doc comment). Called from the same 150ms poll loop as syncVideoOverlay/
// syncTouchOverlay/syncCursorDot (video_gestures_wasm.go) rather than on
// its own faster timer: ApplyAIVisionOverlay already internally paces the
// actual detection passes (aiVisionIconInterval/aiVisionOCRInterval,
// ai_vision.go), so calling it more often than that would just add
// wasted no-op atomic-load checks, not faster detection.
func syncAIVisionOverlay(vw *VideoWidget) {
	overlay := js.Global().Get("document").Call("getElementById", "aiVisionCanvas")
	if overlay.IsUndefined() || overlay.IsNull() {
		return
	}
	style := overlay.Get("style")

	// Same first check syncVideoOverlay makes for the real <video> element,
	// and for the same reason: vw.IsStreaming() alone stays true even after
	// the user navigates off the Control tab (the WebRTC session keeps
	// running in the background, it isn't tied to which tab is visible),
	// so without this the overlay canvas kept floating on top of other
	// screens (Connections, Scripts & AI, ...) using whatever content rect
	// was last computed while the Control tab was showing. Confirmed live.
	if view.NavVideoHidden() {
		style.Set("visibility", "hidden")
		return
	}

	if vw == nil || !vw.IsStreaming() {
		style.Set("visibility", "hidden")
		return
	}
	wrapper := vw.activeViewportWrapper()
	if wrapper == nil || !wrapper.Visible() {
		style.Set("visibility", "hidden")
		return
	}
	size := wrapper.Size()
	if size.Width <= 0 || size.Height <= 0 || vw.contentRectW <= 0 || vw.contentRectH <= 0 {
		style.Set("visibility", "hidden")
		return
	}

	abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(wrapper)
	style.Set("left", pxf(abs.X+vw.contentRectX))
	style.Set("top", pxf(abs.Y+vw.contentRectY))
	style.Set("width", pxf(vw.contentRectW))
	style.Set("height", pxf(vw.contentRectH))
	style.Set("visibility", "visible")

	if !service.AIVisionEnabled() {
		return
	}
	captureAIVisionFrame(vw)
}

// captureAIVisionFrame draws the current <video> frame into a reusable
// offscreen canvas at a reduced resolution (aiVisionCaptureMaxSide), reads
// it back as RGBA, and hands it to service.ApplyAIVisionOverlay -- the
// wasm-side equivalent of the RGBA buffer every native platform's cgo
// decode callback already delivers per frame. ApplyAIVisionOverlay itself
// decides whether this particular call actually kicks off a detection pass
// or is a no-op (its own internal pacing) -- this function's only job is
// producing the pixels.
func captureAIVisionFrame(vw *VideoWidget) {
	el := webrtcVideoElement(vw)
	if el.IsUndefined() || el.IsNull() {
		return
	}
	vidW := el.Get("videoWidth").Int()
	vidH := el.Get("videoHeight").Int()
	if vidW <= 0 || vidH <= 0 {
		return
	}
	capW, capH := vidW, vidH
	if capW > aiVisionCaptureMaxSide || capH > aiVisionCaptureMaxSide {
		if capW >= capH {
			capH = capH * aiVisionCaptureMaxSide / capW
			capW = aiVisionCaptureMaxSide
		} else {
			capW = capW * aiVisionCaptureMaxSide / capH
			capH = aiVisionCaptureMaxSide
		}
	}
	if capW <= 0 || capH <= 0 {
		return
	}

	canvas := aiVisionOffscreenCanvas()
	if canvas.Get("width").Int() != capW {
		canvas.Set("width", capW)
	}
	if canvas.Get("height").Int() != capH {
		canvas.Set("height", capH)
	}
	ctx := canvas.Call("getContext", "2d")
	if ctx.IsUndefined() || ctx.IsNull() {
		return
	}
	ctx.Call("drawImage", el, 0, 0, capW, capH)

	imageData := ctx.Call("getImageData", 0, 0, capW, capH)
	jsData := imageData.Get("data") // Uint8ClampedArray, len capW*capH*4
	n := jsData.Get("length").Int()
	if n != capW*capH*4 {
		return
	}
	rgba := make([]byte, n)
	// CopyBytesToGo requires a Uint8Array specifically, not the
	// Uint8ClampedArray getImageData returns -- re-view the same
	// underlying buffer as one instead of copying twice.
	view := js.Global().Get("Uint8Array").New(jsData.Get("buffer"), jsData.Get("byteOffset"), n)
	js.CopyBytesToGo(rgba, view)

	service.ApplyAIVisionOverlay(rgba, capW, capH, capW*4)
}
