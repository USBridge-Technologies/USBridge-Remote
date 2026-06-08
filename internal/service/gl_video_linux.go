//go:build linux && !android && cgo

package service

/*
#cgo LDFLAGS: -lGL -lX11

#include <stdint.h>

// Implemented in gl_video_impl_linux.c.
extern int  gl_video_is_active(void);
extern int  gl_video_try_submit(uint8_t *rgba, int width, int height, int stride);
extern int  gl_video_create(uintptr_t parent_xwin, int x, int y, int w, int h);
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
func GLVideoCreate(xwin uintptr, x, y, w, h int) bool {
	return C.gl_video_create(C.uintptr_t(xwin), C.int(x), C.int(y), C.int(w), C.int(h)) != 0
}

func GLVideoUpdateFrame(x, y, w, h int) {
	C.gl_video_update_frame(C.int(x), C.int(y), C.int(w), C.int(h))
}

func GLVideoDestroy() {
	C.gl_video_destroy()
}
