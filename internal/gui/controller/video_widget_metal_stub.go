//go:build !darwin

package controller

import "fyne.io/fyne/v2"

func (vw *VideoWidget) startMetalVideoOnWindow(_ fyne.Window, _ bool) {}
func (vw *VideoWidget) stopMetalVideo()                               {}
func (vw *VideoWidget) updateMetalVideoFrame()                        {}
func (vw *VideoWidget) metalVideoEnterFullscreen(_ fyne.Window)       {}
func (vw *VideoWidget) metalVideoExitFullscreen()                     {}
