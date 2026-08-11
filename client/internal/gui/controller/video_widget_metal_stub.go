//go:build !darwin && !windows && !linux && !android && !(js && wasm)

package controller

import "fyne.io/fyne/v2"

// Fallback stub for this method set on any platform not covered by one of
// the other per-platform files (darwin/ios have video_widget_metal_darwin.go
// or video_widget_metal_ios.go, windows/linux/android each have their own,
// and wasm has video_widget_dom_overlay_wasm.go -- a DOM <video> CSS overlay
// instead of a native GPU surface, hence the exclusion above). Currently
// unreachable by any of this project's six shipping build targets; kept as
// a safety net for a hypothetical future platform that adds no native
// overlay of its own.
// The 60 Hz render ticker + pendingFrame atomic path is still active.

func (vw *VideoWidget) isNativeVideoActive() bool                     { return false }
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
