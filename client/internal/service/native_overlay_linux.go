//go:build linux && !android && cgo

package service

// NativeVideoOverlayIsActive reports whether the Vulkan or GLX/X11 overlay is live.
func NativeVideoOverlayIsActive() bool {
	return VKVideoIsActive() || GLVideoIsActive()
}
