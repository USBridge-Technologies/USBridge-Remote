//go:build !android

package controller

// nativePointerOriginDp is the overlay origin in the same dp space as pointer
// events. Windows/Linux overlay mouse is already overlay-local, so (0,0).
func (vw *VideoWidget) nativePointerOriginDp() (float32, float32) {
	return 0, 0
}
