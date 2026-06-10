//go:build darwin

package controller

import (
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

// startMetalVideoOnWindow creates (or replaces) the Metal overlay on window.
// Set fullscreen=true to cover the entire contentView; otherwise the overlay
// is positioned to match the Fyne videoCanvas widget bounds.
func (vw *VideoWidget) startMetalVideoOnWindow(window fyne.Window, fullscreen bool) {
	if window == nil {
		logrus.Warn("🍎 [Metal] startMetalVideoOnWindow: window=nil — skipped")
		return
	}
	nw, ok := window.(driver.NativeWindow)
	if !ok {
		logrus.Warnf("🍎 [Metal] window type %T does not implement driver.NativeWindow — Metal skipped", window)
		return
	}
	logrus.Infof("🍎 [Metal] RunNative starting (fullscreen=%v)", fullscreen)
	nw.RunNative(func(ctx any) {
		mac, ok := ctx.(*driver.MacWindowContext)
		if !ok {
			logrus.Warnf("🍎 [Metal] RunNative ctx type=%T, expected *driver.MacWindowContext — Metal skipped", ctx)
			return
		}
		var x, y, w, h float32
		if !fullscreen {
			x, y, w, h = vw.videoCanvasFrame()
			logrus.Infof("🍎 [Metal] videoCanvas frame: x=%.0f y=%.0f w=%.0f h=%.0f", x, y, w, h)
			if w <= 0 || h <= 0 {
				logrus.Warn("🍎 [Metal] videoCanvas has zero size — Metal skipped")
				return
			}
		}
		// w=0,h=0 signals full-window mode in C code.
		if !service.MetalVideoCreate(mac.NSWindow, x, y, w, h) {
			logrus.Warn("🍎 [Metal] failed to create overlay — Fyne canvas path active")
		} else {
			logrus.Infof("🍎 [Metal] overlay active (fullscreen=%v)", fullscreen)
		}
	})
}

// stopMetalVideo destroys the Metal overlay and re-enables the Fyne canvas path.
func (vw *VideoWidget) stopMetalVideo() {
	service.MetalVideoDestroy()
}

// updateMetalVideoFrame repositions the Metal overlay to track videoCanvas.
// Called from updateStats() at 1 Hz to follow window resizes.
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

// videoCanvasFrame returns the Fyne videoCanvas widget bounds in window-local
// dp coordinates (top-left origin, same as macOS points).
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.videoCanvas == nil {
		return
	}
	pos := vw.videoCanvas.Position()
	sz := vw.videoCanvas.Size()
	return pos.X, pos.Y, sz.Width, sz.Height
}

// metalVideoEnterFullscreen tears down the main-window overlay and creates a
// new full-window overlay on the fullscreen window.
// Called by FullscreenDialog.enterFullscreen after the fullscreen window is shown.
func (vw *VideoWidget) metalVideoEnterFullscreen(fsWindow fyne.Window) {
	if fsWindow == nil {
		return
	}
	service.MetalVideoDestroy() // release main-window overlay
	vw.startMetalVideoOnWindow(fsWindow, true)
}

// metalVideoExitFullscreen tears down the fullscreen overlay and restores the
// main-window overlay at the video widget's current bounds.
// Called by FullscreenDialog.exitFullscreen before the fullscreen window closes.
func (vw *VideoWidget) metalVideoExitFullscreen() {
	service.MetalVideoDestroy() // release fullscreen overlay
	vw.startMetalVideoOnWindow(vw.parentWindow, false)
}

func (vw *VideoWidget) isNativeVideoActive() bool {
	return service.MetalVideoIsActive()
}
