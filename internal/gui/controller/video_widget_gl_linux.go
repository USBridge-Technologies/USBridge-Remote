//go:build linux && !android

package controller

import (
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

func (vw *VideoWidget) isNativeVideoActive() bool {
	return service.GLVideoIsActive()
}

func (vw *VideoWidget) startMetalVideoOnWindow(window fyne.Window, fullscreen bool) {
	if window == nil {
		return
	}
	nw, ok := window.(driver.NativeWindow)
	if !ok {
		logrus.Warn("[GL/Linux] window does not implement driver.NativeWindow — GL skipped")
		return
	}
	nw.RunNative(func(ctx any) {
		var xwin uintptr
		switch c := ctx.(type) {
		case *driver.X11WindowContext:
			xwin = c.WindowHandle
		case *driver.WaylandWindowContext:
			// Wayland wl_subsurface support not yet implemented; fall back.
			logrus.Info("[GL/Linux] Wayland detected — native GL overlay not yet supported, using Fyne canvas")
			return
		default:
			logrus.Warnf("[GL/Linux] unexpected native context type %T — GL skipped", ctx)
			return
		}

		var px, py, pw, ph int
		if !fullscreen {
			x, y, w, h := vw.videoCanvasFrame()
			if w <= 0 || h <= 0 {
				logrus.Warn("[GL/Linux] videoCanvas has zero size — GL skipped")
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
		if !service.GLVideoCreate(xwin, px, py, pw, ph, vw.enableVSync) {
			logrus.Warn("[GL/Linux] failed to create overlay — Fyne canvas path active")
		} else {
			if cb := vw.onNativeReady; cb != nil {
				vw.onNativeReady = nil
				cb()
			}
		}
	})
}

func (vw *VideoWidget) stopMetalVideo() {
	vw.onNativeReady = nil
	service.GLVideoDestroy()
}

func (vw *VideoWidget) updateMetalVideoFrame() {
	if !service.GLVideoIsActive() {
		return
	}

	// Read and log stats from the render thread (safe: runs on Fyne main goroutine).
	st := service.GLVideoGetStats()
	if st.FirstFrame || st.FPSReady {
		service.GLVideoClearPendingStats()
		if st.FirstFrame {
			logrus.Infof("[GL/Linux] first frame rendered — %dx%d", st.FW, st.FH)
		}
		if st.FPSReady {
			logrus.Infof("[GL/Linux] fps=%.1f  rendered=%d  submitted=%d  size=%dx%d",
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

func (vw *VideoWidget) metalVideoEnterFullscreen(fsWindow fyne.Window) {
	if fsWindow == nil {
		return
	}
	service.GLVideoDestroy()
	vw.startMetalVideoOnWindow(fsWindow, true)
}

func (vw *VideoWidget) metalVideoExitFullscreen() {
	service.GLVideoDestroy()
	vw.startMetalVideoOnWindow(vw.parentWindow, false)
}

func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.videoCanvas == nil {
		return
	}
	pos := vw.videoCanvas.Position()
	sz := vw.videoCanvas.Size()
	return pos.X, pos.Y, sz.Width, sz.Height
}
