//go:build windows

package service

// NetGraphSupported reports whether this build has a working Net Graph HUD
// push path -- see net_graph_supported_darwin.go's doc comment for the
// general contract. Windows uses the same in-place CPU-buffer blit as
// Linux (ApplyNetGraphOverlay in net_graph.go, wired at
// moonlight_cgo_windows.go's win_deliver_frame/win_deliver_frame_vulkan).
func NetGraphSupported() bool {
	return true
}
