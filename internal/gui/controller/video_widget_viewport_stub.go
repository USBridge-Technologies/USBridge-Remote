//go:build !android

package controller

// updateNativeViewportAndCursor is a no-op on non-Android platforms.
// On Android it forwards zoom/pan state and virtual cursor to the Vulkan renderer.
func (vw *VideoWidget) updateNativeViewportAndCursor() {}

// centerViewportOnVirtualCursor is a no-op on non-Android platforms.
func (vw *VideoWidget) centerViewportOnVirtualCursor() {}

// androidCursorScale returns 1 on non-Android platforms.
func (vw *VideoWidget) androidCursorScale() int { return 1 }

// initAndroidCursorScale is a no-op on non-Android platforms.
func (vw *VideoWidget) initAndroidCursorScale(_ int) {}
