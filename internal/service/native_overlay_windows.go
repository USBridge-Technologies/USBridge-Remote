//go:build windows && cgo

package service

// NativeVideoOverlayIsActive reports whether the Vulkan (or fallback GDI) overlay is live.
// When true, goVTFrame skips Go image allocation; the native render thread already
// received the frame via vk_video_try_submit / gl_video_try_submit at the C level.
func NativeVideoOverlayIsActive() bool {
	return VKVideoIsActive() || GLVideoIsActive()
}
