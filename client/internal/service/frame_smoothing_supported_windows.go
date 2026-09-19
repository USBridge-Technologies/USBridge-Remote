//go:build windows && cgo

package service

// FrameSmoothingSupported reports whether this build has a working frame
// smoothing (motion-extrapolated stall concealment) render path -- see
// frame_smoothing.go's doc comment. Windows/Vulkan only for now (Phase 1,
// RGBA CPU-submit path -- see vk_video_impl_windows.c's "frame smoothing"
// section); other platforms report false until a Metal/GL backend exists.
func FrameSmoothingSupported() bool {
	return true
}
