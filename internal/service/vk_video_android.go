//go:build android && cgo

package service

/*
#cgo LDFLAGS: -lvulkan -landroid

#include <stdint.h>
#include <jni.h>

// Implemented in vk_video_impl_android.c.
extern void android_vk_set_jvm(JavaVM *jvm, jobject ctx);
extern int  android_vk_is_active(void);
extern int  android_vk_is_hidden(void);
extern int  android_vk_create(int x, int y, int w, int h);
extern int  android_vk_try_submit(uint8_t *rgba, int width, int height, int stride);
extern void android_vk_update_rect(int x, int y, int w, int h);
extern void android_vk_set_hidden(int hidden);
extern void android_vk_destroy(void);
extern void android_vk_force_recreate_swapchain(void);
extern void android_vk_set_viewport(float u0, float v0, float u1, float v1);
extern void android_vk_set_cursor(float uc, float vc, int visible);
extern void android_vk_set_cursor_scale(int scale);
*/
import "C"

import (
	"unsafe"
)

// VKVideoAndroidSetJVM caches the JVM pointer so the Vulkan renderer can call
// VulkanOverlayBridge Kotlin methods from native threads.
func VKVideoAndroidSetJVM(jvm uintptr, ctx uintptr) {
	C.android_vk_set_jvm((*C.JavaVM)(unsafe.Pointer(jvm)), (C.jobject)(unsafe.Pointer(ctx)))
}

func VKVideoAndroidIsActive() bool {
	return C.android_vk_is_active() != 0
}

func VKVideoAndroidIsHidden() bool {
	return C.android_vk_is_hidden() != 0
}

// VKVideoAndroidCreate requests a SurfaceView overlay from Kotlin, waits for
// the surface, and initialises the Vulkan renderer. Returns false on failure.
func VKVideoAndroidCreate(x, y, w, h int) bool {
	return C.android_vk_create(C.int(x), C.int(y), C.int(w), C.int(h)) != 0
}

func VKVideoAndroidTrySubmit(rgba []byte, width, height, stride int) bool {
	if len(rgba) == 0 {
		return false
	}
	return C.android_vk_try_submit(
		(*C.uint8_t)(unsafe.Pointer(&rgba[0])),
		C.int(width), C.int(height), C.int(stride),
	) != 0
}

var vkAndroidLastX, vkAndroidLastY, vkAndroidLastW, vkAndroidLastH int

func VKVideoAndroidUpdateRect(x, y, w, h int) {
	if x == vkAndroidLastX && y == vkAndroidLastY &&
		w == vkAndroidLastW && h == vkAndroidLastH {
		return
	}
	vkAndroidLastX, vkAndroidLastY, vkAndroidLastW, vkAndroidLastH = x, y, w, h
	C.android_vk_update_rect(C.int(x), C.int(y), C.int(w), C.int(h))
}

func VKVideoAndroidSetHidden(hidden bool) {
	h := C.int(0)
	if hidden {
		h = 1
	}
	C.android_vk_set_hidden(h)
}

func VKVideoAndroidDestroy() {
	C.android_vk_destroy()
}

// VKVideoAndroidForceRecreateSwapchain signals the Vulkan render thread to
// recreate the swapchain on its next iteration. Use after fullscreen transitions
// to pick up the updated surface dimensions without waiting for the proactive
// size-change detection in vk_render_frame.
func VKVideoAndroidForceRecreateSwapchain() {
	C.android_vk_force_recreate_swapchain()
}

// VKVideoAndroidSetViewport sets the visible UV sub-rect of the video frame.
// u0,v0 = top-left corner; u1,v1 = bottom-right corner; all in [0,1].
// Pass 0,0,1,1 for the full frame (default).
func VKVideoAndroidSetViewport(u0, v0, u1, v1 float32) {
	C.android_vk_set_viewport(C.float(u0), C.float(v0), C.float(u1), C.float(v1))
}

// VKVideoAndroidSetCursor sets the virtual cursor position (uc, vc in frame UV)
// and whether to render it. The cursor appears on top of the video each frame.
func VKVideoAndroidSetCursor(uc, vc float32, visible bool) {
	vis := C.int(0)
	if visible {
		vis = 1
	}
	C.android_vk_set_cursor(C.float(uc), C.float(vc), vis)
}

// VKVideoAndroidSetCursorScale reinitialises the cursor bitmap at the given
// integer pixel scale (1=18×24, 2=36×48, 3=54×72, 4=72×96 physical pixels).
func VKVideoAndroidSetCursorScale(scale int) {
	C.android_vk_set_cursor_scale(C.int(scale))
}
