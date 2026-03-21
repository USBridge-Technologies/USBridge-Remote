//go:build !windows

package gui

import "fyne.io/fyne/v2"

func startupWindowScale(app fyne.App) float32 {
	return app.Settings().Scale()
}

func startupWindowMaxSize(scale float32) fyne.Size {
	return fyne.NewSize(0, 0)
}

func startupWindowPreferredSize(scale float32) fyne.Size {
	return fyne.NewSize(0, 0)
}
