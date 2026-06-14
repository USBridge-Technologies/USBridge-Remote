//go:build linux && !android && cgo

package service

/*
#cgo LDFLAGS: -lvulkan -lX11

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
extern void vk_video_set_hidden(int hidden);

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

func VKVideoSetHidden(hidden bool) {
	h := C.int(0)
	if hidden {
		h = 1
	}
	C.vk_video_set_hidden(h)
}
