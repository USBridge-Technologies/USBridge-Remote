package controller

import (
	"time"

	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
)

// Portrait Control chrome under the video: 52dp row + 6/10 pads + 1dp TopLine.
// Landscape uses the compact strip only (version sits in that row).
const (
	mobileControlPortraitStrip  = float32(52 + 6 + 10 + 1)
	mobileControlLandscapeStrip = float32(36 + 4 + 4 + 1)
)

func videoChromeBelow() float32 {
	if !view.IsMobile() {
		return view.AppFooterOuterHeight()
	}
	if view.IsLandscape() {
		return mobileControlLandscapeStrip
	}
	return mobileControlPortraitStrip + view.AppFooterOuterHeight()
}

// overlayWindowPos maps Fyne mobile AbsolutePosition (InteractiveArea-relative)
// to window-canvas dp. Native overlays sit on the Activity decorView, whose
// (0,0) is the physical screen origin — the same space as Canvas.Size(), not
// the padded InteractiveArea. Without this, Vulkan is too high by the status
// bar / camera-cutout inset (worse on punch-hole phones).
func overlayWindowPos(absPos, interactiveOrigin fyne.Position) fyne.Position {
	return fyne.NewPos(absPos.X+interactiveOrigin.X, absPos.Y+interactiveOrigin.Y)
}

func canvasInteractiveOrigin(c fyne.Canvas) fyne.Position {
	if c == nil {
		return fyne.NewPos(0, 0)
	}
	pos, _ := c.InteractiveArea()
	return pos
}

// videoContainerOrigin is the video container's top-left from
// AbsolutePositionForObject. On mobile Fyne that is InteractiveArea-relative
// (status bar / cutout already subtracted). Native overlays on the Activity
// window must add canvasInteractiveOrigin via overlayWindowPos.
//
// Native overlays used to derive Y as canvasH − containerH, which is
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
			if videoOriginLooksSettled(vw, pos, sz, canvasH, estimated) {
				y := pos.Y
				// AbsolutePosition can briefly sit under the special-keys
				// header after safe-area clear; clamp so Vulkan never paints
				// under the keys (top content was clipped there).
				if r := vw.specialKeysHeaderReserve; r > 0 && y < r {
					y = r
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
	// Keep this IA-relative: mobile AbsolutePosition subtracts InteractiveArea,
	// and overlayWindowPos adds it back for the SurfaceView. Using full
	// canvasH here would include the status-bar inset twice.
	areaH := canvasH
	if vw != nil && vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
		if _, area := vw.parentWindow.Canvas().InteractiveArea(); area.Height > 0 {
			areaH = area.Height
		}
	}
	if vw != nil && vw.parentWindow != nil && containerH > 0 {
		content := vw.parentWindow.Content()
		if content != nil {
			if app := fyne.CurrentApp(); app != nil {
				if drv := app.Driver(); drv != nil {
					root := drv.AbsolutePositionForObject(content)
					ch := content.Size().Height
					if ch > containerH {
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
	if areaH <= 0 || containerH <= 0 {
		return 0
	}
	y := areaH - containerH - chrome
	if y < 0 {
		return 0
	}
	return y
}

func videoOriginLooksSettled(vw *VideoWidget, pos fyne.Position, sz fyne.Size, canvasH, estimatedY float32) bool {
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
	// edge-to-edge). Require it to be at least nearly the estimate — except
	// while the keyboard stack is open: clearing the top safe inset drops
	// AbsolutePosition faster than the estimate, and rejecting it leaves a
	// black strip under the special keys.
	if estimatedY > 8 && pos.Y < estimatedY-12 {
		if vw != nil && (vw.IsVirtualKeyboardVisible() || vw.IsSystemIMESticky()) {
			return true
		}
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

// SyncNativeOverlayVisibility applies NavVideoHidden / popup state to the
// native video surface immediately. Tab switches must not wait for the next
// render tick: after zoom the Control container can already be size 0, and
// a delayed hide left Vulkan covering Devices.
func (vw *VideoWidget) SyncNativeOverlayVisibility() {
	if vw == nil {
		return
	}
	vw.updateMetalVideoFrame()
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
