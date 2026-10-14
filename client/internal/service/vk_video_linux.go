//go:build linux && !android && cgo

package service

/*
#cgo LDFLAGS: -lvulkan -lX11 -lXext

#include <stdint.h>

// Implemented in vk_video_impl_linux.c.
extern int  vk_video_is_active(void);
extern int  vk_video_try_submit(uint8_t *rgba, int width, int height, int stride);
extern int  vk_video_create(uintptr_t parent_xwin, int x, int y, int w, int h);
extern void vk_video_update_frame(int x, int y, int w, int h);
extern void vk_video_destroy(void);
extern void vk_video_get_stats(long long *rendered, long long *submitted,
                               float *fps, int *fps_ready,
                               int *first_frame, int *fw, int *fh,
                               float *max_gap_ms);
extern void vk_video_clear_pending_stats(void);
extern void vk_video_get_diag(long long *hb, int *stage);
extern void vk_video_get_gpu_name(char *out, int out_len);
extern void vk_video_set_hidden(int hidden);
extern void vk_video_set_holes(const int *xywh, int n);
extern int  vk_video_next_event(int *type_out, int *x_out, int *y_out, int *btn_out);
extern void vk_video_get_video_dest(int *dx, int *dy, int *dw, int *dh, int *sw, int *sh);

extern void goVKLog(char *msg, int level);
*/
import "C"

import (
	"unsafe"

	"github.com/sirupsen/logrus"
)

//export goVKLog
func goVKLog(msg *C.char, level C.int) {
	text := C.GoString(msg)
	switch int(level) {
	case 1:
		logrus.Warnf("[Vulkan/Linux] %s", text)
	case 2:
		logrus.Errorf("[Vulkan/Linux] %s", text)
	default:
		logrus.Infof("[Vulkan/Linux] %s", text)
	}
}

func VKVideoIsActive() bool {
	return C.vk_video_is_active() != 0
}

func VKVideoTrySubmit(rgba []byte, width, height, stride int) bool {
	if len(rgba) == 0 {
		return false
	}
	return C.vk_video_try_submit(
		(*C.uint8_t)(unsafe.Pointer(&rgba[0])),
		C.int(width), C.int(height), C.int(stride),
	) != 0
}

func VKVideoCreate(xwin uintptr, x, y, w, h int) bool {
	return C.vk_video_create(C.uintptr_t(xwin), C.int(x), C.int(y), C.int(w), C.int(h)) != 0
}

var vkOverlayLastX, vkOverlayLastY, vkOverlayLastW, vkOverlayLastH int

func VKVideoUpdateFrame(x, y, w, h int) {
	if x == vkOverlayLastX && y == vkOverlayLastY && w == vkOverlayLastW && h == vkOverlayLastH {
		return
	}
	vkOverlayLastX, vkOverlayLastY, vkOverlayLastW, vkOverlayLastH = x, y, w, h
	C.vk_video_update_frame(C.int(x), C.int(y), C.int(w), C.int(h))
}

func VKVideoResetLastFrame() {
	vkOverlayLastX, vkOverlayLastY, vkOverlayLastW, vkOverlayLastH = 0, 0, 0, 0
}

func VKVideoDestroy() {
	C.vk_video_destroy()
}

type VKVideoStats struct {
	Rendered   int64
	Submitted  int64
	FPS        float32
	FPSReady   bool
	FirstFrame bool
	FW, FH     int
	MaxGapMs   float32
}

func VKVideoGetStats() VKVideoStats {
	var r, s C.longlong
	var fp, mgap C.float
	var fpsr, ff, fw, fh C.int
	C.vk_video_get_stats(&r, &s, &fp, &fpsr, &ff, &fw, &fh, &mgap)
	return VKVideoStats{
		Rendered:   int64(r),
		Submitted:  int64(s),
		FPS:        float32(fp),
		FPSReady:   fpsr != 0,
		FirstFrame: ff != 0,
		FW:         int(fw),
		FH:         int(fh),
		MaxGapMs:   float32(mgap),
	}
}

func VKVideoClearPendingStats() {
	C.vk_video_clear_pending_stats()
}

func VKVideoGetDiag() (hb int64, stage int) {
	var h C.longlong
	var s C.int
	C.vk_video_get_diag(&h, &s)
	return int64(h), int(s)
}

// VKVideoGetGPUName returns the active Vulkan physical device's name, for
// Net Graph's GPU line. ok is false before any device has been selected
// (no stream started this session yet).
func VKVideoGetGPUName() (string, bool) {
	buf := make([]C.char, 256)
	C.vk_video_get_gpu_name(&buf[0], C.int(len(buf)))
	name := C.GoString(&buf[0])
	return name, name != ""
}

func VKVideoSetHidden(hidden bool) {
	h := C.int(0)
	if hidden {
		h = 1
	}
	C.vk_video_set_hidden(h)
}

// VKVideoSetHoles cuts rects (overlay-window pixels, [x,y,w,h] each) out of
// the Vulkan child window so Fyne menus underneath show through while the
// rest of the video keeps playing. nil/empty clears every hole.
func VKVideoSetHoles(rects [][4]int) {
	if len(rects) == 0 {
		C.vk_video_set_holes(nil, 0)
		return
	}
	flat := make([]C.int, 0, len(rects)*4)
	for _, r := range rects {
		flat = append(flat, C.int(r[0]), C.int(r[1]), C.int(r[2]), C.int(r[3]))
	}
	C.vk_video_set_holes(&flat[0], C.int(len(rects)))
}

// VKVideoNextEvent drains one pending pointer event from the Vulkan overlay window.
// Returns (type, x, y, button, ok). Types: 1=motion 2=button-press 3=button-release.
// Buttons: 1=left 2=middle 3=right 4=wheel-up 5=wheel-down.
// Safe to call from any goroutine (XInitThreads already active via GLFW).
func VKVideoNextEvent() (typ, x, y, button int, ok bool) {
	var t, ex, ey, btn C.int
	r := C.vk_video_next_event(&t, &ex, &ey, &btn)
	return int(t), int(ex), int(ey), int(btn), r != 0
}

func nativeVideoDestRect() (NativeVideoDest, bool) {
	var dx, dy, dw, dh, sw, sh C.int
	C.vk_video_get_video_dest(&dx, &dy, &dw, &dh, &sw, &sh)
	if int(dw) <= 0 || int(dh) <= 0 {
		return NativeVideoDest{}, false
	}
	return NativeVideoDest{DX: int(dx), DY: int(dy), DW: int(dw), DH: int(dh), SW: int(sw), SH: int(sh)}, true
}
