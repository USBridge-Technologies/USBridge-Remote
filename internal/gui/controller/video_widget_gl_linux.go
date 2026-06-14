//go:build linux && !android

package controller

import (
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

func vkStageName(s int) string {
	names := []string{"idle", "got-frame", "staging", "acquire", "fence-wait", "queue-submit", "present", "recreate-swapchain"}
	if s >= 0 && s < len(names) {
		return names[s]
	}
	return "unknown"
}

func (vw *VideoWidget) isNativeVideoActive() bool {
	return service.VKVideoIsActive() || service.GLVideoIsActive()
}

// startMetalVideoOnWindow creates the Vulkan overlay on the X11 window.
// Falls back to GLX overlay if Vulkan is unavailable.
func (vw *VideoWidget) startMetalVideoOnWindow(window fyne.Window, fullscreen bool) {
	// Wire hide/show hooks for Fyne menus — mirrors macOS Metal / Windows Vulkan pattern.
	view.OnOverlayShow = func() { service.VKVideoSetHidden(true) }
	view.OnOverlayHide = func() { service.VKVideoSetHidden(false) }

	if window == nil {
		return
	}
	nw, ok := window.(driver.NativeWindow)
	if !ok {
		logrus.Warn("[VK/Linux] window does not implement NativeWindow — using Fyne canvas")
		return
	}
	nw.RunNative(func(ctx any) {
		var xwin uintptr
		switch c := ctx.(type) {
		case *driver.X11WindowContext:
			xwin = c.WindowHandle
		case driver.X11WindowContext:
			xwin = c.WindowHandle
		case *driver.WaylandWindowContext:
			logrus.Info("[VK/Linux] Wayland detected — Vulkan/Xlib overlay not supported, using Fyne canvas")
			return
		case driver.WaylandWindowContext:
			logrus.Info("[VK/Linux] Wayland detected — Vulkan/Xlib overlay not supported, using Fyne canvas")
			return
		default:
			logrus.Warnf("[VK/Linux] unexpected native context type %T — skipped", ctx)
			return
		}

		var px, py, pw, ph int
		if !fullscreen {
			x, y, w, h := vw.videoCanvasFrame()
			if w <= 0 || h <= 0 {
				logrus.Warn("[VK/Linux] videoCanvas has zero size — skipped")
				return
			}
			scale := float32(1)
			if window.Canvas() != nil {
				scale = window.Canvas().Scale()
			}
			px = int(x * scale)
			py = int(y * scale)
			pw = int(w * scale)
			ph = int(h * scale)
		}

		// Try Vulkan first; fall back to GLX if unavailable.
		service.VKVideoResetLastFrame()
		if service.VKVideoCreate(xwin, px, py, pw, ph) {
			logrus.Infof("[VK/Linux] overlay active (fullscreen=%v) rect=(%d,%d,%dx%d)",
				fullscreen, px, py, pw, ph)
			if view.OverlayActive() {
				service.VKVideoSetHidden(true)
			}
		} else {
			logrus.Warn("[VK/Linux] Vulkan init failed — trying GLX fallback")
			view.OnOverlayShow = nil
			view.OnOverlayHide = nil
			if !service.GLVideoCreate(xwin, px, py, pw, ph, vw.enableVSync) {
				logrus.Warn("[GLX/Linux] GLX overlay also failed — using Fyne canvas")
				return
			}
			logrus.Infof("[GLX/Linux] overlay active (fullscreen=%v) rect=(%d,%d,%dx%d)",
				fullscreen, px, py, pw, ph)
		}

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

func (vw *VideoWidget) stopMetalVideo() {
	vw.onNativeReady = nil
	view.OnOverlayShow = nil
	view.OnOverlayHide = nil
	service.VKVideoDestroy()
	service.GLVideoDestroy()
}

func (vw *VideoWidget) updateMetalVideoFrame() {
	if service.VKVideoIsActive() {
		st := service.VKVideoGetStats()

		// Watchdog: render-thread heartbeat stuck.
		// (Simplified version — full watchdog like Windows can be added here if needed.)
		if st.FirstFrame || st.FPSReady {
			service.VKVideoClearPendingStats()
			if st.FirstFrame {
				logrus.Infof("[VK/Linux] first frame — %dx%d", st.FW, st.FH)
			}
			if st.FPSReady {
				logrus.Infof("[VK/Linux] fps=%.1f rendered=%d submitted=%d size=%dx%d",
					st.FPS, st.Rendered, st.Submitted, st.FW, st.FH)
			}
		}

		x, y, w, h := vw.videoCanvasFrame()
		if w <= 0 || h <= 0 {
			return
		}
		scale := float32(1)
		if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
			scale = vw.parentWindow.Canvas().Scale()
		}
		service.VKVideoUpdateFrame(int(x*scale), int(y*scale), int(w*scale), int(h*scale))
		return
	}

	if service.GLVideoIsActive() {
		st := service.GLVideoGetStats()
		if st.FirstFrame || st.FPSReady {
			service.GLVideoClearPendingStats()
			if st.FirstFrame {
				logrus.Infof("[GLX/Linux] first frame — %dx%d", st.FW, st.FH)
			}
			if st.FPSReady {
				logrus.Infof("[GLX/Linux] fps=%.1f rendered=%d submitted=%d size=%dx%d",
					st.FPS, st.Rendered, st.Submitted, st.FW, st.FH)
			}
		}
		x, y, w, h := vw.videoCanvasFrame()
		if w <= 0 || h <= 0 {
			return
		}
		scale := float32(1)
		if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
			scale = vw.parentWindow.Canvas().Scale()
		}
		service.GLVideoUpdateFrame(int(x*scale), int(y*scale), int(w*scale), int(h*scale))
	}
}

func (vw *VideoWidget) metalVideoEnterFullscreen(fsWindow fyne.Window) {
	if fsWindow == nil {
		return
	}
	service.VKVideoDestroy()
	service.GLVideoDestroy()
	vw.startMetalVideoOnWindow(fsWindow, true)
}

func (vw *VideoWidget) metalVideoExitFullscreen() {
	service.VKVideoDestroy()
	service.GLVideoDestroy()
	vw.startMetalVideoOnWindow(vw.parentWindow, false)
}

// videoCanvasFrame returns the video area rect in window-local dp coordinates.
// vw.videoCanvas.Position() is always near (0,0) within its parent container,
// so we derive the y-offset the same way as on Windows/macOS: the video container
// fills everything below the toolbar, so y = canvasHeight - containerHeight.
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.container == nil || vw.parentWindow == nil {
		return
	}
	sz := vw.container.Size()
	canvasH := vw.parentWindow.Canvas().Size().Height
	topOffset := canvasH - sz.Height
	return 0, topOffset, sz.Width, sz.Height
}
