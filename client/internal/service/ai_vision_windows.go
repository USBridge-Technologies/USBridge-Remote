//go:build windows && cgo

package service

/*
#include <stdint.h>

extern int  vk_aivision_set_pixels(const uint8_t *rgba, int w, int h);
extern void vk_aivision_clear(void);
*/
import "C"

import (
	"unsafe"

	"usbridge-client/internal/localui"
)

// init wires ai_vision.go's platform-agnostic detection loop to Windows's
// native Vulkan overlay layer (vk_aivision_record_draw in
// vk_video_impl_windows.c) -- same "core stays cgo-free, thin platform push
// hook" split metal_video_darwin.go's init() uses for macOS's Metal
// compositor, and net_graph_windows.go's init() uses for the Net Graph HUD.
func init() {
	aiVisionMetalPush = pushAIVisionOverlayToVulkan
	aiVisionMetalClear = vulkanClearAIVisionOverlay
}

// pushAIVisionOverlayToVulkan renders a just-completed AI Vision detection
// result onto a transparent w×h canvas (buildAIVisionOverlayImage, shared
// with macOS's identical push) and hands it to vk_video_impl_windows.c's
// native overlay texture. Called once per completed detection pass (every
// aiVisionIconInterval/aiVisionOCRInterval, see ai_vision.go's package doc
// comment), not per frame -- unlike drawCachedOverlay's in-place pixel
// writes, which only run on win_deliver_frame's non-zero-copy branches (see
// goAIVisionOverlay's doc comment in moonlight_cgo_windows.go) and never on
// win_deliver_frame_vulkan's hardware decode path, which this exists for.
func pushAIVisionOverlayToVulkan(result *localui.Result, w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	img := buildAIVisionOverlayImage(result, w, h)
	C.vk_aivision_set_pixels((*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.int(w), C.int(h))
}

// vulkanClearAIVisionOverlay is aiVisionMetalClear's Windows/Vulkan
// counterpart -- called when AI Vision is disabled so the render thread
// stops drawing the (now stale) detection boxes. See vk_aivision_clear's doc
// comment.
func vulkanClearAIVisionOverlay() {
	C.vk_aivision_clear()
}
