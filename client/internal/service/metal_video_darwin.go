//go:build darwin && !ios && cgo

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
extern int  metal_video_get_last_frame_rgba(int *outW, int *outH, uint8_t **out);
extern int  metal_video_next_event(int *type_out, float *x_out, float *y_out, int *btn_out);

// Forward declaration matching the CGO-generated export signature (char*, not const char*).
extern void goMetalLog(char *msg, int level);
*/
import "C"

import (
	"image"
	"unsafe"

	"github.com/sirupsen/logrus"
)

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

// MetalVideoGetLastFrameRGBA returns the last rendered VT frame as an image.RGBA.
// Returns nil if no frame has been rendered yet. Called once on stream stop — cost is acceptable.
func MetalVideoGetLastFrameRGBA() *image.RGBA {
	var w, h C.int
	var ptr *C.uint8_t
	if C.metal_video_get_last_frame_rgba(&w, &h, &ptr) == 0 || ptr == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(ptr))
	width, height := int(w), int(h)
	size := width * height * 4
	src := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), size)
	dst := make([]byte, size)
	copy(dst, src)
	return &image.RGBA{
		Pix:    dst,
		Stride: width * 4,
		Rect:   image.Rect(0, 0, width, height),
	}
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

// MetalVideoNextEvent drains one pending pointer event from the Metal overlay view.
// Returns (type, button, x, y, ok). Types: 1=motion 2=button-press 3=button-release.
// Buttons: 1=left 2=middle 3=right 4=wheel-up 5=wheel-down.
// Coordinates are in NSView points with top-left origin (matches Fyne dp directly).
// Safe to call from any goroutine.
func MetalVideoNextEvent() (typ, button int, x, y float32, ok bool) {
	var t, btn C.int
	var ex, ey C.float
	r := C.metal_video_next_event(&t, &ex, &ey, &btn)
	return int(t), int(btn), float32(ex), float32(ey), r != 0
}
