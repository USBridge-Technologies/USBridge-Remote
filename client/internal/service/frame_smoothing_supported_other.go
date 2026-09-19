//go:build !(windows && cgo)

package service

// FrameSmoothingSupported: see frame_smoothing_supported_windows.go's doc
// comment. Not yet implemented outside Windows/Vulkan.
func FrameSmoothingSupported() bool {
	return false
}
