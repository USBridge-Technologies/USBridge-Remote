//go:build linux && !android && cgo

package service

/*
#include <stdint.h>

// Implemented in vk_video_impl_linux.c (via vk_overlay_common.h).
extern int  vk_aivision_set_pixels(const uint8_t *rgba, int w, int h);
extern void vk_aivision_clear(void);
*/
import "C"

import (
	"unsafe"

	"usbridge-client/internal/localui"
)

// init routes AI Vision detection results to the native Vulkan overlay layer
// so they show up on zero-copy (dma-buf) frames, which have no CPU pixel
// buffer for drawCachedOverlay to draw into. Called once per completed
// detection pass (~0.5-2Hz), never per frame; the GPU upload is lazy on the
// render thread. Same split as ai_vision_windows.go / metal_video_darwin.go.
func init() {
	aiVisionMetalPush = pushAIVisionOverlayToVulkan
	aiVisionMetalClear = func() { C.vk_aivision_clear() }
}

func pushAIVisionOverlayToVulkan(result *localui.Result, w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	img := buildAIVisionOverlayImage(result, w, h)
	C.vk_aivision_set_pixels((*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.int(w), C.int(h))
}
