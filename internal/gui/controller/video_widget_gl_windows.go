//go:build windows

package controller

import (
	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// isNativeVideoActive — Windows uses Fyne canvas for both windowed and fullscreen.
// GDI overlay is not used; always false so the Fyne canvas path is active.
func (vw *VideoWidget) isNativeVideoActive() bool { return false }

// startMetalVideoOnWindow is called on the first decoded frame (windowed, fullscreen=false)
// and on fullscreen transitions.  Fyne canvas handles rendering in both modes, so this
// function's job is just to (re)start the render ticker at the stream's configured FPS.
func (vw *VideoWidget) startMetalVideoOnWindow(_ fyne.Window, fullscreen bool) {
	if fullscreen {
		// Fullscreen window has its own canvas.Image (fd.videoImage in FullscreenDialog)
		// updated via fyne.Do in updateVideoFrame — nothing to do here.
		return
	}
	// Windowed: restart the render ticker at the stream's FPS so it is in sync
	// with the decoder rate instead of being hardcoded at 60 Hz.
	fps := 60
	if vw.videoClient != nil {
		if cfg := vw.videoClient.GetConfig(); cfg != nil && cfg.VideoFPS > 0 {
			fps = cfg.VideoFPS
		}
	}
	logrus.Infof("[Win] render ticker → %d Hz (stream FPS)", fps)
	vw.startRenderTicker(fps)
}

func (vw *VideoWidget) stopMetalVideo()                               {}
func (vw *VideoWidget) updateMetalVideoFrame()                        {}
func (vw *VideoWidget) metalVideoEnterFullscreen(_ fyne.Window)       {}
func (vw *VideoWidget) metalVideoExitFullscreen()                     {}
