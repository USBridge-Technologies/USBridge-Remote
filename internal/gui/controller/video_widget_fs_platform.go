//go:build !android

package controller

// keepNativeVideoAliveForFullscreenTransition reports whether the native GPU
// video overlay should remain active during a fullscreen transition. Returns
// false on non-Android platforms where the overlay is window-specific (Metal
// on macOS) and must be destroyed/recreated for each window change.
func (vw *VideoWidget) keepNativeVideoAliveForFullscreenTransition() bool { return false }
