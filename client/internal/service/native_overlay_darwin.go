//go:build darwin && !ios && cgo

package service

// NativeVideoOverlayIsActive reports whether the Metal CALayer overlay is live.
// When true, goVTFrame skips the heavy Go image allocation since the C-level
// vt_callback already forwarded the frame to Metal before calling goVTFrame
// (with a NULL pointer, purely so Go-side frame counting/lastFrameTime stats
// keep moving — see vt_callback's own doc comment for why that NULL call
// matters).
func NativeVideoOverlayIsActive() bool {
	return MetalVideoIsActive()
}
