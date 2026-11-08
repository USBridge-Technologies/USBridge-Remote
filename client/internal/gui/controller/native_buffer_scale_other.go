//go:build !linux || android

package controller

import "fyne.io/fyne/v2"

func nativeBufferScale(c fyne.Canvas) float32 { return c.Scale() }
