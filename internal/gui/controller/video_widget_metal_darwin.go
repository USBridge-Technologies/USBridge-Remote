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
		// Fyne passes MacWindowContext as a value (not pointer) in recent versions.
		// Support both forms so we don't silently skip Metal on version changes.
		var nsWin uintptr
		switch m := ctx.(type) {
		case driver.MacWindowContext:
			nsWin = m.NSWindow
		case *driver.MacWindowContext:
			nsWin = m.NSWindow
		default:
			logrus.Warnf("🍎 [Metal] RunNative ctx type=%T — Metal skipped", ctx)
			return
		}
		if nsWin == 0 {
			logrus.Warn("🍎 [Metal] NSWindow pointer is nil — Metal skipped")
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
		if !service.MetalVideoCreate(nsWin, x, y, w, h) {
			logrus.Warn("🍎 [Metal] failed to create overlay — Fyne canvas path active")
		} else {
			logrus.Infof("🍎 [Metal] overlay active (fullscreen=%v)", fullscreen)
			// Clear the static Fyne canvas frame — Metal overlay now handles rendering.
			// Running on main thread already (RunNative context).
			if vw.videoCanvas != nil {
				vw.videoCanvas.Image = nil
				vw.videoCanvas.Refresh()
			}
		}
	})
}

// stopMetalVideo destroys the Metal overlay and re-enables the Fyne canvas path.
func (vw *VideoWidget) stopMetalVideo() {
	service.MetalVideoDestroy()
	vw.metalFPSWarned.Store(false)
}

// updateMetalVideoFrame repositions the Metal overlay to track videoCanvas.
// Called from updateStats() at 1 Hz to follow window resizes.
// Also emits a one-shot FPS mismatch warning when Metal FPS < 75% of configured.
func (vw *VideoWidget) updateMetalVideoFrame() {
	if !service.MetalVideoIsActive() {
		return
	}
	x, y, w, h := vw.videoCanvasFrame()
	if w <= 0 || h <= 0 {
		return
	}
	service.MetalVideoUpdateFrame(x, y, w, h)

	if vw.metalFPSWarned.Load() || vw.videoClient == nil {
		return
	}
	actualFPS := service.MetalVideoLastFPS()
	if actualFPS < 5 {
		return // not enough data yet
	}
	cfg := vw.videoClient.GetConfig()
	if cfg == nil || cfg.VideoFPS <= 0 {
		return
	}
	vw.metalFPSWarned.Store(true)
	if actualFPS >= float64(cfg.VideoFPS)*0.75 {
		logrus.Infof("✅ [FPS] Metal=%.0ffps configured=%dfps — OK", actualFPS, cfg.VideoFPS)
		return
	}
	logrus.Warnf(
		"⚠️ [FPS] Metal renders=%.0ffps but configured=%dfps. "+
			"Pipeline: Sunshine encoder → network → VT decode → Metal render. "+
			"VT decode also shows ~%.0ffps — source sends %.0ffps. "+
			"Most likely cause: V4L2 capture device on RPi hardware-capped at 30fps. "+
			"Fix: set FPS=30 in UI to match the actual source capability.",
		actualFPS, cfg.VideoFPS, actualFPS, actualFPS,
	)
}

// videoCanvasFrame returns the video widget's bounds in window-local dp coordinates
// (top-left origin, same as macOS points).
//
// Fyne positions are relative to parent, so vw.container.Position() is always (0,0)
// within the tab content — it does not reflect the window-absolute offset.
// We derive the y-offset indirectly: the video container fills everything below
// the address bar + tab bar, so  y = canvasHeight − containerHeight.
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.container == nil || vw.parentWindow == nil {
		return
	}
	sz := vw.container.Size()
	canvasH := vw.parentWindow.Canvas().Size().Height
	// Everything above the video area (address bar + tab bar) = canvasH - sz.Height
	topOffset := canvasH - sz.Height
	return 0, topOffset, sz.Width, sz.Height
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
