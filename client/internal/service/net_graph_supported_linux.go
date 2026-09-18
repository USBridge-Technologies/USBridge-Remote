//go:build linux && !android

package service

// NetGraphSupported reports whether this build has a working Net Graph HUD
// push path -- see net_graph_supported_darwin.go's doc comment for the
// general contract. Linux uses the in-place CPU-buffer blit
// (ApplyNetGraphOverlay in net_graph.go, wired at moonlight_cgo_linux.go's
// deliver_frame), mirroring ai_vision.go's drawCachedOverlay rather than a
// native compositor layer -- there's already a CPU-readable RGBA buffer on
// every decoded frame there.
func NetGraphSupported() bool {
	return true
}
