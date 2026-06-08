//go:build windows && cgo

package service

/*
#cgo LDFLAGS: -lopengl32

#include <stdint.h>

// Implemented in gl_video_impl_windows.c.
extern int  gl_video_is_active(void);
extern int  gl_video_try_submit(uint8_t *rgba, int width, int height, int stride);
extern int  gl_video_create(uintptr_t parent_hwnd, int x, int y, int w, int h);
extern void gl_video_update_frame(int x, int y, int w, int h);
extern void gl_video_destroy(void);

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
		logrus.Warnf("[GL/Win] %s", text)
	case 2:
		logrus.Errorf("[GL/Win] %s", text)
	default:
		logrus.Infof("[GL/Win] %s", text)
	}
}

func GLVideoIsActive() bool {
	return C.gl_video_is_active() != 0
}

// GLVideoTrySubmit offers an RGBA frame to the GL render thread.
// Returns true if the GL overlay is active and the frame was queued.
func GLVideoTrySubmit(rgba []byte, width, height, stride int) bool {
	if len(rgba) == 0 {
		return false
	}
	return C.gl_video_try_submit(
		(*C.uint8_t)(unsafe.Pointer(&rgba[0])),
		C.int(width), C.int(height), C.int(stride),
	) != 0
}

// GLVideoCreate creates (or replaces) the GL overlay on the given HWND.
// x,y,w,h are in Windows client pixels (DPI-scaled by caller).
// Pass w=0,h=0 to cover the entire parent client area.
func GLVideoCreate(hwnd uintptr, x, y, w, h int) bool {
	return C.gl_video_create(C.uintptr_t(hwnd), C.int(x), C.int(y), C.int(w), C.int(h)) != 0
}

// GLVideoUpdateFrame repositions the GL overlay (pixels).
func GLVideoUpdateFrame(x, y, w, h int) {
	C.gl_video_update_frame(C.int(x), C.int(y), C.int(w), C.int(h))
}

// GLVideoDestroy removes the overlay and stops the render thread.
func GLVideoDestroy() {
	C.gl_video_destroy()
}
