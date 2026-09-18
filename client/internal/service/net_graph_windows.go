//go:build windows && cgo

package service

/*
#include <stdint.h>

extern void     do_get_rtp_video_stats(uint32_t *out);
extern int      do_get_estimated_rtt_info(uint32_t *out);
extern uint16_t do_get_last_host_latency_tenths_ms(void);
extern uint64_t do_get_playout_jitter_us(void);
extern uint64_t do_get_playout_applied_delay_us(void);
*/
import "C"

import "unsafe"

// RTPVideoStats / GetRTPVideoStats / GetEstimatedRttInfo /
// GetLastHostLatencyMs / GetPlayoutJitterMs / GetPlayoutAppliedDelayMs:
// Windows counterparts to moonlight_cgo_wrapper.go's identically-named
// declarations (that file's build tag excludes windows -- see its own doc
// comment). The C functions they call are defined in moonlight_cgo_windows.go,
// right next to dr_submit, duplicating moonlight_cgo_shared.h's bodies
// verbatim since that header can't be shared with Windows's fully
// self-contained do_li_start.
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
	if tenths == 0 {
		return 0, false
	}
	return float64(tenths) / 10.0, true
}

func GetPlayoutJitterMs() float64 {
	return float64(uint64(C.do_get_playout_jitter_us())) / 1000.0
}

func GetPlayoutAppliedDelayMs() float64 {
	return float64(uint64(C.do_get_playout_applied_delay_us())) / 1000.0
}

// init wires net_graph.go's platform-agnostic hooks to the getters above
// (network stats) and to VKVideoGetStats/GLVideoGetStats (render fps) --
// same "core stays tag-free, platform files wire the hooks" split as
// metal_video_darwin.go's init() for macOS.
//
// netGraphDecodeMs is left nil: see net_graph_linux.go's identical note --
// no per-frame submit-to-display timer exists on the VK/GL CPU-buffer path.
func init() {
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
	netGraphRenderFPS = netGraphWindowsNativeFPS
}

func netGraphWindowsNativeFPS() float64 {
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

// goNetGraphActive lets win_deliver_frame_vulkan's zero-copy path decide
// whether it's worth paying for a GPU->CPU readback + sws_scale at all when
// AI Vision is off -- mirrors goAIVisionActive exactly (see that function's
// doc comment in moonlight_cgo_windows.go).
//
//export goNetGraphActive
func goNetGraphActive() C.int {
	if NetGraphEnabled() {
		return 1
	}
	return 0
}

// goNetGraphOverlay is the Windows counterpart to moonlight_cgo_wrapper.go's
// identically-named export (that file's build tag excludes windows) --
// called from win_deliver_frame/win_deliver_frame_vulkan in
// moonlight_cgo_windows.go, right next to goAIVisionOverlay's call sites,
// on the same genuine CPU-readable RGBA buffer.
//
//export goNetGraphOverlay
func goNetGraphOverlay(rgba *C.uint8_t, width, height, stride C.int) {
	if rgba == nil || width <= 0 || height <= 0 || stride <= 0 {
		return
	}
	w, h, s := int(width), int(height), int(stride)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(rgba)), s*h)
	ApplyNetGraphOverlay(buf, w, h, s)
}
