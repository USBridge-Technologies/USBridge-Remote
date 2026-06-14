//go:build android

package controller

import (
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (vw *VideoWidget) isNativeVideoActive() bool {
	return service.VKVideoAndroidIsActive()
}

// startMetalVideoOnWindow creates a Vulkan SurfaceView overlay for video.
// Falls back to the Fyne canvas render-ticker if Vulkan is unavailable.
func (vw *VideoWidget) startMetalVideoOnWindow(_ fyne.Window, fullscreen bool) {
	if fullscreen {
		return
	}

	// Wire overlay lifecycle hooks so menus/popups hide the VK SurfaceView.
	view.OnOverlayShow = func() { service.VKVideoAndroidSetHidden(true) }
	view.OnOverlayHide = func() { service.VKVideoAndroidSetHidden(false) }

	// Compute the actual pixel rect for the video area now, before creating
	// the overlay.  Passing (0,0,0,0) would cause the SurfaceView to be
	// created at 1×1, producing a 1×1 Vulkan swapchain.  All subsequent
	// frames would be presented on that invisible 1×1 surface while the Fyne
	// canvas stays frozen at the last pre-Vulkan frame.
	var px, py, pw, ph int
	done := make(chan struct{})
	fyne.Do(func() {
		x, y, w, h := vw.videoCanvasFrame()
		scale := float32(1)
		if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
			scale = vw.parentWindow.Canvas().Scale()
		}
		px = int(x * scale)
		py = int(y * scale)
		pw = int(w * scale)
		ph = int(h * scale)
		close(done)
	})
	<-done

	if service.VKVideoAndroidCreate(px, py, pw, ph) {
		logrus.Infof("[Android/VK] Vulkan overlay created at (%d,%d) %dx%d px", px, py, pw, ph)
		if view.OverlayActive() {
			service.VKVideoAndroidSetHidden(true)
		}
		if cb := vw.onNativeReady; cb != nil {
			vw.onNativeReady = nil
			cb()
		}
		// Fallthrough to startRenderTicker even with Vulkan — we need it to 
		// call renderLatestFrame, which hides the Fyne canvas and tracks resizes.
	} else {
		// Fallback: Disable overlay hooks if Vulkan failed.
		view.OnOverlayShow = nil
		view.OnOverlayHide = nil
		logrus.Infof("[Android] Vulkan unavailable — using Fyne canvas fallback")
	}

	fps := 60
	if vw.videoClient != nil {
		if cfg := vw.videoClient.GetConfig(); cfg != nil && cfg.VideoFPS > 0 {
			fps = cfg.VideoFPS
		}
	}
	vw.startRenderTicker(fps)
}

func (vw *VideoWidget) stopMetalVideo() {
	vw.onNativeReady = nil
	view.OnOverlayShow = nil
	view.OnOverlayHide = nil
	service.VKVideoAndroidDestroy()
}

func (vw *VideoWidget) updateMetalVideoFrame() {
	if !service.VKVideoAndroidIsActive() {
		return
	}
	x, y, w, h := vw.videoCanvasFrame()
	if w <= 0 || h <= 0 {
		return
	}
	scale := float32(1)
	if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
		scale = vw.parentWindow.Canvas().Scale()
	}
	service.VKVideoAndroidUpdateRect(int(x*scale), int(y*scale), int(w*scale), int(h*scale))
}

func (vw *VideoWidget) metalVideoEnterFullscreen(_ fyne.Window) {
	// Fullscreen on Android: hide the overlay (Fyne takes fullscreen).
	if service.VKVideoAndroidIsActive() {
		service.VKVideoAndroidSetHidden(true)
	}
}

func (vw *VideoWidget) metalVideoExitFullscreen() {
	if service.VKVideoAndroidIsActive() {
		service.VKVideoAndroidSetHidden(false)
	}
}

// videoCanvasFrame returns the video area rect in window-local dp coordinates.
// Uses same approach as Windows/macOS: y = canvasHeight - containerHeight.
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.container == nil || vw.parentWindow == nil {
		return
	}
	sz := vw.container.Size()
	canvasH := vw.parentWindow.Canvas().Size().Height
	topOffset := canvasH - sz.Height
	return 0, topOffset, sz.Width, sz.Height
}
