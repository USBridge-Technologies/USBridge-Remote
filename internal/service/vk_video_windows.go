//go:build windows && cgo

package service

/*
#cgo CFLAGS: -I/ucrt64/include
#cgo LDFLAGS: -L/ucrt64/lib -lvulkan-1 -lgdi32 -luser32

#include <stdint.h>

// Implemented in vk_video_impl_windows.c.
extern int  vk_video_is_active(void);
extern int  vk_video_try_submit(uint8_t *rgba, int width, int height, int stride);
extern int  vk_video_create(uintptr_t parent_hwnd, int x, int y, int w, int h);
extern void vk_video_update_frame(int x, int y, int w, int h);
extern void vk_video_destroy(void);
extern void vk_video_get_stats(long long *rendered, long long *submitted,
                               float *fps, int *fps_ready,
                               int *first_frame, int *fw, int *fh,
                               float *max_gap_ms);
extern void vk_video_clear_pending_stats(void);
extern void vk_video_get_diag(long long *hb, int *stage);
extern void vk_video_set_hidden(int hidden);
extern void vk_video_bring_to_top(void);
extern int  vk_video_next_event(int *type_out, int *x_out, int *y_out, int *btn_out);
extern int  vk_video_create_standalone(void);
extern int  vk_video_next_key_event(int *type_out, int *vk_out);

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
		logrus.Warnf("[Vulkan/Win] %s", text)
	case 2:
		logrus.Errorf("[Vulkan/Win] %s", text)
	default:
		logrus.Infof("[Vulkan/Win] %s", text)
	}
}

func VKVideoIsActive() bool {
	return C.vk_video_is_active() != 0
}

// VKVideoTrySubmit submits an RGBA frame to the Vulkan render thread.
func VKVideoTrySubmit(rgba []byte, width, height, stride int) bool {
	if len(rgba) == 0 {
		return false
	}
	return C.vk_video_try_submit(
		(*C.uint8_t)(unsafe.Pointer(&rgba[0])),
		C.int(width), C.int(height), C.int(stride),
	) != 0
}

// VKVideoCreate initialises the Vulkan child-window renderer.
// Returns false if Vulkan is unavailable; caller should fall back to GDI.
func VKVideoCreate(hwnd uintptr, x, y, w, h int) bool {
	return C.vk_video_create(C.uintptr_t(hwnd), C.int(x), C.int(y), C.int(w), C.int(h)) != 0
}

var vkOverlayLastX, vkOverlayLastY, vkOverlayLastW, vkOverlayLastH int

// VKVideoUpdateFrame repositions the child HWND overlay.
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

// VKVideoStats mirrors GLVideoStats for the Vulkan render thread.
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

// VKVideoGetDiag returns the render-thread heartbeat counter and current stage code.
// Stage codes: 0=idle 1=got-frame 2=staging 3=acquire 4=fence-wait 5=queue-submit 6=present 7=recreate
func VKVideoGetDiag() (hb int64, stage int) {
	var h C.longlong
	var s C.int
	C.vk_video_get_diag(&h, &s)
	return int64(h), int(s)
}

// VKVideoBringToTop re-asserts HWND_TOPMOST on the Vulkan overlay window.
// Must be called after any RequestFocus/glfwFocusWindow that can promote the Fyne
// window above the overlay inside the TOPMOST Z-order layer.
func VKVideoBringToTop() {
	C.vk_video_bring_to_top()
}

// VKVideoSetHidden hides (hidden=true) or shows (hidden=false) the overlay without
// destroying it. Called when a Fyne menu/popup opens or closes so the native overlay
// doesn't paint over Fyne's own UI — mirrors macOS MetalVideoSetHidden.
func VKVideoSetHidden(hidden bool) {
	h := C.int(0)
	if hidden {
		h = 1
	}
	C.vk_video_set_hidden(h)
}

// VKVideoCreateStandalone creates a standalone fullscreen Vulkan window covering the
// primary monitor. No parent HWND needed; the window captures keyboard focus directly.
// Use this instead of VKVideoCreate when entering fullscreen without a Fyne window.
func VKVideoCreateStandalone() bool {
	return C.vk_video_create_standalone() != 0
}

// VKVideoNextKeyEvent drains one pending keyboard event from the standalone VK window.
// Returns (type, vkCode, ok). type: 1=keydown, 2=keyup. vkCode is a Win32 Virtual Key.
// Safe to call from any goroutine.
func VKVideoNextKeyEvent() (typ, vkCode int, ok bool) {
	var t, v C.int
	r := C.vk_video_next_key_event(&t, &v)
	return int(t), int(v), r != 0
}

// VKVideoNextEvent drains one pending pointer event from the Vulkan overlay window.
// Returns (type, x, y, button, ok). Types: 1=motion 2=button-press 3=button-release.
// Buttons: 1=left 2=middle 3=right 4=wheel-up 5=wheel-down.
// Safe to call from any goroutine.
func VKVideoNextEvent() (typ, x, y, button int, ok bool) {
	var t, ex, ey, btn C.int
	r := C.vk_video_next_event(&t, &ex, &ey, &btn)
	return int(t), int(ex), int(ey), int(btn), r != 0
}
