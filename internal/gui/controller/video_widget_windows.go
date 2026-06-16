//go:build windows

package controller

import (
	"image"
	"sync"
	"sync/atomic"
	"time"

	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/sirupsen/logrus"
)

// ── Vulkan mouse event forwarding ────────────────────────────────────────────
//
// The Vulkan overlay window (WS_POPUP, no WS_EX_TRANSPARENT) captures all
// pointer events so they never reach Fyne's GLFW window while the overlay is
// visible. We compensate by polling vk_video_next_event and dispatching
// directly to TouchpadWrapper on the Fyne main goroutine — same pattern as
// the Linux X11 implementation (video_widget_gl_linux.go).

var vkWinMouseQuit     chan struct{}
var vkWinFullscreenWin fyne.Window
var vkWinMouseCheckPending int32 // atomic

func (vw *VideoWidget) startVKMouseForwarding(scale float32) {
	vw.stopVKMouseForwarding()
	quit := make(chan struct{})
	vkWinMouseQuit = quit
	logrus.Info("[VK/Win] mouse forwarding started")
	go func() {
		ticker := time.NewTicker(4 * time.Millisecond) // ~250 Hz poll
		defer ticker.Stop()
		for {
			select {
			case <-quit:
				return
			case <-ticker.C:
				for {
					typ, ex, ey, btn, ok := service.VKVideoNextEvent()
					if !ok {
						break
					}
					evTyp, evX, evY, evBtn := typ, ex, ey, btn
					fyne.Do(func() {
						if !service.VKVideoIsActive() {
							return
						}
						s := scale
						if fsWin := vkWinFullscreenWin; fsWin != nil {
							if fsWin.Canvas() != nil {
								s = fsWin.Canvas().Scale()
							}
						} else if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
							s = vw.parentWindow.Canvas().Scale()
						}
						x := float32(evX) / s
						y := float32(evY) / s
						vw.dispatchVKWinMouseEvent(evTyp, x, y, evBtn)
					})
				}
			}
		}
	}()
}

func (vw *VideoWidget) stopVKMouseForwarding() {
	if vkWinMouseQuit != nil {
		close(vkWinMouseQuit)
		vkWinMouseQuit = nil
		logrus.Info("[VK/Win] mouse forwarding stopped")
	}
}

func (vw *VideoWidget) dispatchVKWinMouseEvent(typ int, x, y float32, button int) {
	var tw *TouchpadWrapper
	if vkWinFullscreenWin != nil && vw.fullscreenDialog != nil {
		tw = vw.fullscreenDialog.touchpadWrapper
	} else {
		tw = vw.touchpadWrapper
	}
	if tw == nil {
		return
	}
	if !vw.isMouseConnected {
		if atomic.CompareAndSwapInt32(&vkWinMouseCheckPending, 0, 1) {
			go func() {
				vw.checkMouseConnected()
				atomic.StoreInt32(&vkWinMouseCheckPending, 0)
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
	case 2: // button/scroll press
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
				Button:     vkWinButtonToFyne(button),
			})
		}
	case 3: // button release — ignore scroll pseudo-buttons
		if button != 4 && button != 5 {
			tw.MouseUp(&desktop.MouseEvent{
				PointEvent: fyne.PointEvent{Position: pos},
				Button:     vkWinButtonToFyne(button),
			})
		}
	}
}

func vkWinButtonToFyne(b int) desktop.MouseButton {
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

// vkStageName translates the render-thread stage code into a human-readable label.
func vkStageName(s int) string {
	names := []string{"idle", "got-frame", "staging", "acquire", "fence-wait", "queue-submit", "present", "recreate-swapchain"}
	if s >= 0 && s < len(names) {
		return names[s]
	}
	return "unknown"
}

// Watchdog state — package-level (one Vulkan overlay active at a time).
var (
	vkWatchLastHB      int64
	vkWatchLastHBTime  time.Time
	vkWatchStuckLogged bool

	// Frame-stall watchdog: detects when frames are delivered to Go but Vulkan stops rendering.
	vkWatchLastRendered    int64
	vkWatchLastRenderedTime time.Time
	vkWatchRenderLogged    bool

	// Fyne main-loop watchdog: a goroutine pings fyne.Do every second; if the
	// ping takes >2 s the main loop is frozen and we log the cause immediately.
	vkFyneWatchOnce sync.Once
)

// startFyneWatchdog launches a background goroutine that detects Fyne main-loop freezes.
// IMPORTANT: must use wait=false in DoFromGoroutine. With wait=true DoFromGoroutine
// would block forever when the main loop is frozen, so the timeout select is unreachable.
func startFyneWatchdog(app fyne.App) {
	vkFyneWatchOnce.Do(func() {
		go func() {
			logrus.Info("[Vulkan/Win] watchdog: started (pinging Fyne main loop every 1s)")
			lastAliveLog := time.Now()
			for {
				time.Sleep(1 * time.Second)

				done := make(chan struct{}, 1)
				sent := time.Now()
				// wait=false: posts fn to main-loop queue and returns immediately.
				// This goroutine then races the timeout; if main loop never runs fn
				// within 2 s it means the main loop is frozen.
				app.Driver().DoFromGoroutine(func() {
					done <- struct{}{}
				}, false)

				select {
				case <-done:
					// Main loop responded. Log alive status every 5 s so log confirms watchdog is running.
					if time.Since(lastAliveLog) >= 5*time.Second {
						logrus.Infof("[Vulkan/Win] watchdog: Fyne main loop alive (resp in %dms)",
							time.Since(sent).Milliseconds())
						lastAliveLog = time.Now()
					}
				case <-time.After(2 * time.Second):
					logrus.Errorf("[Vulkan/Win] ⚠️ FYNE MAIN LOOP FROZEN >2s — sent=%s stage=%d",
						sent.Format("15:04:05.000"), func() int {
							_, s := service.VKVideoGetDiag()
							return s
						}())
					lastAliveLog = time.Now()
				}
			}
		}()
	})
}

func (vw *VideoWidget) isNativeVideoActive() bool {
	return service.VKVideoIsActive() || service.GLVideoIsActive()
}

// getNativeFPS returns the Vulkan (or GDI fallback) render FPS for the stats counter.
func (vw *VideoWidget) getNativeFPS() float64 {
	if service.VKVideoIsActive() {
		st := service.VKVideoGetStats()
		if st.FPS > 0 {
			return float64(st.FPS)
		}
	}
	if service.GLVideoIsActive() {
		st := service.GLVideoGetStats()
		if st.FPS > 0 {
			return float64(st.FPS)
		}
	}
	return 0
}

// startMetalVideoOnWindow creates the Vulkan child-window overlay (or falls back to GDI).
// Called on the first decoded frame and on fullscreen transitions.
func (vw *VideoWidget) startMetalVideoOnWindow(window fyne.Window, fullscreen bool) {
	// Wire overlay lifecycle hooks: hide the Vulkan overlay whenever a Fyne popup or
	// menu is visible, exactly mirroring the macOS Metal pattern.
	view.OnOverlayShow = func() { service.VKVideoSetHidden(true) }
	view.OnOverlayHide = func() { service.VKVideoSetHidden(false) }

	if window == nil {
		logrus.Warn("[Vulkan/Win] startMetalVideoOnWindow: window=nil — skipped")
		return
	}
	nw, ok := window.(driver.NativeWindow)
	if !ok {
		logrus.Warnf("[Vulkan/Win] window type %T does not implement NativeWindow — using Fyne canvas", window)
		vw.startRenderTicker()
		return
	}
	nw.RunNative(func(ctx any) {
		var hwnd uintptr
		switch c := ctx.(type) {
		case driver.WindowsWindowContext:
			hwnd = c.HWND
		case *driver.WindowsWindowContext:
			hwnd = c.HWND
		default:
			logrus.Warnf("[Vulkan/Win] RunNative ctx type=%T — using Fyne canvas", ctx)
			vw.startRenderTicker()
			return
		}
		if hwnd == 0 {
			logrus.Warn("[Vulkan/Win] HWND is null — using Fyne canvas")
			vw.startRenderTicker()
			return
		}

		var x, y, w, h int
		if !fullscreen {
			fx, fy, fw, fh := vw.videoCanvasFrame()
			if fw <= 0 || fh <= 0 {
				logrus.Warn("[Vulkan/Win] videoCanvas has zero size — using Fyne canvas")
				vw.startRenderTicker()
				return
			}
			scale := float32(1)
			if window.Canvas() != nil {
				scale = window.Canvas().Scale()
			}
			x = int(fx * scale)
			y = int(fy * scale)
			w = int(fw * scale)
			h = int(fh * scale)
		} else {
			// Fullscreen: the overlay must cover the entire fullscreen window.
			// Pass (0,0,fullW,fullH) so the Vulkan child-window fills the client area.
			if window.Canvas() != nil {
				cs := window.Canvas().Size()
				scale := window.Canvas().Scale()
				w = int(cs.Width * scale)
				h = int(cs.Height * scale)
			}
			if w <= 0 || h <= 0 {
				logrus.Warn("[Vulkan/Win] fullscreen canvas has zero size — using Fyne canvas")
				vw.startRenderTicker()
				return
			}
		}

		// Try Vulkan first; fall back to GDI if unavailable.
		service.VKVideoResetLastFrame()
		if service.VKVideoCreate(hwnd, x, y, w, h) {
			logrus.Infof("[Vulkan/Win] overlay active (fullscreen=%v) rect=(%d,%d,%dx%d)", fullscreen, x, y, w, h)
			// If a Fyne overlay (popup/menu) is already open when we start, hide immediately.
			if view.OverlayActive() {
				service.VKVideoSetHidden(true)
			}
			// Start Fyne main-loop watchdog: detects if wglSwapBuffers or Win32 message
			// dispatch hangs after the Vulkan overlay starts presenting.
			if vw.parentWindow != nil {
				startFyneWatchdog(fyne.CurrentApp())
			}
			// Forward mouse events from the Vulkan overlay window to TouchpadWrapper.
			// The overlay (without WS_EX_TRANSPARENT) captures all pointer events so
			// they never reach Fyne's GLFW window — we re-dispatch them manually via
			// fyne.Do, mirroring the Linux X11 implementation.
			scale := float32(1)
			if window.Canvas() != nil {
				scale = window.Canvas().Scale()
			}
			vw.startVKMouseForwarding(scale)
		} else {
			logrus.Warn("[Vulkan/Win] Vulkan init failed — trying GDI fallback")
			service.GLVideoResetLastFrame()
			if service.GLVideoCreate(hwnd, x, y, w, h, vw.enableVSync) {
				logrus.Infof("[GDI/Win] overlay active (fullscreen=%v) rect=(%d,%d,%dx%d)", fullscreen, x, y, w, h)
			} else {
				logrus.Warn("[GDI/Win] GDI overlay also failed — using Fyne canvas")
				vw.startRenderTicker()
				return
			}
		}

		// Clear the static Fyne canvas frame so Metal overlay takes over rendering.
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
	vw.stopVKMouseForwarding()
	service.VKVideoDestroy()
	service.GLVideoDestroy()
}

// updateMetalVideoFrame repositions the overlay and logs stats at 1 Hz.
func (vw *VideoWidget) updateMetalVideoFrame() {
	// When fullscreen is active the Vulkan child-window belongs to the fullscreen
	// window. The main-window VideoWidget must not call VKVideoUpdateFrame or it
	// would reposition the overlay to the smaller main-window rect, causing the
	// swapchain to be recreated at the wrong size and the fullscreen to go black.
	if vw.fullscreenDialog != nil && vw.fullscreenDialog.IsFullscreen() {
		return
	}

	if service.VKVideoIsActive() {
		st := service.VKVideoGetStats()

		// Watchdog 1: render-thread heartbeat stuck (thread blocked in Vulkan call).
		hb, stage := service.VKVideoGetDiag()
		if hb != vkWatchLastHB {
			vkWatchLastHB = hb
			vkWatchLastHBTime = time.Now()
			vkWatchStuckLogged = false
		} else if !vkWatchStuckLogged && time.Since(vkWatchLastHBTime) > 3*time.Second {
			vkWatchStuckLogged = true
			logrus.Warnf("[Vulkan/Win] ⚠️ RENDER THREAD STUCK >3s stage=%d(%s) rendered=%d submitted=%d hb=%d",
				stage, vkStageName(stage), st.Rendered, st.Submitted, hb)
		}

		// Watchdog 2: frames submitted to Vulkan but rendered count stalled.
		if st.Rendered != vkWatchLastRendered {
			vkWatchLastRendered = st.Rendered
			vkWatchLastRenderedTime = time.Now()
			vkWatchRenderLogged = false
		} else if st.Submitted > 0 && !vkWatchRenderLogged && time.Since(vkWatchLastRenderedTime) > 3*time.Second {
			vkWatchRenderLogged = true
			logrus.Warnf("[Vulkan/Win] ⚠️ RENDER STALL >3s rendered=%d submitted=%d stage=%d(%s) hb=%d",
				st.Rendered, st.Submitted, stage, vkStageName(stage), hb)
		}

		if st.FirstFrame || st.FPSReady {
			service.VKVideoClearPendingStats()
			if st.FirstFrame {
				logrus.Infof("[Vulkan/Win] first frame rendered — %dx%d", st.FW, st.FH)
			}
			if st.FPSReady {
				logrus.Infof("[Vulkan/Win] fps=%.1f rendered=%d submitted=%d size=%dx%d",
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
				logrus.Infof("[GDI/Win] first frame rendered — %dx%d", st.FW, st.FH)
			}
			if st.FPSReady {
				logrus.Infof("[GDI/Win] fps=%.1f rendered=%d submitted=%d size=%dx%d",
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
	vkWinFullscreenWin = fsWindow
	vw.stopVKMouseForwarding()
	service.VKVideoDestroy()
	service.GLVideoDestroy()
	vw.startMetalVideoOnWindow(fsWindow, true)
}

func (vw *VideoWidget) metalVideoExitFullscreen() {
	vkWinFullscreenWin = nil
	vw.stopVKMouseForwarding()
	service.VKVideoDestroy()
	service.GLVideoDestroy()
	vw.startMetalVideoOnWindow(vw.parentWindow, false)
}

// getMetalLastFrame is a no-op on Windows — no zero-copy GPU frame capture.
func (vw *VideoWidget) getMetalLastFrame() *image.RGBA { return nil }

// videoCanvasFrame returns the video canvas rect in window-local dp coordinates.
//
// Fyne widget positions are relative to their parent container, not the window,
// so vw.videoCanvas.Position() is always near (0,0) within its tab container.
// We derive the absolute y-offset the same way Mac Metal does: the video container
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
