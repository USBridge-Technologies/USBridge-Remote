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
		logrus.Warn("[GL/Win] window does not implement driver.NativeWindow — GL skipped")
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
			logrus.Warnf("[GL/Win] unexpected native context type %T", ctx)
			return
		}
		var px, py, pw, ph int
		if !fullscreen {
			x, y, w, h := vw.videoCanvasFrame()
			if w <= 0 || h <= 0 {
				logrus.Warn("[GL/Win] videoCanvas has zero size — GL skipped")
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
		// pw=0,ph=0 means full client area in C code.
		if !service.GLVideoCreate(hwnd, px, py, pw, ph, vw.enableVSync) {
			logrus.Warn("[GL/Win] failed to create overlay — Fyne canvas path active")
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
