//go:build windows

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
		logrus.Warn("[GDI/Win] window does not implement driver.NativeWindow — overlay skipped")
		return
	}
	nw.RunNative(func(ctx any) {
		var hwnd uintptr
		switch c := ctx.(type) {
		case *driver.WindowsWindowContext:
			hwnd = c.HWND
		case driver.WindowsWindowContext:
			hwnd = c.HWND
		default:
			logrus.Warnf("[GDI/Win] unexpected native context type %T", ctx)
			return
		}
		var px, py, pw, ph int
		if !fullscreen {
			x, y, w, h := vw.videoCanvasFrame()
			if w <= 0 || h <= 0 {
				logrus.Warn("[GDI/Win] videoCanvas has zero size — overlay skipped")
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
		// Reset cached geometry so the first GLVideoUpdateFrame call after creation
		// unconditionally repositions (in case position changed since creation).
		service.GLVideoResetLastFrame()
		if !service.GLVideoCreate(hwnd, px, py, pw, ph, vw.enableVSync) {
			logrus.Warn("[GDI/Win] failed to create overlay — Fyne canvas path active")
			return
		}
		// GDI child window now owns the video area. Clear the Fyne canvas so Fyne
		// does not repaint the stale first-frame image underneath the overlay,
		// which would produce a "double image" artefact.
		if vw.videoCanvas != nil {
			vw.videoCanvas.Image = nil
			vw.videoCanvas.Resource = nil
			vw.videoCanvas.Refresh()
		}
	})
}

func (vw *VideoWidget) stopMetalVideo() {
	service.GLVideoDestroy()
}

func (vw *VideoWidget) updateMetalVideoFrame() {
	if !service.GLVideoIsActive() {
		return
	}

	// Read and log any stats accumulated by the render thread.
	// This runs on the Fyne main goroutine (safe CGO context) once per second.
	st := service.GLVideoGetStats()
	if st.FirstFrame || st.FPSReady {
		service.GLVideoClearPendingStats()
		if st.FirstFrame {
			logrus.Infof("[GDI/Win] first frame rendered — %dx%d", st.FW, st.FH)
		}
		if st.FPSReady {
			logrus.Infof("[GDI/Win] fps=%.1f  rendered=%d  submitted=%d  size=%dx%d",
				st.FPS, st.Rendered, st.Submitted, st.FW, st.FH)
		}
	}

	// Reposition the overlay to follow window resizes.
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
