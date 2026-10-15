//go:build android && cgo

package service

/*
#include <stdint.h>

extern void     do_get_rtp_video_stats(uint32_t *out);
extern int      do_get_estimated_rtt_info(uint32_t *out);
extern uint16_t do_get_last_host_latency_tenths_ms(void);
extern uint64_t do_get_playout_jitter_us(void);
extern uint64_t do_get_playout_applied_delay_us(void);
extern double   vlat_last_decode_ms(void); // video_latency_android.c
extern int      do_get_negotiated_video_format(void);

// vk_video_impl_android.c (vk_overlay_common.h): the overlay's own HUD
// layer, drawn by both its Vulkan and GLES backends.
extern int  vk_hud_set_pixels(const uint8_t *rgba, int w, int h);
extern void vk_hud_clear(void);
extern void vk_hud_set_scale(float s);
*/
import "C"

import (
	"image"
	"unsafe"
)

// RTPVideoStats / GetRTPVideoStats / GetEstimatedRttInfo /
// GetLastHostLatencyMs / GetPlayoutJitterMs / GetPlayoutAppliedDelayMs:
// Android counterparts to moonlight_cgo_wrapper.go's identically-named
// declarations (that file's build tag excludes android). The C functions
// they call live in net_graph_stats_android.c (see that file's header
// comment for why they aren't inline in moonlight_cgo_android.go).
type RTPVideoStats struct {
	PacketCountVideo        uint32
	PacketCountFec          uint32
	PacketCountFecRecovered uint32
	PacketCountFecFailed    uint32
	PacketCountOOS          uint32
	PacketCountInvalid      uint32
	PacketCountFecInvalid   uint32
}

func GetRTPVideoStats() RTPVideoStats {
	var raw [7]C.uint32_t
	C.do_get_rtp_video_stats(&raw[0])
	return RTPVideoStats{
		PacketCountVideo:        uint32(raw[0]),
		PacketCountFec:          uint32(raw[1]),
		PacketCountFecRecovered: uint32(raw[2]),
		PacketCountFecFailed:    uint32(raw[3]),
		PacketCountOOS:          uint32(raw[4]),
		PacketCountInvalid:      uint32(raw[5]),
		PacketCountFecInvalid:   uint32(raw[6]),
	}
}

func GetEstimatedRttInfo() (rttMs, rttVarianceMs float64, ok bool) {
	var raw [2]C.uint32_t
	got := C.do_get_estimated_rtt_info(&raw[0])
	if got == 0 {
		return 0, 0, false
	}
	return float64(raw[0]), float64(raw[1]), true
}

func GetLastHostLatencyMs() (ms float64, valid bool) {
	tenths := uint16(C.do_get_last_host_latency_tenths_ms())
	return float64(tenths) / 10.0, true
}

func GetPlayoutJitterMs() float64 {
	return float64(uint64(C.do_get_playout_jitter_us())) / 1000.0
}

func GetPlayoutAppliedDelayMs() float64 {
	return float64(uint64(C.do_get_playout_applied_delay_us())) / 1000.0
}

func init() {
	// Same role as net_graph_linux.go: HUD FPS comes from the native
	// overlay that is actually painting the video (Vulkan here). DEC is
	// video_latency_android.c's decoder-submit -> present average (the
	// same span Metal's g_decodeMsSum covers), "--" until its first 2 s
	// window.
	netGraphRenderFPS = func() float64 {
		return VKVideoAndroidGetFPS()
	}
	netGraphDecodeMs = func() float64 {
		return float64(C.vlat_last_decode_ms())
	}
	netGraphDecodeMsValid = func() bool {
		return C.vlat_last_decode_ms() > 0
	}
	netGraphCodecFn = func() (string, bool) {
		return videoFormatCodecName(int32(C.do_get_negotiated_video_format()))
	}
	netGraphGPUNameFn = VKVideoAndroidGetGPUName
	// The HUD is a layer of the video overlay, not a burn-in, so Net Graph
	// keeps the zero-copy decoder path (see amc_present_image).
	netGraphMetalPush = func(img *image.RGBA) {
		if img == nil || len(img.Pix) == 0 {
			return
		}
		C.vk_hud_set_pixels((*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.int(img.Rect.Dx()), C.int(img.Rect.Dy()))
	}
	netGraphMetalClear = func() { C.vk_hud_clear() }
	netGraphScalePush = func(scale float32) { C.vk_hud_set_scale(C.float(scale)) }
	netGraphNetworkStatsFn = func() netGraphRawNetworkStats {
		rtp := GetRTPVideoStats()
		rttMs, rttVarianceMs, rttOk := GetEstimatedRttInfo()
		hostLatencyMs, hostLatencyOk := GetLastHostLatencyMs()
		return netGraphRawNetworkStats{
			PacketCountVideo:        rtp.PacketCountVideo,
			PacketCountFec:          rtp.PacketCountFec,
			PacketCountFecRecovered: rtp.PacketCountFecRecovered,
			PacketCountFecFailed:    rtp.PacketCountFecFailed,
			PacketCountOOS:          rtp.PacketCountOOS,
			PacketCountInvalid:      rtp.PacketCountInvalid,
			RTTMs:                   rttMs,
			RTTVarianceMs:           rttVarianceMs,
			RTTValid:                rttOk,
			HostLatencyMs:           hostLatencyMs,
			HostLatencyValid:        hostLatencyOk,
			JitterMs:                GetPlayoutJitterMs(),
			PlayoutDelayMs:          GetPlayoutAppliedDelayMs(),
		}
	}
}

// goNetGraphOverlay burns the HUD into a CPU frame; only for the Fyne
// fallback, when there is no native overlay to draw it.
//
//export goNetGraphOverlay
func goNetGraphOverlay(rgba *C.uint8_t, width, height, stride C.int) {
	if rgba == nil || width <= 0 || height <= 0 || stride <= 0 {
		return
	}
	w, h, s := int(width), int(height), int(stride)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(rgba)), s*h)
	ApplyNetGraphOverlay(buf, w, h, s, false)
}
