//go:build js && wasm

// The wasm counterpart of the native GPU video overlays
// (video_widget_metal_darwin.go/video_widget_android.go/etc.): instead of a
// Vulkan/Metal surface layered under Fyne's own window, this positions the
// browser's own <video> element (already playing the WebRTC stream
// natively, hardware-decoded -- see internal/webrtcweb/client_wasm.go) as a
// CSS overlay directly over the video widget's on-screen rect. The browser
// composites it straight to the screen; no per-frame pixel ever crosses
// into Go, unlike the drawImage+getImageData readback path this replaces
// (WebRTCClient.StartFrameCapture, kept only as a manual fallback).
package controller

import (
	"syscall/js"

	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
)

// isNativeVideoActive reports whether the DOM video overlay is the thing
// actually showing pixels right now -- mirrors Android/Metal's meaning of
// the same method (see their own doc comments): when true, VideoWidget's
// own Fyne-canvas render path skips drawing a frame into the touchpad's
// canvas.Image, since the real pixels are already on screen via a
// different compositing path entirely.
func (vw *VideoWidget) isNativeVideoActive() bool {
	el := webrtcVideoElement(vw)
	return !el.IsUndefined() && !el.IsNull() && vw.IsStreaming()
}

// videoOverlayForceHidden mirrors what every native GPU-overlay platform
// (VKVideoAndroidSetHidden/MetalVideoSetHidden/VKVideoSetHidden) does when
// a Fyne overlay (menu/dialog) or app-level navigation (switching off the
// Control tab, opening the connection-manager screen — see
// MainWindow.syncVideoOverlayForNav) wants the video hidden without
// actually tearing the stream down: those platforms hide the native
// surface so it doesn't paint over the dialog/other screen; the DOM
// overlay needs the exact same guard, since it's a position:fixed element
// that paints above Fyne's own canvas regardless of which Fyne screen is
// logically "on top". Read/written only from the Fyne main goroutine
// (view.OnOverlayShow/Hide and syncVideoOverlay's own callers are all
// invoked from there), so a plain bool is enough -- wasm has no real
// parallelism anyway (single OS thread, cooperatively scheduled goroutines).
var videoOverlayForceHidden bool

// startMetalVideoOnWindow/stopMetalVideo/updateMetalVideoFrame/
// metalVideoEnterFullscreen/metalVideoExitFullscreen exist purely so wasm
// satisfies the same method set the cross-platform code in
// video_widget_ui.go and video_widget_viewport_stub.go expects every
// platform to provide (see video_widget_metal_stub.go, which used to cover
// wasm too before this file narrowed that stub's build tag). Real
// platforms call startMetalVideoOnWindow from video_widget_ui.go's
// handleVideoFrame at frameNum==1 -- but that call site is inside the
// frame!=nil branch, and every frame wasm ever delivers is nil (see
// webrtc_video_client_wasm.go's OnVideoTrack: WebRTC decode never produces
// a Go-side image.Image at all), so that call site is dead code here.
// ensureOverlayHooksRegistered below (called from syncVideoOverlay
// instead) is the real registration point for wasm.
func (vw *VideoWidget) startMetalVideoOnWindow(_ fyne.Window, _ bool) {}
func (vw *VideoWidget) stopMetalVideo() {
	view.OnOverlayShow = nil
	view.OnOverlayHide = nil
	videoOverlayHooksRegistered = false
}
func (vw *VideoWidget) updateMetalVideoFrame()                  { syncVideoOverlay(vw) }
func (vw *VideoWidget) metalVideoEnterFullscreen(_ fyne.Window) {}
func (vw *VideoWidget) metalVideoExitFullscreen()               {}

// videoOverlayHooksRegistered guards ensureOverlayHooksRegistered below so
// it only wires view.OnOverlayShow/OnOverlayHide once per session, not on
// every syncVideoOverlay tick.
var videoOverlayHooksRegistered bool

// ensureOverlayHooksRegistered wires the same view.OnOverlayShow/
// OnOverlayHide hooks every native platform's own startMetalVideoOnWindow
// registers (see video_widget_android.go's version for the pattern this
// mirrors), lazily on the first syncVideoOverlay call that finds a real
// video element -- see startMetalVideoOnWindow's doc comment for why that
// method itself is never actually invoked under wasm and can't be the
// registration point here. Without this,
// MainWindow.syncVideoOverlayForNav's NotifyOverlayShow/Hide calls on tab
// switches and dialog open/close are no-ops under wasm (OnOverlayShow/Hide
// stay nil), leaving the DOM <video> overlay visibly painted over every
// other tab/screen -- confirmed live (switching to the Devices tab left
// the video on top).
func ensureOverlayHooksRegistered(vw *VideoWidget) {
	if videoOverlayHooksRegistered {
		return
	}
	videoOverlayHooksRegistered = true
	view.OnOverlayShow = func() {
		videoOverlayForceHidden = true
		syncVideoOverlay(vw)
	}
	view.OnOverlayHide = func() {
		videoOverlayForceHidden = false
		syncVideoOverlay(vw)
	}
	// Deliberately NOT seeded from view.OverlayActive() (unlike
	// video_widget_android.go's equivalent registration point): that depth
	// counter is shared ambient state touched by every unrelated
	// popup/dropdown in the whole app, not scoped to "is something
	// covering the video right now" -- reading it here risked snapshotting
	// a stale "hidden" state and setting visibility:hidden on the <video>
	// element. That's far more damaging under wasm than it sounds: Chrome
	// stops firing requestVideoFrameCallback entirely on a hidden <video>
	// (confirmed live), which starves WatchVideoFrames' onFrame(nil) loop,
	// which starves VideoWidget's lastFrameTime bookkeeping, which trips
	// checkVideoSilence's 4s mid-stream watchdog into forcing a full
	// reconnect -- whose teardown (stopMetalVideo) resets
	// videoOverlayHooksRegistered, so the *next* session's registration
	// re-reads the same racy counter and can repeat the exact same
	// hidden-video/no-rVFC/watchdog-reconnect cycle indefinitely. Confirmed
	// live as the actual cause of a client reconnecting every ~4s
	// indefinitely with "frame=1" and never another frame logged.
	// Starting visible and trusting the two hooks above for every real
	// state change from here on avoids the whole class of bug.
	videoOverlayForceHidden = false
}

// videoCanvasFrame mirrors video_widget_metal_stub.go's generic
// implementation (Fyne's own videoCanvas widget geometry) -- nothing in
// the wasm build's own code calls this today, it exists only for method-set
// parity with the other platforms' files.
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.videoCanvas == nil {
		return
	}
	pos := vw.videoCanvas.Position()
	sz := vw.videoCanvas.Size()
	return pos.X, pos.Y, sz.Width, sz.Height
}

// webrtcVideoElement returns the browser <video> element behind vw's
// current video client, or the zero js.Value if there isn't one (not
// connected yet, or a non-WebRTC VideoClient -- shouldn't happen under
// wasm, but this build has no other VideoClient implementation to assert
// against).
func webrtcVideoElement(vw *VideoWidget) js.Value {
	if vw == nil {
		return js.Value{}
	}
	wc, ok := vw.videoClient.(*service.WebRTCVideoClient)
	if !ok || wc == nil {
		return js.Value{}
	}
	return wc.VideoElement()
}

// lastKnownVideoW/H remember the last video pixel size we fed into
// UpdateTouchpadAndContentRect, so syncVideoOverlay only pays for the
// (still cheap) letterbox recompute when the actual stream resolution
// changes, not on every poll tick.
var lastKnownVideoW, lastKnownVideoH int

// syncVideoOverlay keeps the real <video> element's CSS box positioned
// exactly over vw's current letterboxed content rect, and its
// visibility/pointer-events in sync with whether a session is actually
// streaming -- the same lifecycle syncCursorDot/syncTouchOverlay already
// follow (video_widget_cursor_wasm.go / video_gestures_wasm.go). Called
// from that same 150ms poll loop, and immediately on every pan/zoom/drag
// update via updateNativeViewportAndCursor so the video visually keeps up
// with a gesture instead of lagging to the next tick.
func syncVideoOverlay(vw *VideoWidget) {
	el := webrtcVideoElement(vw)
	if el.IsUndefined() || el.IsNull() {
		return
	}
	ensureOverlayHooksRegistered(vw)
	style := el.Get("style")

	if videoOverlayForceHidden {
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
	if size.Width <= 0 || size.Height <= 0 {
		style.Set("visibility", "hidden")
		return
	}

	// Pick up a real stream resolution change (e.g. the host changed
	// display mode mid-session) -- videoWidth/videoHeight are plain JS
	// property reads on the <video> element, no readback involved. Feeding
	// this into UpdateTouchpadAndContentRect keeps vw.contentRectX/Y/W/H
	// (the letterbox-aware box every other platform also computes) correct
	// for both the CSS box below and mouse-position mapping
	// (PositionToAbsolute).
	vidW := el.Get("videoWidth").Int()
	vidH := el.Get("videoHeight").Int()
	if vidW > 0 && vidH > 0 && (vidW != lastKnownVideoW || vidH != lastKnownVideoH) {
		lastKnownVideoW, lastKnownVideoH = vidW, vidH
		vw.lastVideoImgW = float32(vidW)
		vw.lastVideoImgH = float32(vidH)
		vw.UpdateTouchpadAndContentRect(size.Width, size.Height, nil)
	}

	abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(wrapper)
	x := abs.X + vw.contentRectX
	y := abs.Y + vw.contentRectY
	style.Set("left", pxf(x))
	style.Set("top", pxf(y))
	style.Set("width", pxf(vw.contentRectW))
	style.Set("height", pxf(vw.contentRectH))
	style.Set("visibility", "visible")
}
