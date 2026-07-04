//go:build ios

package controller

import (
	"image"
	"time"

	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)


func (vw *VideoWidget) isNativeVideoActive() bool {
	return service.MetalVideoIsActive()
}

// getNativeFPS returns the current Metal render FPS.
func (vw *VideoWidget) getNativeFPS() float64 {
	return service.MetalVideoLastFPS()
}

// getMetalLastFrame is a no-op on iOS (no pause-snapshot path needed).
func (vw *VideoWidget) getMetalLastFrame() *image.RGBA { return nil }

// ensureNativeOverlayOnTop is a no-op on iOS (CALayer z-order is stable).
func (vw *VideoWidget) ensureNativeOverlayOnTop() {}

// startMetalVideoOnWindow creates the Metal CALayer overlay on the key UIWindow.
// On iOS the UIWindow is found internally in C code — no RunNative needed.
func (vw *VideoWidget) startMetalVideoOnWindow(window fyne.Window, fullscreen bool) {
	view.OnOverlayShow = func() { service.MetalVideoSetHidden(true) }
	view.OnOverlayHide = func() { service.MetalVideoSetHidden(false) }

	var x, y, w, h float32
	if !fullscreen {
		// Get the canvas frame on the main thread where layout is computed.
		// Retry up to 500 ms if layout isn't ready yet (same pattern as Android).
		for attempt := 0; attempt < 5; attempt++ {
			if attempt > 0 {
				time.Sleep(100 * time.Millisecond)
			}
			done := make(chan struct{})
			fyne.Do(func() {
				x, y, w, h = vw.videoCanvasFrame()
				close(done)
			})
			<-done
			if w > 0 && h > 0 {
				break
			}
			logrus.Infof("📱 [Metal/iOS] layout not ready (attempt %d), waiting...", attempt+1)
		}
		logrus.Infof("📱 [Metal/iOS] videoCanvas frame x=%.0f y=%.0f w=%.0f h=%.0f", x, y, w, h)
		if w <= 0 || h <= 0 {
			// Layout not ready yet — create full-window overlay (w=0,h=0 signals
			// full-window in C). updateMetalVideoFrame repositions it once layout
			// is computed in the next render-ticker cycle.
			logrus.Info("📱 [Metal/iOS] layout not ready — creating full-window overlay")
			x, y, w, h = 0, 0, 0, 0
		}
	}

	logrus.Infof("📱 [Metal/iOS] creating overlay (fullscreen=%v)", fullscreen)
	// w=0, h=0 passed to C when fullscreen=true signals full-window mode.
	if !service.MetalVideoCreate(0, x, y, w, h) {
		logrus.Warn("📱 [Metal/iOS] failed to create overlay — Fyne canvas fallback")
		view.OnOverlayShow = nil
		view.OnOverlayHide = nil
		return
	}

	logrus.Info("📱 [Metal/iOS] overlay active")
	if view.OverlayActive() {
		service.MetalVideoSetHidden(true)
	}

	// Upload the arrow cursor image (3× upscaled so it looks sharp on retina).
	go func() {
		pix, cw, ch := iosCursorImagePixels()
		if pix != nil {
			service.MetalVideoSetCursorImage(pix, cw, ch)
		}
	}()

	// Clear the Fyne canvas — Metal overlay now handles rendering.
	fyne.Do(func() {
		if vw.videoCanvas != nil {
			vw.videoCanvas.Image = nil
			vw.videoCanvas.Translucency = 0
			vw.videoCanvas.Refresh()
		}
		if cb := vw.onNativeReady; cb != nil {
			vw.onNativeReady = nil
			cb()
		}
	})
}

// stopMetalVideo destroys the Metal overlay.
func (vw *VideoWidget) stopMetalVideo() {
	vw.onNativeReady = nil
	view.OnOverlayShow = nil
	view.OnOverlayHide = nil
	service.MetalVideoDestroy()
	vw.metalFPSWarned.Store(false)
}

var (
	// Clip rect = widget (touchpad) bounds. Content rect = zoomed/panned video rect.
	lastMetalClipX, lastMetalClipY     float32
	lastMetalClipW, lastMetalClipH     float32
	lastMetalFrameX, lastMetalFrameY   float32
	lastMetalFrameW, lastMetalFrameH   float32
	lastMetalCursorVis                 bool
	lastMetalCursorX, lastMetalCursorY float32
)

// videoWidgetFrame returns the touchpad widget bounds in window-local dp coordinates.
// This is the visible area the Metal overlay must not overflow.
func (vw *VideoWidget) videoWidgetFrame() (x, y, w, h float32) {
	if vw.container == nil || vw.touchpadWrapper == nil || vw.parentWindow == nil {
		return
	}
	szMain := vw.container.Size()
	canvasH := vw.parentWindow.Canvas().Size().Height
	topOffset := canvasH - szMain.Height

	if getImeExpandHeightDp() > 0 {
		if vw.contentContainer != nil && vw.contentContainer.Visible() {
			absPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(vw.contentContainer)
			videoH := absPos.Y
			if videoH <= 0 {
				videoH = canvasH
				if kh := vw.contentContainer.Size().Height; kh > 0 {
					videoH -= kh
				}
			}
			if videoH > 0 {
				// Expand clip rect to cover the top interface (tabs area) while IME is open.
				return 0, 0, szMain.Width, videoH
			}
		}
	}

	szVideo := vw.touchpadWrapper.Size()
	return 0, topOffset, szVideo.Width, szVideo.Height
}

// updateMetalVideoFrame repositions the Metal overlay:
//   clip rect   = widget (touchpad) bounds — constrains the visible area.
//   content rect = zoomed/panned video content — may extend outside clip when zoomed.
// The Metal C layer uses clipsToBounds on the clip container so the video never
// bleeds over the toolbar or button panel regardless of zoom level.
func (vw *VideoWidget) updateMetalVideoFrame() {
	if !service.MetalVideoIsActive() {
		return
	}

	// Clip rect = widget bounds (never changes with zoom/pan).
	clipX, clipY, clipW, clipH := vw.videoWidgetFrame()
	if clipW <= 0 || clipH <= 0 {
		return
	}

	// Note: keyboard-height capping is handled in C (metal_video_update_layout) using
	// direct UIKit window coordinates. Go-side Fyne coordinates are unreliable during
	// keyboard transitions on iOS, so we let C override the clip rect when kbH > 100pt.

	// Content rect = zoomed/panned video position (may be outside clip bounds).
	contentX, contentY, contentW, contentH := vw.videoCanvasFrame()
	if contentW <= 0 || contentH <= 0 {
		// Fallback: content = clip.
		contentX, contentY, contentW, contentH = clipX, clipY, clipW, clipH
	}

	// Cursor position: use virtualCursorU/V (persistent across touch events) so
	// the arrow stays where the user left it rather than jumping to the finger tip.
	cursorVisible := isVirtualCursorLikeMode(vw.GetMouseInputMode())
	var uc, vc float32
	if cursorVisible {
		cw := vw.contentRectW
		ch := vw.contentRectH
		if cw > 0 && ch > 0 {
			vw.vcMu.Lock()
			u := vw.virtualCursorU
			v := vw.virtualCursorV
			vw.vcMu.Unlock()
			uc = u * cw
			vc = v * ch
		} else {
			uc = contentW / 2
			vc = contentH / 2
		}
		// Clamp to content rect dimensions.
		uc = clampFloat(uc, 0, contentW)
		vc = clampFloat(vc, 0, contentH)
	}

	if clipX == lastMetalClipX && clipY == lastMetalClipY && clipW == lastMetalClipW && clipH == lastMetalClipH &&
		contentX == lastMetalFrameX && contentY == lastMetalFrameY && contentW == lastMetalFrameW && contentH == lastMetalFrameH &&
		cursorVisible == lastMetalCursorVis && uc == lastMetalCursorX && vc == lastMetalCursorY {
		return // Nothing changed — avoid spamming the CGO/main-queue 60×/s.
	}
	lastMetalClipX, lastMetalClipY, lastMetalClipW, lastMetalClipH = clipX, clipY, clipW, clipH
	lastMetalFrameX, lastMetalFrameY, lastMetalFrameW, lastMetalFrameH = contentX, contentY, contentW, contentH
	lastMetalCursorVis, lastMetalCursorX, lastMetalCursorY = cursorVisible, uc, vc

	service.MetalVideoUpdateLayout(clipX, clipY, clipW, clipH, contentX, contentY, contentW, contentH)
	// Cursor is on win.layer (window-absolute coords): content origin + UV offset.
	service.MetalVideoUpdateCursor(contentX+uc, contentY+vc, cursorVisible)
}

// updateNativeViewportAndCursor implements the iOS equivalent of Android's
// updateNativeViewportAndCursor: in virtual cursor mode it pans the viewport
// to keep the cursor centred on screen, then repositions the Metal overlay.
func (vw *VideoWidget) updateNativeViewportAndCursor() {
	if isVirtualCursorLikeMode(vw.GetMouseInputMode()) {
		vw.vcMu.Lock()
		targetU := vw.virtualCursorU
		targetV := vw.virtualCursorV
		vw.vcMu.Unlock()

		vw.centerViewportOnVirtualCursor(targetU, targetV)

		// Recompute contentRect after the pan change.
		if tw := vw.activeViewportWrapper(); tw != nil {
			vw.UpdateTouchpadAndContentRect(vw.touchpadSizeW, vw.touchpadSizeH, nil)
		}
	}
	vw.updateMetalVideoFrame()
}

// centerViewportOnVirtualCursor pans the viewport so the virtual cursor is
// centred on screen (RustDesk-style follow). Only effective when zoom > 1.
func (vw *VideoWidget) centerViewportOnVirtualCursor(u, v float32) {
	if vw.zoomScale <= 1.001 {
		return
	}
	cw := vw.baseContentRectW * vw.zoomScale
	ch := vw.baseContentRectH * vw.zoomScale

	// X: cursor-centering pan.
	if cw > vw.touchpadSizeW {
		idealPanX := cw * (0.5 - u)
		maxPanX := (cw - vw.touchpadSizeW) / 2
		zoneX := vw.touchpadSizeW * 0.15
		vw.panOffsetX = iosSoftClamp(idealPanX, -maxPanX, maxPanX, zoneX)
	} else {
		vw.panOffsetX = 0
	}

	// Y: cursor-centering pan.
	availH := vw.touchpadSizeH - vw.bottomInset
	if ch > availH {
		idealPanY := availH/2 - (availH-ch) - v*ch
		maxPanY := ch - availH
		zoneY := availH * 0.15
		vw.panOffsetY = iosSoftClamp(idealPanY, 0, maxPanY, zoneY)
	} else {
		vw.panOffsetY = 0
	}

	vw.recalculateViewport()
}

// iosSoftClamp is the smoothstep edge-pan clamp used by centerViewportOnVirtualCursor.
// Identical to softClampEdgePan in video_widget_android.go.
func iosSoftClamp(val, lo, hi, zone float32) float32 {
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
		t := (lo + zone - val) / zone
		t = t * t * (3 - 2*t)
		return val*(1-t) + lo*t
	}
	if val > hi-zone {
		t := (val - (hi - zone)) / zone
		t = t * t * (3 - 2*t)
		return val*(1-t) + hi*t
	}
	return val
}

// metalVideoEnterFullscreen recreates the overlay in full-window mode.
func (vw *VideoWidget) metalVideoEnterFullscreen(_ fyne.Window) {
	service.MetalVideoDestroy()
	vw.startMetalVideoOnWindow(nil, true)
}

// metalVideoExitFullscreen restores the overlay at the video widget bounds.
func (vw *VideoWidget) metalVideoExitFullscreen() {
	service.MetalVideoDestroy()
	vw.startMetalVideoOnWindow(vw.parentWindow, false)
}

// videoCanvasFrame returns the video container bounds in window-local dp coordinates.
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.container == nil || vw.touchpadWrapper == nil || vw.parentWindow == nil {
		return
	}
	// Fyne's AbsolutePositionForObject is unreliable on mobile canvases.
	// We calculate Y manually: mainContainer is placed below the header.
	szMain := vw.container.Size()
	canvasH := vw.parentWindow.Canvas().Size().Height
	topOffset := canvasH - szMain.Height

	// Apply zoom and pan coordinates directly to the native overlay frame!
	// This naturally zooms and crops the CALayer.
	if vw.contentRectW > 0 && vw.contentRectH > 0 {
		return vw.contentRectX, topOffset + vw.contentRectY, vw.contentRectW, vw.contentRectH
	}

	// The video container (touchpadWrapper) size dynamically shrinks when the virtual
	// keyboard panel appears at the bottom.
	szVideo := vw.touchpadWrapper.Size()
	return 0, topOffset, szVideo.Width, szVideo.Height
}

// ─────────────────────────────────────────────────────────────────────────────
// Stubs for Android-specific APIs (no-ops on iOS).
// ─────────────────────────────────────────────────────────────────────────────

func (vw *VideoWidget) androidCursorScale() int     { return 1 }
func (vw *VideoWidget) initAndroidCursorScale(_ int) {}
func triggerRmbHaptic()                              {}

// onIMEHeightChanged is called when the iOS software keyboard appears/disappears.
// Keyboard height is also tracked directly in C (UIKeyboardWillChangeFrameNotification)
// and queried each frame via MetalVideoGetKeyboardHeight, so the next updateMetalVideoFrame
// call will automatically pick up the change.  We only need to invalidate the
// last-frame cache here so the update happens in the very next tick (not next change).
func (vw *VideoWidget) onIMEHeightChanged(imeHeightDp float32) {
	const minRealIMEDp = 100
	imeOpen := imeHeightDp > minRealIMEDp
	if imeOpen {
		setImeExpandHeightDp(imeHeightDp)
	} else {
		setImeExpandHeightDp(0)
	}
	lastMetalClipH = 0 // force cache miss → immediate layout update
	vw.forceCanvasRefresh.Store(true)
}

// iosCursorImagePixels rasterizes cursor-pointer.svg at 3× (54×72 px) —
// the same green arrow used by Android/Vulkan, displayed at 18×24 dp.
func iosCursorImagePixels() ([]byte, int, int) {
	return cursorSVGPixels(3)
}
