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

// startMetalVideoOnWindow starts the native GDI renderer ONLY in fullscreen mode.
//
// Windowed mode → Fyne canvas path (pendingFrame → 60Hz ticker → canvas.Image).
//   GDI on the parent DC bleeds over menus/tabs in windowed mode and conflicts
//   with Fyne's GL SwapBuffers, causing visible 1-3 Hz flickering.
//
// Fullscreen mode → GDI direct path on the fullscreen window.
//   The fullscreen window has no overlapping UI, so parent-DC GDI is safe and fast.
func (vw *VideoWidget) startMetalVideoOnWindow(window fyne.Window, fullscreen bool) {
	if !fullscreen {
		// Windowed: Fyne canvas path handles rendering — nothing to do here.
		return
	}

	if window == nil {
		return
	}
	nw, ok := window.(driver.NativeWindow)
	if !ok {
		logrus.Warn("[GDI/Win] fullscreen window does not implement NativeWindow — GDI skipped")
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
		// Pass 0,0,0,0 — gl_video_create uses GetClientRect(hwnd) as fallback,
		// so the overlay always covers the entire fullscreen window automatically.
		service.GLVideoResetLastFrame()
		if !service.GLVideoCreate(hwnd, 0, 0, 0, 0, vw.enableVSync) {
			logrus.Warn("[GDI/Win] fullscreen GDI overlay creation failed — Fyne canvas path active")
			return
		}
		logrus.Info("[GDI/Win] fullscreen GDI overlay active")
	})
}

func (vw *VideoWidget) stopMetalVideo() {
	service.GLVideoDestroy()
}

func (vw *VideoWidget) updateMetalVideoFrame() {
	if !service.GLVideoIsActive() {
		return
	}
	// GDI is only active in fullscreen mode.
	// The overlay covers the entire fullscreen window via GetClientRect fallback —
	// no explicit rect update is needed.
	st := service.GLVideoGetStats()
	if st.FirstFrame || st.FPSReady {
		service.GLVideoClearPendingStats()
		if st.FirstFrame {
			logrus.Infof("[GDI/Win] fullscreen first frame rendered — %dx%d", st.FW, st.FH)
		}
		if st.FPSReady {
			logrus.Infof("[GDI/Win] fullscreen fps=%.1f  rendered=%d  submitted=%d  max_gap=%.1fms",
				st.FPS, st.Rendered, st.Submitted, st.MaxGapMs)
			// Warn if the GDI surface was dark for more than 8 ms — indicates
			// that DWM cleared the GDI layer between our blits (visible flash).
			if st.MaxGapMs > 8 {
				logrus.Warnf("[GDI/Win] blit gap %.1fms > 8ms — DWM may be clearing GDI surface; consider reducing blit interval", st.MaxGapMs)
			}
		}
	}
}

func (vw *VideoWidget) metalVideoEnterFullscreen(fsWindow fyne.Window) {
	if fsWindow == nil {
		return
	}
	// Destroy any existing overlay (windowed mode has none, but defensive).
	service.GLVideoDestroy()
	vw.startMetalVideoOnWindow(fsWindow, true)
}

func (vw *VideoWidget) metalVideoExitFullscreen() {
	// Destroy the fullscreen GDI overlay.
	// Windowed mode resumes the Fyne canvas path automatically:
	// isNativeVideoActive() returns false → pendingFrame → canvas.Image.
	service.GLVideoDestroy()
}
