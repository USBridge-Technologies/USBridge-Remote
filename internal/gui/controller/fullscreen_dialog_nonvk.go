//go:build !windows

package controller

func (fd *FullscreenDialog) canWindowlessVKFullscreen() bool { return false }
func (fd *FullscreenDialog) enterWindowlessVKFullscreen()    {}
func (fd *FullscreenDialog) exitWindowlessVKFullscreen()     {}
