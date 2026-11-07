//go:build !linux || android

package gui

import "fyne.io/fyne/v2"

// availableWindowArea: the window manager keeps windows on screen there.
func availableWindowArea(fyne.Canvas) fyne.Size { return fyne.Size{} }
