//go:build linux && !android && cgo

package service

/*
#cgo LDFLAGS: -lGL -lX11 -lXext -lEGL -lwayland-egl -lwayland-client

#include <stdint.h>

// Implemented in gl_video_impl_linux.c.
extern int  gl_video_is_active(void);
extern int  gl_video_try_submit(uint8_t *rgba, int width, int height, int stride);
extern int  gl_video_create(uintptr_t parent_xwin, int x, int y, int w, int h, int vsync);
extern void gl_video_update_frame(int x, int y, int w, int h);
extern int  gl_video_create_wayland(uintptr_t display, uintptr_t parent_surface,
                                    int x, int y, int w, int h, float scale, int vsync);
extern void gl_video_set_wl_scale(float scale);
extern void gl_video_set_holes(const int *xywh, int n);
extern void gl_video_set_hidden(int hidden);
extern void gl_video_destroy(void);
extern void gl_video_get_stats(long long *rendered, long long *submitted,
                               float *fps, int *fps_ready,
                               int *first_frame, int *fw, int *fh);
extern void gl_video_clear_pending_stats(void);

// goGLLog is called only from gl_video_create/destroy (CGO context — safe).
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
		logrus.Warnf("[GL/Linux] %s", text)
	case 2:
		logrus.Errorf("[GL/Linux] %s", text)
	default:
		logrus.Infof("[GL/Linux] %s", text)
	}
}

func GLVideoIsActive() bool {
	return C.gl_video_is_active() != 0
}

func GLVideoTrySubmit(rgba []byte, width, height, stride int) bool {
	if len(rgba) == 0 {
		return false
	}
	return C.gl_video_try_submit(
		(*C.uint8_t)(unsafe.Pointer(&rgba[0])),
		C.int(width), C.int(height), C.int(stride),
	) != 0
}

// GLVideoCreate creates (or replaces) the GL overlay on the given X11 Window.
// x,y,w,h are in X11 pixels (DPI-scaled by caller).
// Pass w=0,h=0 to cover the entire parent window.
func GLVideoCreate(xwin uintptr, x, y, w, h int, vsync bool) bool {
	v := C.int(0)
	if vsync {
		v = 1
	}
	return C.gl_video_create(C.uintptr_t(xwin), C.int(x), C.int(y), C.int(w), C.int(h), v) != 0
}

// GLVideoCreateWayland is the native-Wayland GL fallback: EGL on a
// wl_subsurface of the window's wl_surface. x,y,w,h are buffer pixels,
// scale is buffer pixels per logical unit.
func GLVideoCreateWayland(display, surface uintptr, x, y, w, h int, scale float32, vsync bool) bool {
	v := C.int(0)
	if vsync {
		v = 1
	}
	return C.gl_video_create_wayland(C.uintptr_t(display), C.uintptr_t(surface),
		C.int(x), C.int(y), C.int(w), C.int(h), C.float(scale), v) != 0
}

// GLVideoSetWaylandScale updates the pixels-per-logical-unit factor.
func GLVideoSetWaylandScale(scale float32) {
	C.gl_video_set_wl_scale(C.float(scale))
}

var glOverlayLastX, glOverlayLastY, glOverlayLastW, glOverlayLastH int

// GLVideoUpdateFrame moves/resizes the overlay; repeats of the last rect are
// dropped (it is called on every UI tick, and on X11 a rect change re-cuts
// the menu holes' shape).
func GLVideoUpdateFrame(x, y, w, h int) {
	if x == glOverlayLastX && y == glOverlayLastY && w == glOverlayLastW && h == glOverlayLastH {
		return
	}
	glOverlayLastX, glOverlayLastY, glOverlayLastW, glOverlayLastH = x, y, w, h
	C.gl_video_update_frame(C.int(x), C.int(y), C.int(w), C.int(h))
}

// GLVideoSetHoles sets the menu rects (overlay pixels) the GL overlay leaves
// uncovered: an X11 shape cut-out, or alpha-0 pixels on Wayland.
func GLVideoSetHoles(rects [][4]int) {
	if len(rects) == 0 {
		C.gl_video_set_holes(nil, 0)
		return
	}
	flat := make([]C.int, 0, len(rects)*4)
	for _, r := range rects {
		flat = append(flat, C.int(r[0]), C.int(r[1]), C.int(r[2]), C.int(r[3]))
	}
	C.gl_video_set_holes(&flat[0], C.int(len(rects)))
}

// GLVideoSetHidden hides the overlay (an overlay of unknown shape is open).
func GLVideoSetHidden(hidden bool) {
	h := C.int(0)
	if hidden {
		h = 1
	}
	C.gl_video_set_hidden(h)
}

func GLVideoDestroy() {
	glOverlayLastX, glOverlayLastY, glOverlayLastW, glOverlayLastH = 0, 0, 0, 0
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

func GLVideoClearPendingStats() {
	C.gl_video_clear_pending_stats()
}
