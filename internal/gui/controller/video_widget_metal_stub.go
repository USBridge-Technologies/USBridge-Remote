//go:build !darwin && !windows && !linux && !android

package controller

import "fyne.io/fyne/v2"

// Stub implementations for platforms without a native GPU video overlay
// (Android, iOS, and other non-desktop targets).
// The 60 Hz render ticker + pendingFrame atomic path is still active.

func (vw *VideoWidget) isNativeVideoActive() bool               { return false }
func (vw *VideoWidget) startMetalVideoOnWindow(_ fyne.Window, _ bool) {}
func (vw *VideoWidget) stopMetalVideo()                               {}
func (vw *VideoWidget) updateMetalVideoFrame()                        {}
func (vw *VideoWidget) metalVideoEnterFullscreen(_ fyne.Window)       {}
func (vw *VideoWidget) metalVideoExitFullscreen()                     {}
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.videoCanvas == nil {
		return
	}
	pos := vw.videoCanvas.Position()
	sz := vw.videoCanvas.Size()
	return pos.X, pos.Y, sz.Width, sz.Height
}
