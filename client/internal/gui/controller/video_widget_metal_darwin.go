//go:build darwin && !ios

package controller

import (
	"C"
	"image"
	"sync"
	"sync/atomic"

	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/sirupsen/logrus"
)

// ── Metal mouse event forwarding ──────────────────────────────────────────────
//
// USBridgeMetalView (ObjC) captures all pointer events from AppKit and pushes
// each one straight into the goMetalMouseEvent CGO export as it happens (no
// polling tick, unlike the Linux X11 / Windows Vulkan overlays). Events are
// coalesced into metalMousePending and dispatched to TouchpadWrapper via a
// single fyne.Do per animation-loop turn — see dispatchMetalMouseBatch.

var (
	metalFullscreenWindow            fyne.Window
	metalMouseCheckPending           int32 // atomic
	lastMetalFrameMu                 sync.Mutex
	lastMetalFrameX, lastMetalFrameY float32
	lastMetalFrameW, lastMetalFrameH float32

	// activeVideoWidget is used by the CGO callback to route events.
	// We only ever have one active Metal overlay at a time.
	activeVideoWidget atomic.Pointer[VideoWidget]
)

type metalMouseEv struct {
	typ, btn int
	x, y     float32
}

// metalMouseDoPending/metalMousePending coalesce bursts of AppKit mouseMoved:
// events into a single fyne.Do dispatch instead of one per sample. Unlike
// Linux/Windows, USBridgeMetalView pushes straight into goMetalMouseEvent
// per-event (no polling tick to batch against), so a fast mouse can call
// this CGO export many times within a single Fyne animation-loop turn; the
// old code fired one fyne.Do per call, serializing that whole burst onto
// the same goroutine that drives Metal frame presentation and causing the
// video to visibly stutter only while the mouse was moving. Mirrors the
// batching fix applied to video_widget_gl_linux.go / video_widget_windows.go.
var (
	metalMouseDoPending int32
	metalMousePendingMu sync.Mutex
	metalMousePending   []metalMouseEv
)

//export goMetalMouseEvent
func goMetalMouseEvent(typ C.int, x C.float, y C.float, btn C.int) {
	ev := metalMouseEv{typ: int(typ), btn: int(btn), x: float32(x), y: float32(y)}

	metalMousePendingMu.Lock()
	// Coalesce consecutive moves so a fast mouse does not enqueue one
	// fyne.Do per sample.
	if ev.typ == 1 && len(metalMousePending) > 0 && metalMousePending[len(metalMousePending)-1].typ == 1 {
		metalMousePending[len(metalMousePending)-1] = ev
	} else {
		metalMousePending = append(metalMousePending, ev)
	}
	metalMousePendingMu.Unlock()

	dispatchMetalMouseBatch()
}

// dispatchMetalMouseBatch claims the single in-flight fyne.Do slot (if free)
// and drains metalMousePending on the Fyne main goroutine. If more events
// arrive while that dispatch is running, it re-claims the slot itself once
// done so nothing is dropped.
func dispatchMetalMouseBatch() {
	if !atomic.CompareAndSwapInt32(&metalMouseDoPending, 0, 1) {
		return
	}
	fyne.Do(func() {
		metalMousePendingMu.Lock()
		evs := metalMousePending
		metalMousePending = nil
		metalMousePendingMu.Unlock()
		if vw := activeVideoWidget.Load(); vw != nil && service.MetalVideoIsActive() {
			for _, ev := range evs {
				vw.dispatchMetalMouseEvent(ev.typ, ev.x, ev.y, ev.btn)
			}
		}
		atomic.StoreInt32(&metalMouseDoPending, 0)
		metalMousePendingMu.Lock()
		more := len(metalMousePending) > 0
		metalMousePendingMu.Unlock()
		if more {
			dispatchMetalMouseBatch()
		}
	})
}

func (vw *VideoWidget) startMetalMouseForwarding() {
	activeVideoWidget.Store(vw)
	logrus.Info("[Metal/Mac] mouse forwarding started via CGO callback")
}

func (vw *VideoWidget) stopMetalMouseForwarding() {
	activeVideoWidget.Store(nil)
	metalMousePendingMu.Lock()
	metalMousePending = nil
	metalMousePendingMu.Unlock()
	logrus.Info("[Metal/Mac] mouse forwarding stopped")
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
	lastMetalFrameMu.Lock()
	changed := (x != lastMetalFrameX || y != lastMetalFrameY || w != lastMetalFrameW || h != lastMetalFrameH)
	if changed {
		lastMetalFrameX, lastMetalFrameY, lastMetalFrameW, lastMetalFrameH = x, y, w, h
	}
	lastMetalFrameMu.Unlock()

	if changed {
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

	// Desktop: AbsolutePositionForObject is the canvas origin of the
	// container. canvasH − height assumed the video was flush with the
	// window bottom and shifted the overlay once Control grew a footer.
	pos := vw.videoContainerOrigin()
	szVideo := vw.touchpadWrapper.Size()
	return pos.X, pos.Y, szVideo.Width, szVideo.Height
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
