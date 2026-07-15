//go:build !android && !ios

package controller

// updateNativeViewportAndCursor is a no-op on most platforms, but for Metal (iOS/macOS)
// we forward the new viewport to update the overlay frame.
func (vw *VideoWidget) updateNativeViewportAndCursor() {
	vw.updateMetalVideoFrame()
}

// centerViewportOnVirtualCursor is a no-op on non-Android platforms.
func (vw *VideoWidget) centerViewportOnVirtualCursor() {}

// androidCursorScale returns 1 on non-Android platforms.
func (vw *VideoWidget) androidCursorScale() int { return 1 }

// initAndroidCursorScale is a no-op on non-Android platforms.
func (vw *VideoWidget) initAndroidCursorScale(_ int) {}

// onIMEHeightChanged is a no-op on non-Android platforms.
func (vw *VideoWidget) onIMEHeightChanged(_ float32) {}

// triggerRmbHaptic is a no-op on non-Android platforms.
func triggerRmbHaptic() {}
