//go:build android

package controller

import (
	"math"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (vw *VideoWidget) isNativeVideoActive() bool {
	return service.VKVideoAndroidIsActive()
}

// keepNativeVideoAliveForFullscreenTransition reports true on Android because
// the Vulkan SurfaceView overlay is attached to the Activity, not to a Fyne
// window. Destroying and recreating it on each fullscreen toggle is wrong —
// the SurfaceView stays alive through window content changes.
func (vw *VideoWidget) keepNativeVideoAliveForFullscreenTransition() bool { return true }

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
		// Reinitialize cursor at the correct display density scale.
		vw.initAndroidCursorScale(vw.androidCursorScale())
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

// vkLastRenderedW/H track the last pixel size sent to the Vulkan overlay.
// Any change (rotation, keyboard, fullscreen) triggers a forced swapchain recreation
// so the render thread picks up the new surface dimensions immediately.
var vkLastRenderedW, vkLastRenderedH int

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
	pw, ph := int(w*scale), int(h*scale)
	if pw != vkLastRenderedW || ph != vkLastRenderedH {
		vkLastRenderedW, vkLastRenderedH = pw, ph
		service.VKVideoAndroidForceRecreateSwapchain()
	}
	service.VKVideoAndroidUpdateRect(int(x*scale), int(y*scale), pw, ph)
	vw.updateNativeViewportAndCursor()
}

func (vw *VideoWidget) metalVideoEnterFullscreen(_ fyne.Window) {
	// Vulkan stays VISIBLE in fullscreen — videoCanvasFrame() detects IsFullscreen()
	// and returns the full-canvas rect so the SurfaceView expands to fill the screen.
	// Force swapchain recreation so the render thread picks up the new surface
	// dimensions immediately (SetFullScreen may have resized the SurfaceView surface).
	vw.forceCanvasRefresh.Store(true)
	service.VKVideoAndroidForceRecreateSwapchain()
}

func (vw *VideoWidget) metalVideoExitFullscreen() {
	// Restore normal rect — videoCanvasFrame() will return to the video-area rect.
	// Force swapchain recreation so the render thread picks up the restored size.
	vw.forceCanvasRefresh.Store(true)
	service.VKVideoAndroidForceRecreateSwapchain()
}

// updateNativeViewportAndCursor forwards the current Go viewport state (zoom/pan)
// to the Vulkan renderer and updates the virtual cursor if in cursor mode.
func (vw *VideoWidget) updateNativeViewportAndCursor() {
	if !service.VKVideoAndroidIsActive() {
		return
	}
	// Compute visible UV rect from Go viewport state.
	cw, ch := vw.contentRectW, vw.contentRectH
	if cw <= 0 || ch <= 0 {
		service.VKVideoAndroidSetViewport(0, 0, 1, 1)
	} else {
		u0 := clampFloat(-vw.contentRectX/cw, 0, 1)
		v0 := clampFloat(-vw.contentRectY/ch, 0, 1)
		u1 := clampFloat((vw.touchpadSizeW-vw.contentRectX)/cw, 0, 1)
		v1 := clampFloat((vw.touchpadSizeH-vw.contentRectY)/ch, 0, 1)
		service.VKVideoAndroidSetViewport(u0, v0, u1, v1)
	}

	if vw.GetMouseInputMode() == mouseModeVirtualCursor {
		service.VKVideoAndroidSetCursor(vw.virtualCursorU, vw.virtualCursorV, true)
	} else {
		service.VKVideoAndroidSetCursor(0, 0, false)
	}
}

// centerViewportOnVirtualCursor pans the viewport so the virtual cursor is
// centred on screen (RustDesk-style follow).  Only effective when zoom > 1.
func (vw *VideoWidget) centerViewportOnVirtualCursor() {
	if vw.zoomScale <= 1.001 {
		return
	}
	cw := vw.baseContentRectW * vw.zoomScale
	ch := vw.baseContentRectH * vw.zoomScale
	// panOffsetX = cw*(0.5 - u) so cursor lands at touchpadSizeW/2
	vw.panOffsetX = cw * (0.5 - vw.virtualCursorU)
	// panOffsetY: with overflow ch > availH, baseY = availH-ch
	availH := vw.touchpadSizeH - vw.bottomInset
	if ch > availH {
		// panOffsetY = availH/2 - (availH-ch) - vc*ch
		vw.panOffsetY = availH/2 - (availH - ch) - vw.virtualCursorV*ch
		if vw.panOffsetY < 0 {
			vw.panOffsetY = 0
		}
	}
	vw.recalculateViewport()
}

// initAndroidCursorScale reinitialises the Vulkan cursor pixel buffer at `scale`.
func (vw *VideoWidget) initAndroidCursorScale(scale int) {
	if service.VKVideoAndroidIsActive() {
		service.VKVideoAndroidSetCursorScale(scale)
	}
}

// androidCursorScale returns the integer cursor scale factor for the current
// display density (1-4×).
func (vw *VideoWidget) androidCursorScale() int {
	if vw.parentWindow == nil {
		return 2
	}
	scale := vw.parentWindow.Canvas().Scale()
	s := int(math.Round(float64(scale)))
	if s < 1 {
		s = 1
	}
	if s > 4 {
		s = 4
	}
	return s
}

// videoCanvasFrame returns the Vulkan SurfaceView rect in window-local dp coords.
//   - Fullscreen: full canvas (Vulkan expands to fill the screen).
//   - Keyboard visible: video area above the keyboard panel.
//   - Normal: full container area below the tab bar.
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.parentWindow == nil {
		return
	}
	cs := vw.parentWindow.Canvas().Size()

	// In fullscreen mode the Vulkan SurfaceView covers the whole screen.
	if vw.fullscreenDialog != nil && vw.fullscreenDialog.IsFullscreen() {
		return 0, 0, cs.Width, cs.Height
	}

	if vw.container == nil {
		return
	}
	sz := vw.container.Size()
	topOffset := cs.Height - sz.Height

	videoH := sz.Height
	if vw.contentContainer != nil && vw.contentContainer.Visible() {
		if kh := vw.contentContainer.Size().Height; kh > 0 {
			videoH -= kh
		}
	}
	return 0, topOffset, sz.Width, videoH
}
