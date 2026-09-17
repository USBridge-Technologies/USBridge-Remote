//go:build !windows

package gui

import "fyne.io/fyne/v2"

func nativeWindowFrame(fyne.Window) (windowFrame, bool) { return windowFrame{}, false }

func nativeSetWindowFrame(fyne.Window, windowFrame) bool { return false }

func nativeMoveWindow(fyne.Window, int, int) bool { return false }

func nativeUnlockWindowSize(fyne.Window) {}

func nativeWindowFrameIsVisible(windowFrame) bool { return false }
