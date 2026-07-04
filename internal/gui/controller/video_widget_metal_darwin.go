//go:build darwin && !ios

package controller

import (
	"image"
	"sync/atomic"
	"time"

	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/sirupsen/logrus"
)

// ── Metal mouse event forwarding ──────────────────────────────────────────────
//
// USBridgeMetalView (ObjC) captures all pointer events from AppKit and queues
// them. This goroutine polls metal_video_next_event at ~250 Hz and dispatches
// to TouchpadWrapper via fyne.Do — same pattern as Linux X11 / Windows Vulkan.

var metalMouseQuit         chan struct{}
var metalFullscreenWindow  fyne.Window
var metalMouseCheckPending int32 // atomic

func (vw *VideoWidget) startMetalMouseForwarding() {
	vw.stopMetalMouseForwarding()
	quit := make(chan struct{})
	metalMouseQuit = quit
	logrus.Info("[Metal/Mac] mouse forwarding started")
	go func() {
		ticker := time.NewTicker(4 * time.Millisecond) // ~250 Hz
		defer ticker.Stop()
		for {
			select {
			case <-quit:
				return
			case <-ticker.C:
				for {
					typ, btn, x, y, ok := service.MetalVideoNextEvent()
					if !ok {
						break
					}
					evTyp, evBtn, evX, evY := typ, btn, x, y
					fyne.Do(func() {
						if !service.MetalVideoIsActive() {
							return
						}
						vw.dispatchMetalMouseEvent(evTyp, evX, evY, evBtn)
					})
				}
			}
		}
	}()
}

func (vw *VideoWidget) stopMetalMouseForwarding() {
	if metalMouseQuit != nil {
		close(metalMouseQuit)
		metalMouseQuit = nil
		logrus.Info("[Metal/Mac] mouse forwarding stopped")
	}
}

func (vw *VideoWidget) dispatchMetalMouseEvent(typ int, x, y float32, button int) {
	var tw *TouchpadWrapper
	if metalFullscreenWindow != nil && vw.fullscreenDialog != nil {
		tw = vw.fullscreenDialog.touchpadWrapper
	} else {
		tw = vw.touchpadWrapper
	}
	if tw == nil {
		return
	}
	if !vw.isMouseConnected {
		if atomic.CompareAndSwapInt32(&metalMouseCheckPending, 0, 1) {
			go func() {
				vw.checkMouseConnected()
				atomic.StoreInt32(&metalMouseCheckPending, 0)
			}()
		}
		return
	}
	pos := fyne.NewPos(x, y)
	switch typ {
	case 1: // move
		tw.MouseMoved(&desktop.MouseEvent{
			PointEvent: fyne.PointEvent{Position: pos},
		})
	case 2: // press
		switch button {
		case 4: // wheel up
			tw.Scrolled(&fyne.ScrollEvent{
				PointEvent: fyne.PointEvent{Position: pos},
				Scrolled:   fyne.Delta{DX: 0, DY: 10},
			})
		case 5: // wheel down
			tw.Scrolled(&fyne.ScrollEvent{
				PointEvent: fyne.PointEvent{Position: pos},
				Scrolled:   fyne.Delta{DX: 0, DY: -10},
			})
		default:
			tw.MouseDown(&desktop.MouseEvent{
				PointEvent: fyne.PointEvent{Position: pos},
				Button:     metalButtonToFyne(button),
			})
		}
	case 3: // release
		if button != 4 && button != 5 {
			tw.MouseUp(&desktop.MouseEvent{
				PointEvent: fyne.PointEvent{Position: pos},
				Button:     metalButtonToFyne(button),
			})
		}
	}
}

func metalButtonToFyne(b int) desktop.MouseButton {
	switch b {
	case 1:
		return desktop.MouseButtonPrimary
	case 2:
		return desktop.MouseButtonTertiary
	case 3:
		return desktop.MouseButtonSecondary
	default:
		return desktop.MouseButtonPrimary
	}
}

// startMetalVideoOnWindow creates (or replaces) the Metal overlay on window.
// Set fullscreen=true to cover the entire contentView; otherwise the overlay
// is positioned to match the Fyne videoCanvas widget bounds.
func (vw *VideoWidget) startMetalVideoOnWindow(window fyne.Window, fullscreen bool) {
	// Wire up overlay lifecycle hooks so that every Fyne popup/menu temporarily
	// hides the Metal NSView, letting Fyne render popups on top of the video.
	// These are idempotent no-ops when Metal is not active.
	view.OnOverlayShow = func() { service.MetalVideoSetHidden(true) }
	view.OnOverlayHide = func() { service.MetalVideoSetHidden(false) }
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
			// If a Fyne overlay (popup/menu) is already visible when Metal starts,
			// hide the Metal view immediately so the UI stays on top.
			if view.OverlayActive() {
				service.MetalVideoSetHidden(true)
			}
			// Clear the static Fyne canvas frame — Metal overlay now handles rendering.
			// Also clear Translucency so the darkened pause frame doesn't bleed through.
			// Running on main thread already (RunNative context).
			if vw.videoCanvas != nil {
				vw.videoCanvas.Image = nil
				vw.videoCanvas.Translucency = 0
				vw.videoCanvas.Refresh()
			}
			// Fire the one-shot ready callback (e.g. fullscreen dialog clears its canvas).
			if cb := vw.onNativeReady; cb != nil {
				vw.onNativeReady = nil
				cb()
			}
			// Forward mouse events from the Metal NSView to TouchpadWrapper.
			// USBridgeMetalView captures all AppKit pointer events so they don't
			// reach Fyne's GLFW view — we re-dispatch them manually via fyne.Do.
			vw.startMetalMouseForwarding()
		}
	})
}

// stopMetalVideo destroys the Metal overlay and re-enables the Fyne canvas path.
func (vw *VideoWidget) stopMetalVideo() {
	vw.onNativeReady = nil // discard any pending fullscreen-ready callback
	vw.stopMetalMouseForwarding()
	service.MetalVideoDestroy()
	vw.metalFPSWarned.Store(false)
}

var (
	lastMetalFrameX, lastMetalFrameY float32
	lastMetalFrameW, lastMetalFrameH float32
)

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
	if x != lastMetalFrameX || y != lastMetalFrameY || w != lastMetalFrameW || h != lastMetalFrameH {
		lastMetalFrameX, lastMetalFrameY, lastMetalFrameW, lastMetalFrameH = x, y, w, h
		service.MetalVideoUpdateFrame(x, y, w, h)
	}

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
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.container == nil || vw.touchpadWrapper == nil || vw.parentWindow == nil {
		return
	}
	
	// Fyne's AbsolutePositionForObject is unreliable on mobile canvases, 
	// and we match the robust iOS math here for consistency.
	szMain := vw.container.Size()
	canvasH := vw.parentWindow.Canvas().Size().Height
	topOffset := canvasH - szMain.Height
	
	szVideo := vw.touchpadWrapper.Size()
	return 0, topOffset, szVideo.Width, szVideo.Height
}

// metalVideoEnterFullscreen tears down the main-window overlay and creates a
// new full-window overlay on the fullscreen window.
// Called by FullscreenDialog.enterFullscreen after the fullscreen window is shown.
func (vw *VideoWidget) metalVideoEnterFullscreen(fsWindow fyne.Window) {
	if fsWindow == nil {
		return
	}
	metalFullscreenWindow = fsWindow
	vw.stopMetalMouseForwarding()
	service.MetalVideoDestroy() // release main-window overlay
	vw.startMetalVideoOnWindow(fsWindow, true)
}

// metalVideoExitFullscreen tears down the fullscreen overlay and restores the
// main-window overlay at the video widget's current bounds.
// Called by FullscreenDialog.exitFullscreen before the fullscreen window closes.
func (vw *VideoWidget) metalVideoExitFullscreen() {
	metalFullscreenWindow = nil
	vw.stopMetalMouseForwarding()
	service.MetalVideoDestroy() // release fullscreen overlay
	vw.startMetalVideoOnWindow(vw.parentWindow, false)
}

func (vw *VideoWidget) isNativeVideoActive() bool {
	return service.MetalVideoIsActive()
}

// getNativeFPS returns the current Metal render FPS for the status/icon counter.
func (vw *VideoWidget) getNativeFPS() float64 {
	return service.MetalVideoLastFPS()
}

// getMetalLastFrame captures the last VT-decoded frame from the Metal overlay.
// Called by clearVideo() before stopMetalVideo() so the pause display has something to show.
func (vw *VideoWidget) getMetalLastFrame() *image.RGBA {
	return service.MetalVideoGetLastFrameRGBA()
}

// ensureNativeOverlayOnTop is a no-op on macOS (Metal uses CALayer, not a separate window).
func (vw *VideoWidget) ensureNativeOverlayOnTop() {}
