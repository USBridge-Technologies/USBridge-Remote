//go:build windows && cgo

package service

/*
#include <stdint.h>

extern void     do_get_rtp_video_stats(uint32_t *out);
extern int      do_get_estimated_rtt_info(uint32_t *out);
extern uint16_t do_get_last_host_latency_tenths_ms(void);
extern uint64_t do_get_playout_jitter_us(void);
extern uint64_t do_get_playout_applied_delay_us(void);
extern double   win_get_last_decode_ms(void);

extern int  vk_hud_set_pixels(const uint8_t *rgba, int w, int h);
extern void vk_hud_clear(void);
*/
import "C"

import (
	"image"
	"os"
	"unsafe"
)

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

// See moonlight_cgo_wrapper.go's identically-named function for why 0 is
// treated as a genuine measurement (valid always true) rather than hidden.
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

// GetDecodeMs returns the most recent win_deliver_frame/
// win_deliver_frame_vulkan call's wall time (moonlight_cgo_windows.go) --
// decode plus, on the zero-copy path, the HUD/AI-Vision overlay draw calls
// and submit. The closest Windows equivalent to
// metal_video_impl_darwin.m's "submit-to-display" decode latency stat on
// macOS. 0 before the first frame.
func GetDecodeMs() float64 {
	return float64(C.win_get_last_decode_ms())
}

// init wires net_graph.go's platform-agnostic hooks to the getters above
// (network stats, decode latency) and to VKVideoGetStats/GLVideoGetStats
// (render fps) -- same "core stays tag-free, platform files wire the hooks"
// split as metal_video_darwin.go's init() for macOS.
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
	netGraphDecodeMs = GetDecodeMs
	// Frame smoothing's running synthesized-frame count (see
	// frame_smoothing_windows.go) -- netGraphDrawConcealedMarkers turns this
	// into the purple dots on the RTT graph. Reads 0 (a no-op diff) whenever
	// the feature is off, since ConcealedFrames only ever increments while
	// SetFrameSmoothingEnabled(true) is active.
	netGraphConcealedFramesFn = func() int64 { return GetFrameSmoothingStats().ConcealedFrames }
	// Native Vulkan HUD compositor layer (vk_hud_record_draw in
	// vk_video_impl_windows.c) -- see pushNetGraphOverlayToVulkan's doc
	// comment for why this exists instead of the CPU-buffer
	// ApplyNetGraphOverlay path used by win_deliver_frame's non-zero-copy
	// branches.
	netGraphMetalPush = pushNetGraphOverlayToVulkan
	netGraphMetalClear = vulkanClearHudOverlay

	// USBRIDGE_NET_GRAPH=1: force the HUD on at startup, same debug/QA aid
	// as frame_smoothing_windows.go's USBRIDGE_FRAME_SMOOTHING -- lets a
	// deep-link-launched build show the concealed-frame purple dots without
	// clicking the checkbox.
	if os.Getenv("USBRIDGE_NET_GRAPH") == "1" {
		SetNetGraphEnabled(true)
	}
}

// pushNetGraphOverlayToVulkan hands the freshly built HUD canvas straight to
// vk_video_impl_windows.c's native Vulkan overlay layer (vk_hud_set_pixels),
// instead of relying on win_deliver_frame_vulkan's zero-copy decode path to
// route it through a CPU-readable RGBA buffer -- that path deliberately
// never produces one (see its own doc comment in moonlight_cgo_windows.go),
// so without this, the HUD would only ever show up when Vulkan hardware
// decode ISN'T in use. Called from net_graph.go's netGraphLoop at ~10Hz, not
// once per rendered video frame -- vk_hud_set_pixels just copies into a
// pending buffer and returns, the actual GPU upload happens lazily on the
// render thread's next frame (vk_video_impl_windows.c's
// vk_hud_maybe_upload_cmds), so this is cheap to call from this goroutine.
func pushNetGraphOverlayToVulkan(img *image.RGBA) {
	if img == nil || len(img.Pix) == 0 {
		return
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	C.vk_hud_set_pixels((*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.int(w), C.int(h))
}

// vulkanClearHudOverlay is netGraphMetalClear's Windows/Vulkan counterpart --
// called when Net Graph is disabled so the render thread stops drawing the
// (now stale) HUD texture. See vk_hud_clear's doc comment.
func vulkanClearHudOverlay() {
	C.vk_hud_clear()
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

// goNetGraphOverlay is the Windows counterpart to moonlight_cgo_wrapper.go's
// identically-named export (that file's build tag excludes windows) --
// called from win_deliver_frame in moonlight_cgo_windows.go, right next to
// goAIVisionOverlay's call site, on both the RGBA buffer (D3D11VA->RGBA
// path) and the BGRA one (GDI-fallback path, see gl_video_impl_windows.c) --
// bgr tells ApplyNetGraphOverlay which byte order it's compositing into so
// the HUD's colors come out right on both. Previously the GDI-fallback call
// site skipped this entirely to dodge the color-swap, which meant the HUD
// never rendered on that path.
//
// NOT called from win_deliver_frame_vulkan (hardware Vulkan Video Decode's
// zero-copy path) -- that path's HUD is composited natively instead, via
// pushNetGraphOverlayToVulkan/vulkanClearHudOverlay below, straight into
// vk_video_impl_windows.c's render pass with no CPU readback of the video
// frame. See that path's own doc comment in moonlight_cgo_windows.go.
//
//export goNetGraphOverlay
func goNetGraphOverlay(rgba *C.uint8_t, width, height, stride, bgr C.int) {
	if rgba == nil || width <= 0 || height <= 0 || stride <= 0 {
		return
	}
	w, h, s := int(width), int(height), int(stride)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(rgba)), s*h)
	ApplyNetGraphOverlay(buf, w, h, s, bgr != 0)
}
