//go:build linux && !android

package controller

import (
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
		var scale float32 = 1
		if !fullscreen {
			x, y, w, h := vw.videoCanvasFrame()
			if w <= 0 || h <= 0 {
				logrus.Warn("[VK/Linux] videoCanvas has zero size — skipped")
				return
			}
			if window.Canvas() != nil {
				scale = window.Canvas().Scale()
			}
			px = int(x * scale)
			py = int(y * scale)
			pw = int(w * scale)
			ph = int(h * scale)
		} else {
			if window.Canvas() != nil {
				scale = window.Canvas().Scale()
			}
		}

		// Try Vulkan first; fall back to GLX if unavailable.
		service.VKVideoResetLastFrame()
		if service.VKVideoCreate(xwin, px, py, pw, ph) {
			logrus.Infof("[VK/Linux] overlay active (fullscreen=%v) rect=(%d,%d,%dx%d)",
				fullscreen, px, py, pw, ph)
			if view.OverlayActive() {
				service.VKVideoSetHidden(true)
			}
			// Forward mouse events from Vulkan window to TouchpadWrapper.
			// GLFW stops delivering mouse events to Fyne when the cursor is over
			// the native child window, so we read them directly from the X11
			// connection and dispatch via fyne.Do.
			vw.startVKMouseForwarding(scale)
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
	vw.stopVKMouseForwarding()
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

// vkFullscreenWindow tracks the window currently hosting the VK overlay in
// fullscreen mode. nil means the overlay is on the main window.
var vkFullscreenWindow fyne.Window

func (vw *VideoWidget) metalVideoEnterFullscreen(fsWindow fyne.Window) {
	if fsWindow == nil {
		return
	}
	vkFullscreenWindow = fsWindow
	vw.stopVKMouseForwarding()
	service.VKVideoDestroy()
	service.GLVideoDestroy()
	vw.startMetalVideoOnWindow(fsWindow, true)
}

func (vw *VideoWidget) metalVideoExitFullscreen() {
	vkFullscreenWindow = nil
	vw.stopVKMouseForwarding()
	service.VKVideoDestroy()
	service.GLVideoDestroy()
	vw.startMetalVideoOnWindow(vw.parentWindow, false)
}

// videoCanvasFrame returns the video area rect in window-local dp coordinates.
//
// Fullscreen: the VK child window covers the entire fullscreen window → (0,0,W,H).
//
// Normal: vw.videoCanvas.Position() is always (0,0) within its parent
// container; the canvas origin comes from videoContainerOrigin so a
// footer under the video is not treated as chrome above it.
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if fsWin := vkFullscreenWindow; fsWin != nil {
		sz := fsWin.Canvas().Size()
		return 0, 0, sz.Width, sz.Height
	}
	if vw.container == nil || vw.parentWindow == nil {
		return
	}
	sz := vw.container.Size()
	pos := vw.videoContainerOrigin()
	h = sz.Height
	if vw.contentContainer != nil && vw.contentContainer.Visible() {
		if kh := vw.contentContainer.Size().Height; kh > 0 {
			h -= kh
			if h < 0 {
				h = 0
			}
		}
	}
	return pos.X, pos.Y, sz.Width, h
}

// ── Vulkan mouse event forwarding ─────────────────────────────────────────────
//
// When the Vulkan child window is mapped over the Fyne/GLFW window, the X server
// delivers pointer events to it (registered via XSelectInput in vk_video_create).
// GLFW receives LeaveNotify and stops dispatching mouse events to Fyne.
// We compensate by polling vk_video_next_event and dispatching directly to
// TouchpadWrapper on the Fyne main goroutine.

var vkMouseQuit chan struct{}

// vkMouseCheckPending prevents multiple concurrent checkMouseConnected goroutines.
var vkMouseCheckPending int32 // atomic

type vkMouseEv struct {
	typ, x, y, btn int
}

// vkMouseDoPending/vkMousePending coalesce bursts of X11 events into a single
// fyne.Do dispatch per animation-loop turn instead of one per sample. Xlib
// selects PointerMotionMask (uncompressed) on the overlay window, so a fast
// mouse can queue dozens of MotionNotify events per 4ms poll tick; issuing
// one fyne.Do per event previously serialized that whole burst onto the
// GLFW/Fyne main goroutine — the same goroutine that drives frame
// presentation — which is what caused the video to visibly stutter only
// while the mouse was moving. Mirrors the Windows implementation
// (video_widget_windows.go's queueVKWinMouseBatch), which already did this.
var (
	vkMouseDoPending int32
	vkMousePendingMu sync.Mutex
	vkMousePending   []vkMouseEv
)

func (vw *VideoWidget) startVKMouseForwarding(scale float32) {
	vw.stopVKMouseForwarding()
	quit := make(chan struct{})
	vkMouseQuit = quit
	logrus.Info("[VK/Linux] mouse forwarding started")
	go func() {
		ticker := time.NewTicker(4 * time.Millisecond) // ~250 Hz poll
		defer ticker.Stop()
		for {
			select {
			case <-quit:
				return
			case <-ticker.C:
				var batch []vkMouseEv
				for {
					typ, ex, ey, btn, ok := service.VKVideoNextEvent()
					if !ok {
						break
					}
					// Coalesce consecutive moves so a burst from the overlay
					// queue does not enqueue one fyne.Do per sample.
					if typ == 1 && len(batch) > 0 && batch[len(batch)-1].typ == 1 {
						batch[len(batch)-1] = vkMouseEv{typ, ex, ey, btn}
						continue
					}
					batch = append(batch, vkMouseEv{typ, ex, ey, btn})
				}
				if len(batch) == 0 {
					continue
				}
				vw.queueVKMouseBatch(scale, batch)
			}
		}
	}()
}

func (vw *VideoWidget) queueVKMouseBatch(scale float32, batch []vkMouseEv) {
	vkMousePendingMu.Lock()
	vkMousePending = append(vkMousePending, batch...)
	vkMousePendingMu.Unlock()
	if !atomic.CompareAndSwapInt32(&vkMouseDoPending, 0, 1) {
		return
	}
	fyne.Do(func() {
		vkMousePendingMu.Lock()
		evs := vkMousePending
		vkMousePending = nil
		vkMousePendingMu.Unlock()
		if service.VKVideoIsActive() && len(evs) > 0 {
			// Re-read scale once per dispatch: handles HiDPI changes and
			// fullscreen vs windowed transitions at runtime.
			s := scale
			if fsWin := vkFullscreenWindow; fsWin != nil {
				if fsWin.Canvas() != nil {
					s = fsWin.Canvas().Scale()
				}
			} else if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
				s = vw.parentWindow.Canvas().Scale()
			}
			for _, ev := range evs {
				x := float32(ev.x) / s
				y := float32(ev.y) / s
				vw.dispatchVKMouseEvent(ev.typ, x, y, ev.btn)
			}
		}
		atomic.StoreInt32(&vkMouseDoPending, 0)
		vkMousePendingMu.Lock()
		more := len(vkMousePending) > 0
		vkMousePendingMu.Unlock()
		if more {
			vw.queueVKMouseBatch(scale, nil)
		}
	})
}

func (vw *VideoWidget) stopVKMouseForwarding() {
	if vkMouseQuit != nil {
		close(vkMouseQuit)
		vkMouseQuit = nil
		logrus.Info("[VK/Linux] mouse forwarding stopped")
	}
	vkMousePendingMu.Lock()
	vkMousePending = nil
	vkMousePendingMu.Unlock()
}

// dispatchVKMouseEvent dispatches a forwarded X11 pointer event to TouchpadWrapper.
// Must be called on the Fyne main goroutine (inside fyne.Do).
func (vw *VideoWidget) dispatchVKMouseEvent(typ int, x, y float32, button int) {
	// In fullscreen the active input target is the fullscreen dialog's wrapper.
	var tw *TouchpadWrapper
	if vkFullscreenWindow != nil && vw.fullscreenDialog != nil {
		tw = vw.fullscreenDialog.touchpadWrapper
	} else {
		tw = vw.touchpadWrapper
	}
	if tw == nil {
		logrus.Warn("[VK/Mouse] touchpadWrapper is nil — events dropped")
		return
	}
	if !vw.isMouseConnected {
		// The VK overlay may start before the periodic Refresh()->checkMouseConnected()
		// poll runs. Trigger a single async refresh so subsequent events work.
		if atomic.CompareAndSwapInt32(&vkMouseCheckPending, 0, 1) {
			go func() {
				vw.checkMouseConnected()
				atomic.StoreInt32(&vkMouseCheckPending, 0)
			}()
		}
		return
	}
	pos := fyne.NewPos(x, y)
	switch typ {
	case 1: // MotionNotify
		tw.MouseMoved(&desktop.MouseEvent{
			PointEvent: fyne.PointEvent{Position: pos},
		})
	case 2: // ButtonPress
		switch button {
		case 4: // scroll wheel up
			tw.Scrolled(&fyne.ScrollEvent{
				PointEvent: fyne.PointEvent{Position: pos},
				Scrolled:   fyne.Delta{DX: 0, DY: 10},
			})
		case 5: // scroll wheel down
			tw.Scrolled(&fyne.ScrollEvent{
				PointEvent: fyne.PointEvent{Position: pos},
				Scrolled:   fyne.Delta{DX: 0, DY: -10},
			})
		default:
			tw.MouseDown(&desktop.MouseEvent{
				PointEvent: fyne.PointEvent{Position: pos},
				Button:     vkX11ButtonToFyne(button),
			})
		}
	case 3: // ButtonRelease — ignore scroll pseudo-buttons
		if button != 4 && button != 5 {
			tw.MouseUp(&desktop.MouseEvent{
				PointEvent: fyne.PointEvent{Position: pos},
				Button:     vkX11ButtonToFyne(button),
			})
		}
	}
}

func vkX11ButtonToFyne(b int) desktop.MouseButton {
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
