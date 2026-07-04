//go:build ios && cgo

package service

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework UIKit -framework CoreVideo -framework QuartzCore -framework CoreFoundation

#include <stdint.h>
#include <CoreVideo/CoreVideo.h>

// Implemented in metal_video_impl_ios.m (compiled as a separate translation unit).
extern int    metal_video_is_active(void);
extern int    metal_video_try_submit(CVImageBufferRef img);
extern int    metal_video_create(uintptr_t unused, float x, float y, float w, float h);
extern void   metal_video_update_frame(float x, float y, float w, float h);
extern void   metal_video_update_cursor(float x, float y, int visible);
extern void   metal_video_set_cursor_image(uint8_t *pixels, int w, int h);
extern void   metal_video_destroy(void);
extern double metal_video_last_fps(void);
extern void   metal_video_set_hidden(int hidden);

// Forward declaration matching the CGO-generated export signature.
extern void goMetalLog(char *msg, int level);
*/
import "C"

import (
	"unsafe"

	"github.com/sirupsen/logrus"
)

// goMetalLog is called from C (metal_video_impl_ios.m) to log via logrus.
//
//export goMetalLog
func goMetalLog(msg *C.char, level C.int) {
	text := C.GoString(msg)
	switch int(level) {
	case 1:
		logrus.Warnf("[Metal/iOS] %s", text)
	case 2:
		logrus.Errorf("[Metal/iOS] %s", text)
	default:
		logrus.Infof("[Metal/iOS] %s", text)
	}
}

// MetalVideoCreate creates the Metal CALayer overlay on the key UIWindow.
// Pass w=0, h=0 to cover the entire window. The first argument is unused on iOS
// (the UIWindow is found internally via UIApplication).
func MetalVideoCreate(_ uintptr, x, y, w, h float32) bool {
	return C.metal_video_create(0, C.float(x), C.float(y), C.float(w), C.float(h)) != 0
}

// MetalVideoUpdateFrame repositions the Metal overlay to track the Fyne video widget.
func MetalVideoUpdateFrame(x, y, w, h float32) {
	C.metal_video_update_frame(C.float(x), C.float(y), C.float(w), C.float(h))
}

// MetalVideoUpdateCursor updates the position and visibility of the Metal overlay cursor.
func MetalVideoUpdateCursor(x, y float32, visible bool) {
	vis := C.int(0)
	if visible {
		vis = 1
	}
	C.metal_video_update_cursor(C.float(x), C.float(y), vis)
}

// MetalVideoSetCursorImage uploads raw NRGBA pixels (R,G,B,A per pixel) to the Metal
// cursor layer. Call this once after creating the overlay to replace the default circle
// with a proper arrow cursor. The C function copies the bytes immediately so the Go
// slice may be GC'd after this call returns.
func MetalVideoSetCursorImage(pix []byte, w, h int) {
	if len(pix) == 0 || w <= 0 || h <= 0 {
		return
	}
	C.metal_video_set_cursor_image((*C.uint8_t)(unsafe.Pointer(&pix[0])), C.int(w), C.int(h))
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
func MetalVideoLastFPS() float64 {
	return float64(C.metal_video_last_fps())
}

// MetalVideoSetHidden hides or shows the Metal overlay UIView without destroying it.
// Use this to let Fyne popups/menus render on top of the video.
func MetalVideoSetHidden(hidden bool) {
	h := C.int(0)
	if hidden {
		h = 1
	}
	C.metal_video_set_hidden(h)
}
