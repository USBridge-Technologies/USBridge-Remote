//go:build linux && !android && cgo

package service

/*
#include <stdint.h>

// Implemented in vk_video_impl_linux.c (via vk_overlay_common.h).
extern int  vk_hud_set_pixels(const uint8_t *rgba, int w, int h);
extern void vk_hud_clear(void);
extern void vk_hud_set_scale(float s);
extern void gl_video_hud_set_pixels(const uint8_t *rgba, int w, int h, float scale);
extern void gl_video_hud_clear(void);
*/
import "C"

import (
	"image"
	"sync/atomic"
	"unsafe"

	"github.com/sirupsen/logrus"
)

// init wires the Net Graph HUD's render-fps hook (net_graph.go) to
// whichever native overlay is actually active -- VKVideoGetStats/
// GLVideoGetStats (vk_video_linux.go/gl_video_linux.go) already track this
// for the VideoWidget FPS badge, so this just reads the same counters.
// netGraphNetworkStatsFn is already wired for Linux by
// moonlight_cgo_wrapper.go's own init() (that file's build tag includes
// linux) -- nothing to add here for RTT/loss/FEC/jitter.
//
// netGraphDecodeMs is left nil: unlike macOS/iOS's Metal path (which times
// submit-to-display directly, see metal_video_impl_darwin.m's
// g_decodeMsSum), the VK/GL CPU-buffer path has no equivalent per-frame
// timer today -- buildNetGraphHUD's DEC row simply reads 0 here rather than
// a fabricated number.
//
// netGraphMetalPush/Clear/ScalePush feed the native Vulkan HUD layer: the
// zero-copy VAAPI/QSV dma-buf path (vk_video_impl_linux.c) never produces a
// CPU pixel buffer, so ApplyNetGraphOverlay's burn-in can't reach it and the
// HUD is composited on the GPU as a second alpha-blended draw instead. The GL
// renderer's NV12 path does the same (gl_video_hud_set_pixels); on the RGBA path
// the pixels sit unused and the CPU burn-in draws.
func init() {
	netGraphRenderFPS = netGraphLinuxNativeFPS
	benchRenderedFramesFn = netGraphLinuxRenderedFrames
	netGraphMetalPush = pushNetGraphOverlayToVulkan
	netGraphMetalClear = func() {
		C.vk_hud_clear()
		C.gl_video_hud_clear()
	}
	netGraphScalePush = func(scale float32) { C.vk_hud_set_scale(C.float(scale)) }
	// GPU name: only the Vulkan path tracks a physical device (g_pdev in
	// vk_video_impl_linux.c) -- the GL path has no equivalent, so this
	// reads "--" there, same "no data yet" convention as netGraphDecodeMs
	// above.
	netGraphGPUNameFn = func() (string, bool) {
		if !VKVideoIsActive() {
			return "", false
		}
		return VKVideoGetGPUName()
	}
}

func pushNetGraphOverlayToVulkan(img *image.RGBA) {
	if img == nil || len(img.Pix) == 0 {
		return
	}
	C.gl_video_hud_set_pixels((*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.int(img.Rect.Dx()), C.int(img.Rect.Dy()), C.float(NetGraphScalePercent())/100)
	rc := C.vk_hud_set_pixels((*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.int(img.Rect.Dx()), C.int(img.Rect.Dy()))
	if n := netGraphVkPushCount.Add(1); n == 1 {
		logrus.Infof("[Net Graph/Vulkan] push #%d rc=%d img=%dx%d", n, int(rc), img.Rect.Dx(), img.Rect.Dy())
	}
}

var netGraphVkPushCount atomic.Int64

func netGraphLinuxNativeFPS() float64 {
	if VKVideoIsActive() {
		if st := VKVideoGetStats(); st.FPSReady {
			return float64(st.FPS)
		}
	}
	if GLVideoIsActive() {
		if st := GLVideoGetStats(); st.FPSReady {
			return float64(st.FPS)
		}
	}
	return 0
}

// netGraphLinuxRenderedFrames is the active renderer's running count of
// presented frames, for the streamer benchmark's client-side fps.
func netGraphLinuxRenderedFrames() (int64, bool) {
	if VKVideoIsActive() {
		return VKVideoGetStats().Rendered, true
	}
	if GLVideoIsActive() {
		return GLVideoGetStats().Rendered, true
	}
	return 0, false
}
