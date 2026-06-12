//go:build !darwin && !windows

package controller

import "image"

// getMetalLastFrame is a no-op on non-darwin platforms (no Metal overlay).
func (vw *VideoWidget) getMetalLastFrame() *image.RGBA { return nil }

// getNativeFPS returns 0 on non-darwin platforms (no Metal FPS counter).
func (vw *VideoWidget) getNativeFPS() float64 { return 0 }
