//go:build android

package controller

import (
	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// Android uses the Fyne canvas path for both windowed and fullscreen modes.

func (vw *VideoWidget) isNativeVideoActive() bool { return false }

func (vw *VideoWidget) startMetalVideoOnWindow(_ fyne.Window, fullscreen bool) {
	if fullscreen {
		return
	}
	fps := 60
	if vw.videoClient != nil {
		if cfg := vw.videoClient.GetConfig(); cfg != nil && cfg.VideoFPS > 0 {
			fps = cfg.VideoFPS
		}
	}
	logrus.Infof("[Android] render ticker → %d Hz (stream FPS)", fps)
	vw.startRenderTicker(fps)
}

func (vw *VideoWidget) stopMetalVideo()                         {}
func (vw *VideoWidget) updateMetalVideoFrame()                  {}
func (vw *VideoWidget) metalVideoEnterFullscreen(_ fyne.Window) {}
func (vw *VideoWidget) metalVideoExitFullscreen()               {}

func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.videoCanvas == nil {
		return
	}
	pos := vw.videoCanvas.Position()
	sz := vw.videoCanvas.Size()
	return pos.X, pos.Y, sz.Width, sz.Height
}
