//go:build windows && cgo

package service

/*
#cgo LDFLAGS: -lgdi32 -luser32

#include <stdint.h>

// Implemented in gl_video_impl_windows.c.
extern int  gl_video_is_active(void);
extern int  gl_video_try_submit(uint8_t *rgba, int width, int height, int stride);
extern int  gl_video_create(uintptr_t parent_hwnd, int x, int y, int w, int h, int vsync);
extern void gl_video_update_frame(int x, int y, int w, int h);
extern void gl_video_destroy(void);
extern void gl_video_get_stats(long long *rendered, long long *submitted,
                               float *fps, int *fps_ready,
                               int *first_frame, int *fw, int *fh);
extern void gl_video_clear_pending_stats(void);

// goGLLog is called only from gl_video_create/destroy (CGO context — safe).
// It is NOT called from the render thread to avoid Go-GC deadlock.
extern void goGLLog(char *msg, int level);
*/
import "C"

import (
	"unsafe"

	"github.com/sirupsen/logrus"
)

//export goGLLog
func goGLLog(msg *C.char, level C.int) {
	text := C.GoString(msg)
	switch int(level) {
	case 1:
		logrus.Warnf("[GDI/Win] %s", text)
	case 2:
		logrus.Errorf("[GDI/Win] %s", text)
	default:
		logrus.Infof("[GDI/Win] %s", text)
	}
}

func GLVideoIsActive() bool {
	return C.gl_video_is_active() != 0
}

// GLVideoTrySubmit offers a BGRA frame to the native Windows render thread.
// Returns true if the GDI overlay is active and the frame was queued.
func GLVideoTrySubmit(rgba []byte, width, height, stride int) bool {
	if len(rgba) == 0 {
		return false
	}
	return C.gl_video_try_submit(
		(*C.uint8_t)(unsafe.Pointer(&rgba[0])),
		C.int(width), C.int(height), C.int(stride),
	) != 0
}

// GLVideoCreate creates (or replaces) the GDI overlay on the given HWND.
// x,y,w,h are in Windows client pixels (DPI-scaled by caller).
// Pass w=0,h=0 to cover the entire parent client area.
func GLVideoCreate(hwnd uintptr, x, y, w, h int, vsync bool) bool {
	v := C.int(0)
	if vsync {
		v = 1
	}
	return C.gl_video_create(C.uintptr_t(hwnd), C.int(x), C.int(y), C.int(w), C.int(h), v) != 0
}

// Last overlay geometry sent to C — used to suppress no-op repositions.
// Written and read only from fyne.Do (single-threaded), no mutex needed.
var glOverlayLastX, glOverlayLastY, glOverlayLastW, glOverlayLastH int

// GLVideoUpdateFrame repositions the GDI overlay (pixels).
// Skips the call if the geometry hasn't changed to avoid redundant PostMessage spam.
func GLVideoUpdateFrame(x, y, w, h int) {
	if x == glOverlayLastX && y == glOverlayLastY && w == glOverlayLastW && h == glOverlayLastH {
		return
	}
	glOverlayLastX, glOverlayLastY, glOverlayLastW, glOverlayLastH = x, y, w, h
	C.gl_video_update_frame(C.int(x), C.int(y), C.int(w), C.int(h))
}

// GLVideoResetLastFrame resets the cached overlay geometry so the next
// GLVideoUpdateFrame call unconditionally repositions the window.
func GLVideoResetLastFrame() {
	glOverlayLastX, glOverlayLastY, glOverlayLastW, glOverlayLastH = 0, 0, 0, 0
}

// GLVideoDestroy removes the overlay and stops the render thread.
func GLVideoDestroy() {
	C.gl_video_destroy()
}

// GLVideoStats holds render-thread statistics read via atomic polling.
type GLVideoStats struct {
	Rendered   int64
	Submitted  int64
	FPS        float32
	FPSReady   bool
	FirstFrame bool
	FW, FH     int
}

// GLVideoGetStats reads stats accumulated by the render thread.
// Safe to call from any Go goroutine (no CGO from render thread).
func GLVideoGetStats() GLVideoStats {
	var r, s C.longlong
	var fp C.float
	var fpsr, ff, fw, fh C.int
	C.gl_video_get_stats(&r, &s, &fp, &fpsr, &ff, &fw, &fh)
	return GLVideoStats{
		Rendered:   int64(r),
		Submitted:  int64(s),
		FPS:        float32(fp),
		FPSReady:   fpsr != 0,
		FirstFrame: ff != 0,
		FW:         int(fw),
		FH:         int(fh),
	}
}

// GLVideoClearPendingStats resets the "first frame" and "fps ready" flags
// after Go has consumed them.
func GLVideoClearPendingStats() {
	C.gl_video_clear_pending_stats()
}
