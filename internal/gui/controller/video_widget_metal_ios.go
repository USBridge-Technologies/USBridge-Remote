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

// updateMetalVideoFrame repositions the Metal overlay to track videoCanvas.
func (vw *VideoWidget) updateMetalVideoFrame() {
	if !service.MetalVideoIsActive() {
		return
	}
	x, y, w, h := vw.videoCanvasFrame()
	if w <= 0 || h <= 0 {
		return
	}
	service.MetalVideoUpdateFrame(x, y, w, h)
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
// The video container fills everything below the tab/address bar.
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.container == nil || vw.parentWindow == nil {
		return
	}
	sz := vw.container.Size()
	canvasH := vw.parentWindow.Canvas().Size().Height
	topOffset := canvasH - sz.Height
	return 0, topOffset, sz.Width, sz.Height
}
