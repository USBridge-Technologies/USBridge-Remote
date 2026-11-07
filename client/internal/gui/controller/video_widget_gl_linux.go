//go:build linux && !android

package controller

import (
	"math"
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
	vw.startMetalVideoOnWindowAttempt(window, fullscreen, 0)
}

// videoCanvasZeroSizeRetries/Delay bound how long the native overlay waits
// for the video canvas to get a layout size: a connect fired at launch
// (usbridge:// deeplink with immediate=true) reaches this before the window
// has been laid out once, and giving up there left a permanently black
// window even though frames were decoding fine.
const (
	videoCanvasZeroSizeRetries = 50
	videoCanvasZeroSizeDelay   = 100 * time.Millisecond
)

func (vw *VideoWidget) startMetalVideoOnWindowAttempt(window fyne.Window, fullscreen bool, attempt int) {
	// Fyne menus no longer hide the video: syncVKOcclusion cuts their rects
	// out of the Vulkan child window instead (see view.OverlayHoleRects).
	view.OnOverlayShow = vw.scheduleVKOcclusionSync
	view.OnOverlayHide = vw.scheduleVKOcclusionSync

	if window == nil {
		return
	}
	renderer := service.VideoRenderer()
	if renderer == service.VideoRendererSoftware {
		if attempt == 0 {
			logrus.Info("[VK/Linux] software renderer chosen in Client settings — using Fyne canvas")
		}
		view.OnOverlayShow = nil
		view.OnOverlayHide = nil
		return
	}
	nw, ok := window.(driver.NativeWindow)
	if !ok {
		logrus.Warn("[VK/Linux] window does not implement NativeWindow — using Fyne canvas")
		return
	}
	nw.RunNative(func(ctx any) {
		var xwin, wlSurface uintptr
		switch c := ctx.(type) {
		case *driver.X11WindowContext:
			xwin = c.WindowHandle
		case driver.X11WindowContext:
			xwin = c.WindowHandle
		case *driver.WaylandWindowContext:
			wlSurface = c.WaylandSurface
		case driver.WaylandWindowContext:
			wlSurface = c.WaylandSurface
		default:
			logrus.Warnf("[VK/Linux] unexpected native context type %T — skipped", ctx)
			return
		}

		var px, py, pw, ph int
		var scale float32 = 1
		if !fullscreen {
			x, y, w, h := vw.videoCanvasFrame()
			if w <= 0 || h <= 0 {
				if attempt < videoCanvasZeroSizeRetries {
					if attempt == 0 {
						logrus.Info("[VK/Linux] videoCanvas has no size yet — retrying until the window is laid out")
					}
					time.AfterFunc(videoCanvasZeroSizeDelay, func() {
						fyne.Do(func() { vw.startMetalVideoOnWindowAttempt(window, fullscreen, attempt+1) })
					})
					return
				}
				logrus.Warn("[VK/Linux] videoCanvas has zero size — skipped")
				return
			}
			scale = overlayPixelScale(window.Canvas())
			px = int(x * scale)
			py = int(y * scale)
			pw = int(w * scale)
			ph = int(h * scale)
		} else {
			scale = overlayPixelScale(window.Canvas())
		}

		if wlSurface != 0 {
			vw.startWaylandVideo(window, wlSurface, px, py, pw, ph)
			return
		}
		vkOnWayland = false

		// Try Vulkan first (unless OpenGL is chosen in Client settings);
		// fall back to GLX if unavailable.
		service.VKVideoResetLastFrame()
		if renderer != service.VideoRendererOpenGL && service.VKVideoCreate(xwin, px, py, pw, ph) {
			logrus.Infof("[VK/Linux] overlay active (fullscreen=%v) rect=(%d,%d,%dx%d)",
				fullscreen, px, py, pw, ph)
			vkOcclusion.reset()
			vw.syncVKOcclusion()
			// Forward mouse events from Vulkan window to TouchpadWrapper.
			// GLFW stops delivering mouse events to Fyne when the cursor is over
			// the native child window, so we read them directly from the X11
			// connection and dispatch via fyne.Do.
			vw.startVKMouseForwarding(scale)
		} else {
			if renderer == service.VideoRendererOpenGL {
				logrus.Info("[VK/Linux] OpenGL chosen in Client settings — GLX overlay")
			} else {
				logrus.Warn("[VK/Linux] Vulkan init failed — trying GLX fallback")
			}
			if !service.GLVideoCreate(xwin, px, py, pw, ph, vw.enableVSync) {
				view.OnOverlayShow = nil
				view.OnOverlayHide = nil
				logrus.Warn("[GLX/Linux] GLX overlay also failed — using Fyne canvas")
				return
			}
			logrus.Infof("[GLX/Linux] overlay active (fullscreen=%v) rect=(%d,%d,%dx%d)",
				fullscreen, px, py, pw, ph)
			glOcclusion.reset()
			vw.syncVKOcclusion()
		}

		vw.nativeVideoReady()
	})
}

// nativeVideoReady hands the picture over from the Fyne canvas to the
// native overlay.
func (vw *VideoWidget) nativeVideoReady() {
	if vw.videoCanvas != nil {
		vw.videoCanvas.Image = nil
		vw.videoCanvas.Translucency = 0
		vw.videoCanvas.Refresh()
	}
	if cb := vw.onNativeReady; cb != nil {
		vw.onNativeReady = nil
		cb()
	}
}

// vkOnWayland: the overlay (Vulkan or its EGL fallback) is a wl_subsurface
// (native Wayland) rather than an X11 child window. Pointer events then reach Fyne directly -- the
// subsurface has an empty input region -- so nothing is forwarded.
var vkOnWayland bool

// vkWaylandScale is the pixels-per-logical-unit factor last given to the
// Wayland overlay.
var vkWaylandScale float32

// startWaylandVideo creates the Vulkan overlay on a subsurface of the
// window's wl_surface, falling back to EGL/GL on the same kind of subsurface
// (the Wayland counterpart of the X11 GLX fallback).
func (vw *VideoWidget) startWaylandVideo(window fyne.Window, surface uintptr, px, py, pw, ph int) {
	display := waylandDisplay()
	ls := overlayLogicalScale(window.Canvas())
	vkOnWayland = true
	vkWaylandScale = ls
	service.VKVideoResetLastFrame()
	openGL := service.VideoRenderer() == service.VideoRendererOpenGL
	if !openGL && display != 0 && service.VKVideoCreateWayland(display, surface, px, py, pw, ph, ls) {
		logrus.Infof("[VK/Wayland] overlay active rect=(%d,%d,%dx%d) scale=%.2f", px, py, pw, ph, ls)
		vkOcclusion.reset()
		vw.syncVKOcclusion()
		vw.nativeVideoReady()
		return
	}
	if openGL {
		logrus.Info("[VK/Wayland] OpenGL chosen in Client settings — EGL overlay")
	} else {
		logrus.Warn("[VK/Wayland] Vulkan subsurface failed — trying EGL fallback")
	}
	if display == 0 || !service.GLVideoCreateWayland(display, surface, px, py, pw, ph, ls, vw.enableVSync) {
		view.OnOverlayShow = nil
		view.OnOverlayHide = nil
		vkOnWayland = false
		logrus.Warn("[GL/Wayland] EGL overlay also failed — using Fyne canvas")
		return
	}
	logrus.Infof("[GL/Wayland] overlay active rect=(%d,%d,%dx%d) scale=%.2f", px, py, pw, ph, ls)
	glOcclusion.reset()
	vw.syncVKOcclusion()
	vw.nativeVideoReady()
}

// overlayPixelScale converts Fyne dp to the overlay's buffer pixels. On X11
// that is the canvas scale; on Wayland the canvas scale stays at the user's
// setting and the output scale lives in the framebuffer (texture) scale,
// which PixelCoordinateForPosition includes.
func overlayPixelScale(c fyne.Canvas) float32 {
	if c == nil {
		return 1
	}
	if !vkOnWaylandPlatform() {
		return c.Scale()
	}
	const probe = 1000
	px, _ := c.PixelCoordinateForPosition(fyne.NewPos(probe, 0))
	if px <= 0 {
		return c.Scale()
	}
	return float32(px) / probe
}

// overlayLogicalScale is buffer pixels per Wayland logical unit.
func overlayLogicalScale(c fyne.Canvas) float32 {
	if c == nil || c.Scale() <= 0 {
		return 1
	}
	return overlayPixelScale(c) / c.Scale()
}

func (vw *VideoWidget) stopMetalVideo() {
	vw.onNativeReady = nil
	view.OnOverlayShow = nil
	view.OnOverlayHide = nil
	vw.stopVKMouseForwarding()
	service.VKVideoDestroy()
	service.GLVideoDestroy()
	vkOnWayland = false
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
		win := vkFullscreenWindow
		if win == nil {
			win = vw.parentWindow
		}
		scale := float32(1)
		if win != nil && win.Canvas() != nil {
			scale = overlayPixelScale(win.Canvas())
			if vkOnWayland {
				if ls := overlayLogicalScale(win.Canvas()); ls != vkWaylandScale {
					vkWaylandScale = ls
					service.VKVideoSetWaylandScale(ls)
					service.VKVideoResetLastFrame()
				}
			}
		}
		service.VKVideoUpdateFrame(int(x*scale), int(y*scale), int(w*scale), int(h*scale))
		vw.syncVKOcclusion()
		if st.FW > 0 && st.FH > 0 {
			vw.noteStreamPixelSize(float32(st.FW), float32(st.FH))
		}
		if w > 0 && h > 0 {
			vw.UpdateTouchpadAndContentRect(w, h, nil)
		}
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
		win := vkFullscreenWindow
		if win == nil {
			win = vw.parentWindow
		}
		scale := float32(1)
		if win != nil && win.Canvas() != nil {
			scale = overlayPixelScale(win.Canvas())
			if vkOnWayland {
				if ls := overlayLogicalScale(win.Canvas()); ls != vkWaylandScale {
					vkWaylandScale = ls
					service.GLVideoSetWaylandScale(ls)
				}
			}
		}
		service.GLVideoUpdateFrame(int(x*scale), int(y*scale), int(w*scale), int(h*scale))
		vw.syncVKOcclusion()
	}
}

// vkOcclusion holds the holes pushed to the Vulkan child window.
var vkOcclusion = overlayOcclusion[[4]int]{
	setHidden: service.VKVideoSetHidden,
	setHoles:  service.VKVideoSetHoles,
}

// glOcclusion is the same for the GL fallback overlay.
var glOcclusion = overlayOcclusion[[4]int]{
	setHidden: service.GLVideoSetHidden,
	setHoles:  service.GLVideoSetHoles,
}

func (vw *VideoWidget) scheduleVKOcclusionSync() {
	scheduleOcclusionSync(vw.syncVKOcclusion)
}

// syncVKOcclusion cuts open menus out of the native video overlay -- the
// Vulkan renderer or its GL fallback; an X11 shape, or transparent pixels on
// Wayland (see overlayOcclusion). Fyne main goroutine only.
func (vw *VideoWidget) syncVKOcclusion() {
	occ := &vkOcclusion
	switch {
	case service.VKVideoIsActive():
	case service.GLVideoIsActive():
		occ = &glOcclusion
	default:
		return
	}
	win := vkFullscreenWindow
	if win == nil {
		win = vw.parentWindow
	}
	var c fyne.Canvas
	if win != nil {
		c = win.Canvas()
	}
	_, changed := occ.sync(c, func(rects []view.OverlayRect) [][4]int {
		vx, vy, vwid, vh := vw.videoCanvasFrame()
		scale := overlayPixelScale(c)
		var holes [][4]int
		for _, r := range rects {
			if x, y, w, h, ok := overlayHolePx(r, -vx, -vy, scale, 0, 0, int(vwid*scale)+1, int(vh*scale)+1); ok {
				holes = appendRoundedHole(holes, x, y, w, h, int(math.Round(float64(r.R*scale))))
			}
		}
		return holes
	})
	if changed {
		repaintUnderHoles(c)
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
	if vkOnWayland {
		return // events reach Fyne directly through the subsurface
	}
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
