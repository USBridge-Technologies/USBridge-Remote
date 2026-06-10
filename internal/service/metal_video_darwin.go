//go:build darwin && cgo

package service

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework CoreVideo -framework QuartzCore -framework CoreFoundation

#include <stdint.h>
#include <CoreVideo/CoreVideo.h>

// Implemented in metal_video_impl_darwin.m (compiled as a separate translation unit).
extern int  metal_video_is_active(void);
extern int  metal_video_try_submit(CVImageBufferRef img);
extern int  metal_video_create(uintptr_t nsWinPtr, float x, float y, float w, float h);
extern void metal_video_update_frame(float x, float y, float w, float h);
extern void metal_video_destroy(void);
extern double metal_video_last_fps(void);
extern void metal_video_set_hidden(int hidden);

// Forward declaration matching the CGO-generated export signature (char*, not const char*).
extern void goMetalLog(char *msg, int level);
*/
import "C"

import "github.com/sirupsen/logrus"

// goMetalLog is called from C (metal_video_impl_darwin.m) to log via logrus.
//
//export goMetalLog
func goMetalLog(msg *C.char, level C.int) {
	text := C.GoString(msg)
	switch int(level) {
	case 1:
		logrus.Warnf("[Metal] %s", text)
	case 2:
		logrus.Errorf("[Metal] %s", text)
	default:
		logrus.Infof("[Metal] %s", text)
	}
}

// MetalVideoCreate creates (or replaces) the Metal overlay on the given NSWindow.
// Pass w=0, h=0 to cover the entire contentView (fullscreen mode).
func MetalVideoCreate(nsWin uintptr, x, y, w, h float32) bool {
	return C.metal_video_create(C.uintptr_t(nsWin), C.float(x), C.float(y), C.float(w), C.float(h)) != 0
}

// MetalVideoUpdateFrame repositions the Metal overlay to track the Fyne video widget.
func MetalVideoUpdateFrame(x, y, w, h float32) {
	C.metal_video_update_frame(C.float(x), C.float(y), C.float(w), C.float(h))
}

// MetalVideoDestroy removes the overlay and disables the Metal decode path.
func MetalVideoDestroy() {
	C.metal_video_destroy()
}

// MetalVideoIsActive reports whether the Metal overlay is live.
func MetalVideoIsActive() bool {
	return C.metal_video_is_active() != 0
}

// MetalVideoLastFPS returns the current Metal render FPS from the last ~2s window.
// Returns 0 if not enough frames yet.
func MetalVideoLastFPS() float64 {
	return float64(C.metal_video_last_fps())
}

// MetalVideoSetHidden hides or shows the Metal overlay NSView without destroying it.
// Use this to let Fyne popups/menus render on top of the video.
func MetalVideoSetHidden(hidden bool) {
	h := C.int(0)
	if hidden {
		h = 1
	}
	C.metal_video_set_hidden(h)
}
