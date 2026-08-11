package controller

import (
	"time"

	"fyne.io/fyne/v2"
)

func (vw *VideoWidget) activeViewportWrapper() *TouchpadWrapper {
	if vw.fullscreenDialog != nil && vw.fullscreenDialog.isFullscreen && vw.fullscreenDialog.touchpadWrapper != nil {
		return vw.fullscreenDialog.touchpadWrapper
	}
	return vw.touchpadWrapper
}

// RefreshViewportGeometry recomputes the touchpad/content rect against the
// viewport wrapper's current (now-visible) size. The pre-set block in
// startVideoWithParamsInternal only takes effect if the Control tab is
// already visible when a device/monitor switch restarts the stream; if the
// switch happened from the Devices tab, the wrapper's size can be stale or
// zero at that point and absolute mouse mapping is left pointing at the old
// monitor's geometry until something else (e.g. a codec change made from the
// Control tab) happens to call UpdateTouchpadAndContentRect again. Call this
// whenever the Control tab becomes visible so the mapping is corrected
// immediately regardless of what triggered the last stream (re)start.
func (vw *VideoWidget) RefreshViewportGeometry() {
	fyne.Do(func() {
		tw := vw.activeViewportWrapper()
		if tw == nil {
			return
		}
		sz := tw.Size()
		if sz.Width <= 0 || sz.Height <= 0 {
			return
		}
		vw.UpdateTouchpadAndContentRect(sz.Width, sz.Height, vw.GetCurrentFrame())
	})
}

func (vw *VideoWidget) refreshViewportViews() {
	fyne.Do(func() {
		if vw.touchpadWrapper != nil {
			vw.touchpadWrapper.Refresh()
		}
		if vw.fullscreenDialog != nil && vw.fullscreenDialog.touchpadWrapper != nil {
			vw.fullscreenDialog.touchpadWrapper.Refresh()
		}
	})
}

func (vw *VideoWidget) cancelLocalTouchState() {
	vw.CancelTouchDownDelay()
	vw.touchActive = false
	vw.dragButton = 0
	vw.isDragging = false
	vw.scrollDragAxis = ""
	if vw.lmbHeld {
		vw.lmbHeld = false
		vw.enqueueMouseButtonUp(1)
	}
	vw.resetRelativeMoveAccumulator()
}

func (vw *VideoWidget) shouldIgnoreTouchInput() bool {
	return vw.multiTouchActive || time.Since(vw.lastMultiTouchAt) < 180*time.Millisecond
}

// softClampEdgePan clamps val to [lo, hi] with a smoothstep blend zone of size
// zone inside each limit. The blend zone runs from (lo) to (lo+zone) and from
// (hi-zone) to (hi). Inside these zones the output is eased toward the limit
// using a smoothstep curve whose derivative is ZERO at the hard limit — so tiny
// oscillations of val around the boundary (touch noise ε) produce only ε²/zone
// change in the output instead of ε, effectively eliminating viewport jitter.
// Shared (not android-only) since wasm's own virtual-cursor-follow viewport
// panning (video_widget_cursor_wasm.go) reuses the same math.
func softClampEdgePan(val, lo, hi, zone float32) float32 {
	if val <= lo {
		return lo
	}
	if val >= hi {
		return hi
	}
	if halfRange := (hi - lo) / 2; zone > halfRange {
		zone = halfRange
	}
	if zone <= 0 {
		return val
	}
	if val < lo+zone {
		// Near lower limit: ease val toward lo.
		t := (lo + zone - val) / zone // 1 at lo, 0 at lo+zone
		t = t * t * (3 - 2*t)         // smoothstep — zero derivative at lo
		return val*(1-t) + lo*t
	}
	if val > hi-zone {
		// Near upper limit: ease val toward hi.
		t := (val - (hi - zone)) / zone // 0 at hi-zone, 1 at hi
		t = t * t * (3 - 2*t)           // smoothstep — zero derivative at hi
		return val*(1-t) + hi*t
	}
	return val
}
