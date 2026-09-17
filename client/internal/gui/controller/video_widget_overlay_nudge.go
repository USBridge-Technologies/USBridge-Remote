//go:build !windows

package controller

// revealNativeVideoOverlay is the Windows Vulkan HWND z-order poke.
// Other platforms already attach the overlay to the toolkit view, so a
// viewport refresh is enough if VideoTrace saw frames but no Fyne paint.
func (vw *VideoWidget) revealNativeVideoOverlay() {
	vw.RefreshViewportGeometry()
}
