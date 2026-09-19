//go:build android

package service

// NetGraphSupported reports whether this build has a working Net Graph HUD
// push path -- see net_graph_supported_darwin.go's doc comment. Android
// uses the CPU-buffer blit (ApplyNetGraphOverlay in net_graph.go), wired at
// moonlight_cgo_android.go's dr_submit: when the HUD is on, that path
// skips AHardwareBuffer zero-copy for that frame so there is a CPU-readable
// RGBA buffer to composite into (see net_graph_android.go).
func NetGraphSupported() bool {
	return true
}
