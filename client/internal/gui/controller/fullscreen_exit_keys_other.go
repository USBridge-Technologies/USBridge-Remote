//go:build !darwin || ios

package controller

// The exit keys reach isFullscreenExitKey through the window here.
func startFullscreenExitKeys(*FullscreenDialog) {}
func stopFullscreenExitKeys()                   {}
