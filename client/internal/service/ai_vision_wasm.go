//go:build js && wasm

package service

import (
	"syscall/js"

	"usbridge-client/internal/localui"
)

// aiVisionCanvasID must match the <canvas id="..."> web/index.html
// declares -- positioned/sized to track the <video> element's own content
// rect by controller/video_widget_ai_vision_wasm.go's syncAIVisionOverlay
// (this file only ever touches its pixel content, never its CSS box, same
// split video_widget_dom_overlay_wasm.go's syncVideoOverlay already
// establishes for the real <video> element).
const aiVisionCanvasID = "aiVisionCanvas"

func init() {
	aiVisionMetalPush = pushAIVisionOverlayToCanvas
	aiVisionMetalClear = clearAIVisionOverlayCanvas
}

// AIVisionSupported reports true on the web build: icon_detect runs via
// onnxruntime-web in the browser tab itself (internal/localui/
// parser_wasm.go) instead of onnxruntime_go's cgo binding every other
// platform uses -- see that file's doc comment. There is no OCR stage
// (dbnet+svtr) on this platform, so the overlay only ever draws icon
// boxes, never text/green outlines -- acceptable for a live preview
// checkbox; ui.parse callers wanting OCR still forward to the device the
// same way they do when local ui.parse offload isn't set up at all.
func AIVisionSupported() bool { return true }

// pushAIVisionOverlayToCanvas is wasm's aiVisionMetalPush (see that var's
// doc comment -- like macOS's Metal path, there's no compositor layer here
// to write pixels into in place, since the real video pixels live entirely
// inside the browser's own <video> decode, never crossing into a Go-owned
// buffer -- see video_widget_dom_overlay_wasm.go's own doc comment).
// Builds a fully transparent w×h RGBA image with just this pass's boxes on
// it (buildAIVisionOverlayImage, the exact same drawing code every other
// platform's overlay path uses) and blits it onto #aiVisionCanvas with
// putImageData. Resizing the canvas's width/height attributes (its pixel
// backing store) to w,h -- as opposed to its CSS box, owned separately by
// video_widget_ai_vision_wasm.go's syncAIVisionOverlay -- is what makes the
// browser scale these pixels to match the video's own on-screen size
// automatically, the same way the <video> element's own native decode
// resolution differs from its CSS display size.
func pushAIVisionOverlayToCanvas(result *localui.Result, w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	canvas := js.Global().Get("document").Call("getElementById", aiVisionCanvasID)
	if canvas.IsUndefined() || canvas.IsNull() {
		return
	}
	if canvas.Get("width").Int() != w {
		canvas.Set("width", w)
	}
	if canvas.Get("height").Int() != h {
		canvas.Set("height", h)
	}
	ctx := canvas.Call("getContext", "2d")
	if ctx.IsUndefined() || ctx.IsNull() {
		return
	}

	img := buildAIVisionOverlayImage(result, w, h)
	jsBytes := js.Global().Get("Uint8Array").New(len(img.Pix))
	js.CopyBytesToJS(jsBytes, img.Pix)
	clamped := js.Global().Get("Uint8ClampedArray").New(jsBytes.Get("buffer"))
	imageData := js.Global().Get("ImageData").New(clamped, w, h)
	ctx.Call("putImageData", imageData, 0, 0)
}

// clearAIVisionOverlayCanvas wipes #aiVisionCanvas the moment the checkbox
// is turned off (see SetAIVisionEnabled) so a stale overlay never lingers
// on screen after detection stops.
func clearAIVisionOverlayCanvas() {
	canvas := js.Global().Get("document").Call("getElementById", aiVisionCanvasID)
	if canvas.IsUndefined() || canvas.IsNull() {
		return
	}
	ctx := canvas.Call("getContext", "2d")
	if ctx.IsUndefined() || ctx.IsNull() {
		return
	}
	ctx.Call("clearRect", 0, 0, canvas.Get("width"), canvas.Get("height"))
}
