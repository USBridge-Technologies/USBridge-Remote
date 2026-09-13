//go:build !windows

package gui

import "fyne.io/fyne/v2"

func nativeWindowFrame(fyne.Window) (windowFrame, bool) { return windowFrame{}, false }

func nativeMoveWindow(fyne.Window, int, int) bool { return false }

func nativeWindowFrameIsVisible(windowFrame) bool { return false }
