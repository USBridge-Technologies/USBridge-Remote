//go:build darwin && cgo

package service

// NativeVideoOverlayIsActive reports whether the Metal CALayer overlay is live.
// When true, goVTFrame skips the heavy Go image allocation since the C-level
// vt_callback already forwarded the frame to Metal before calling goVTFrame.
// In practice this will always be false when goVTFrame is reached on macOS
// (Metal intercepts at C level first), but the check is kept for symmetry.
func NativeVideoOverlayIsActive() bool {
	return MetalVideoIsActive()
}
