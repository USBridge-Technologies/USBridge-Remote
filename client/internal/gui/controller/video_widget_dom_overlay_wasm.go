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
//
// Hiding the DOM <video> overlay (and the touch/cursor layers stacked on
// top of it, video_gestures_wasm.go / video_widget_cursor_wasm.go) when
// app-level navigation moves off the Control tab used to be wired through
// view.OnOverlayShow/OnOverlayHide -- the same push-callback hooks the
// native GPU-overlay platforms register once per session. That doesn't fit
// wasm: stopMetalVideo below is called far more often than a real
// teardown (every fullscreen toggle, every reconnect -- see its callers in
// fullscreen_dialog.go and video_widget_ui.go), and each call nil'd the
// hooks out. A Show/Hide pairing that straddled one of those calls (e.g. a
// dropdown opened, then a reconnect happened, then the dropdown closed)
// lost its Hide -- the callback it would have fired was nil at that
// moment -- leaving the touch overlay's pointer-events permanently
// disabled until something else happened to re-register and reset it.
// Confirmed as the cause of mouse input silently stopping on the Control
// tab with no further errors.
//
// Reading view.VideoShouldBeHidden() directly on every poll tick instead
// (see syncVideoOverlay/syncTouchOverlay/syncCursorDot) removes the whole
// class: there's no registration to straddle and no callback to lose,
// just a plain flag composed live from nav state and the same
// overlayDepth counter dropdownPopup/VideoStartDialog already maintain,
// at most one ~150ms tick stale and self-healing on the very next one.
func (vw *VideoWidget) startMetalVideoOnWindow(_ fyne.Window, _ bool) {}
func (vw *VideoWidget) stopMetalVideo()                               {}
func (vw *VideoWidget) updateMetalVideoFrame()                        { syncVideoOverlay(vw) }
func (vw *VideoWidget) metalVideoEnterFullscreen(_ fyne.Window)       {}
func (vw *VideoWidget) metalVideoExitFullscreen()                     {}

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
	style := el.Get("style")

	// Nav-hidden (off the Control tab) is the one case that gets a real
	// visibility:hidden -- deliberately NOT view.VideoShouldBeHidden()
	// (nav OR popup), unlike the touch/cursor overlays below. Chrome
	// stops firing requestVideoFrameCallback entirely on a hidden
	// <video> (see ensureOverlayHooksRegistered's old doc comment, still
	// true here), which starves the watchdog and forces a reconnect --
	// tolerable when the user has actually navigated away (they're not
	// watching anyway), but not when they've just opened a settings
	// popup while still sitting on the Control tab: confirmed live,
	// coupling this to view.VideoShouldBeHidden() (as an earlier version
	// of this fix did) brought back a reconnect every few seconds
	// whenever *any* popup was open. Popups get the softer opacity:0
	// below instead -- invisible without ever pausing rVFC.
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
	if size.Width <= 0 || size.Height <= 0 {
		style.Set("visibility", "hidden")
		return
	}

	// vw.IsStreaming() alone goes true as soon as WebRTC signaling
	// completes (WebRTCVideoClient.ConnectToMoonlight returns right after
	// client.Connect(sessionID), well before OnVideoTrack/any real frame)
	// -- revealing this <video> element at that point paints an empty,
	// opaque black rectangle over VideoWidget's own Fyne-rendered
	// connecting spinner underneath (video_widget_spinner.go) for however
	// long negotiation/first-keyframe takes, instead of letting it show
	// through. videoWidth/videoHeight only become nonzero once the
	// element actually has real decoded dimensions to report (per the
	// HTMLVideoElement spec, 0 until the first frame's metadata loads),
	// so gate visibility on that too -- the spinner keeps showing through
	// this same overlay's hidden state until there's an actual picture to
	// replace it with.
	if el.Get("videoWidth").Int() <= 0 {
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

	// A popup/dialog open on top of the video (view.PopupActive(), the
	// same overlayDepth counter dropdownPopup/VideoStartDialog already
	// maintain) still needs to visually disappear -- it's a
	// position:fixed element that paints above Fyne's own canvas
	// regardless of what's logically on top -- but opacity:0 rather than
	// visibility:hidden: the element is still considered "rendered" with
	// opacity 0, so Chrome keeps calling requestVideoFrameCallback on it
	// (unlike visibility:hidden, see the nav-hidden branch above), and no
	// reconnect gets triggered just from opening a settings popup.
	if view.PopupActive() {
		style.Set("opacity", "0")
	} else {
		style.Set("opacity", "1")
	}
}
