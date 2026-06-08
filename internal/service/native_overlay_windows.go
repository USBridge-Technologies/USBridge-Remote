//go:build windows && cgo

package service

// NativeVideoOverlayIsActive reports whether the WGL child-window overlay is live.
// When true, goVTFrame skips Go image allocation; the GL render thread already
// received the frame via gl_video_try_submit at the C level.
func NativeVideoOverlayIsActive() bool {
	return GLVideoIsActive()
}
