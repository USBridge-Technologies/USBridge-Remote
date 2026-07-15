//go:build ios && cgo

package service

// NativeVideoOverlayIsActive reports whether the Metal CALayer overlay is live on iOS.
func NativeVideoOverlayIsActive() bool {
	return MetalVideoIsActive()
}
