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
	lastMetalFrameX, lastMetalFrameY float32
	lastMetalFrameW, lastMetalFrameH float32
	lastMetalCursorVis               bool
	lastMetalCursorX, lastMetalCursorY float32
)

// updateMetalVideoFrame repositions the Metal overlay to track videoCanvas and
// forwards the virtual cursor position (computed from persistent UV state so the
// cursor does not teleport back between swipes).
func (vw *VideoWidget) updateMetalVideoFrame() {
	if !service.MetalVideoIsActive() {
		return
	}
	x, y, w, h := vw.videoCanvasFrame()
	if w <= 0 || h <= 0 {
		return
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
			// Map UV → widget-local dp: U=0 → contentRectX, U=1 → contentRectX+contentRectW.
			uc = u*cw + vw.contentRectX
			vc = v*ch + vw.contentRectY
		} else {
			uc = w / 2
			vc = h / 2
		}
		// Clamp to visible widget area so the cursor never appears outside the video.
		uc = clampFloat(uc, 0, w)
		vc = clampFloat(vc, 0, h)
	}

	if x == lastMetalFrameX && y == lastMetalFrameY && w == lastMetalFrameW && h == lastMetalFrameH &&
		cursorVisible == lastMetalCursorVis && uc == lastMetalCursorX && vc == lastMetalCursorY {
		return // Do not spam CGO / iOS main queue 60 times a second if nothing changed.
	}
	lastMetalFrameX, lastMetalFrameY, lastMetalFrameW, lastMetalFrameH = x, y, w, h
	lastMetalCursorVis, lastMetalCursorX, lastMetalCursorY = cursorVisible, uc, vc

	service.MetalVideoUpdateFrame(x, y, w, h)
	// Pass window-absolute coordinates: Metal cursor layer is on win.layer.
	service.MetalVideoUpdateCursor(x+uc, y+vc, cursorVisible)
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

func (vw *VideoWidget) androidCursorScale() int      { return 1 }
func (vw *VideoWidget) initAndroidCursorScale(_ int)  {}
func (vw *VideoWidget) onIMEHeightChanged(_ float32)  {}
func triggerRmbHaptic()                               {}

// ─────────────────────────────────────────────────────────────────────────────
// iosCursorImagePixels returns the arrow cursor bitmap scaled 3× for retina.
// The result is 54×72 NRGBA pixels displayed at 18×24 dp (contentsScale=3).
// ─────────────────────────────────────────────────────────────────────────────
func iosCursorImagePixels() ([]byte, int, int) {
	src := newOverlayCursorImage()
	nrgba, ok := src.(*image.NRGBA)
	if !ok {
		return nil, 0, 0
	}
	srcW := nrgba.Bounds().Dx()
	srcH := nrgba.Bounds().Dy()
	const scale = 3
	w, h := srcW*scale, srcH*scale
	out := make([]byte, w*h*4)
	for sy := 0; sy < srcH; sy++ {
		for sx := 0; sx < srcW; sx++ {
			base := sy*nrgba.Stride + sx*4
			p := nrgba.Pix[base : base+4 : base+4]
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					d := ((sy*scale+dy)*w + sx*scale+dx) * 4
					copy(out[d:d+4:d+4], p)
				}
			}
		}
	}
	return out, w, h
}
