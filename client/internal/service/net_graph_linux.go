//go:build linux && !android && cgo

package service

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
func init() {
	netGraphRenderFPS = netGraphLinuxNativeFPS
}

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
