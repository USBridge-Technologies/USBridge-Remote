package controller

import (
	"time"

	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
)

// mobileControlChromeBelow estimates Control's tab bar + AppFooter under the
// video when AbsolutePosition has not settled yet. Stacked tab icons+labels
// are ~48dp; AppFooterOuterHeight covers the version strip.
const mobileControlTabBarEstimate = float32(48)

func videoChromeBelow() float32 {
	h := view.AppFooterOuterHeight()
	if view.IsMobile() {
		h += mobileControlTabBarEstimate
	}
	return h
}

// videoContainerOrigin is the video container's top-left in window-canvas
// dp. Native overlays used to derive Y as canvasH − containerH, which is
// only correct when the container is flush with the canvas bottom. Control's
// AppFooter sits below the video, so that formula shifts the overlay down by
// the footer height (gap above, overlay covering the footer).
//
// AbsolutePositionForObject often reports (0,0) (or a too-small Y) for a
// frame when the overlay first starts — that paints over the header. Prefer
// AbsolutePosition only when it agrees with the chrome-aware estimate;
// otherwise use the estimate / last settled origin.
func (vw *VideoWidget) videoContainerOrigin() fyne.Position {
	if vw.container == nil {
		return fyne.NewPos(0, 0)
	}
	sz := vw.container.Size()
	var canvasH float32
	if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
		canvasH = vw.parentWindow.Canvas().Size().Height
	}

	estimated := estimatedVideoOriginY(vw, sz.Height, canvasH)

	if app := fyne.CurrentApp(); app != nil {
		if drv := app.Driver(); drv != nil {
			pos := drv.AbsolutePositionForObject(vw.container)
			if videoOriginLooksSettled(pos, sz, canvasH, estimated) {
				y := pos.Y
				// Never sit above the chrome-aware estimate — that paints
				// over the header.
				if estimated > y {
					y = estimated
				}
				out := fyne.NewPos(pos.X, y)
				vw.lastVideoCanvasOrigin = out
				return out
			}
		}
	}
	if vw.lastVideoCanvasOrigin.Y > 0 {
		// Drop a stale cache after rotate / resize (estimate moved a lot).
		// Reduced from 64 to 20 so status-bar / safe-area height changes (which
		// are typically ~24-48dp) correctly bust the cache instead of leaving
		// a black strip where the safe zone used to be.
		if estimated <= 0 || absFloat32(vw.lastVideoCanvasOrigin.Y-estimated) > 20 {
			vw.lastVideoCanvasOrigin = fyne.NewPos(0, 0)
		} else {
			return vw.lastVideoCanvasOrigin
		}
	}
	if estimated > 0 {
		return fyne.NewPos(0, estimated)
	}
	return fyne.NewPos(0, 0)
}

func estimatedVideoOriginY(vw *VideoWidget, containerH, canvasH float32) float32 {
	chrome := videoChromeBelow()
	if vw != nil && vw.parentWindow != nil && containerH > 0 {
		content := vw.parentWindow.Content()
		if content != nil {
			if app := fyne.CurrentApp(); app != nil {
				if drv := app.Driver(); drv != nil {
					root := drv.AbsolutePositionForObject(content)
					ch := content.Size().Height
					if ch > containerH {
						// Content is already laid out inside the safe-area
						// pad, so root.Y is safeTop. This yields safeTop+header
						// without double-counting safeBottom (canvasH−height
						// overshoots by the bottom inset).
						y := root.Y + (ch - containerH - chrome)
						if y < root.Y {
							y = root.Y
						}
						return y
					}
				}
			}
		}
	}
	if canvasH <= 0 || containerH <= 0 {
		return 0
	}
	y := canvasH - containerH - chrome
	if y < 0 {
		return 0
	}
	return y
}

func videoOriginLooksSettled(pos fyne.Position, sz fyne.Size, canvasH, estimatedY float32) bool {
	if sz.Width <= 0 || sz.Height <= 0 {
		return false
	}
	// (0,0) / tiny Y while the container is shorter than the canvas means the
	// driver has not placed the widget yet — the header is still above it.
	if pos.Y <= 1 && canvasH > 0 && sz.Height+8 < canvasH {
		return false
	}
	// AbsolutePosition that sits clearly above the chrome-aware estimate
	// paints the native overlay over the header (seen on Android after
	// edge-to-edge). Require it to be at least nearly the estimate.
	if estimatedY > 8 && pos.Y < estimatedY-12 {
		return false
	}
	return true
}

func absFloat32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func (vw *VideoWidget) activeViewportWrapper() *TouchpadWrapper {
	if vw.fullscreenDialog != nil && vw.fullscreenDialog.isFullscreen && vw.fullscreenDialog.touchpadWrapper != nil {
		return vw.fullscreenDialog.touchpadWrapper
	}
	return vw.touchpadWrapper
}

// InvalidateOverlayGeometry drops the cached canvas origin and forces the
// next render tick to remeasure the native video overlay — used after
// orientation / connected-chrome reflows change header or footer height.
func (vw *VideoWidget) InvalidateOverlayGeometry() {
	if vw == nil {
		return
	}
	vw.lastVideoCanvasOrigin = fyne.NewPos(0, 0)
	vw.forceCanvasRefresh.Store(true)
	vw.RefreshViewportGeometry()
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
	// Keep viewportPanMode armed across multi-touch / cancelled strokes —
	// only the footer button (or leaving Control) should disarm it.
	vw.viewportPanDragActive = false
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
