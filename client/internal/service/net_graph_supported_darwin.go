//go:build darwin && !ios

package service

// NetGraphSupported reports whether this build has a working Net Graph HUD
// push path (see metal_video_darwin.go's netGraphMetalPush wiring) --
// video_start_dialog.go uses this to hide the checkbox entirely on
// platforms without one yet, rather than shipping a toggle that does
// nothing. macOS only for now; see the plan's Phase 3 for the Linux/Windows
// port, which would add its own NetGraphSupported build variant once the
// in-place CPU-buffer push (drawNetGraphOverlay, mirroring ai_vision.go's
// drawCachedOverlay) actually exists there.
func NetGraphSupported() bool {
	return true
}
