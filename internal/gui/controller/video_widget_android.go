//go:build android

package controller

import (
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

	// Attempt to create the Vulkan overlay at the full video area.
	// Coordinates (0,0,0,0) means "full screen" — setRect will refine later.
	if service.VKVideoAndroidCreate(0, 0, 0, 0) {
		logrus.Info("[Android/VK] Vulkan overlay created")
		if cb := vw.onNativeReady; cb != nil {
			vw.onNativeReady = nil
			cb()
		}
		return
	}

	// Fallback: Fyne canvas render ticker.
	fps := 60
	if vw.videoClient != nil {
		if cfg := vw.videoClient.GetConfig(); cfg != nil && cfg.VideoFPS > 0 {
			fps = cfg.VideoFPS
		}
	}
	logrus.Infof("[Android] Vulkan unavailable — render ticker → %d Hz (stream FPS)", fps)
	vw.startRenderTicker(fps)
}

func (vw *VideoWidget) stopMetalVideo() {
	vw.onNativeReady = nil
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

func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.videoCanvas == nil {
		return
	}
	pos := vw.videoCanvas.Position()
	sz := vw.videoCanvas.Size()
	return pos.X, pos.Y, sz.Width, sz.Height
}
