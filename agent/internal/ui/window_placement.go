package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

const (
	prefWindowFrameX   = "window.frame.x"
	prefWindowFrameY   = "window.frame.y"
	prefWindowFrameW   = "window.frame.w"
	prefWindowFrameH   = "window.frame.h"
	prefWindowLogicalW = "window.logical.w"
	prefWindowLogicalH = "window.logical.h"

	windowPlacementSave = 3 * time.Second

	minConfiguredWindowWidth   float32 = 640
	minConfiguredWindowHeight  float32 = 400
	defaultWindowWidth         float32 = 800
	defaultWindowHeight        float32 = 570
	defaultWindowHeightLinux   float32 = 660
)

type windowFrame struct {
	X, Y, W, H int
}

func windowFrameVisible(f windowFrame, vx, vy, vw, vh int) bool {
	if f.W <= 0 || f.H <= 0 || vw <= 0 || vh <= 0 {
		return false
	}
	cx := f.X + f.W/2
	cy := f.Y + f.H/2
	return cx >= vx && cy >= vy && cx < vx+vw && cy < vy+vh
}

func (w *Window) savedWindowFrame() (windowFrame, bool) {
	if w == nil || w.app == nil {
		return windowFrame{}, false
	}
	prefs := w.app.Preferences()
	f := windowFrame{
		X: prefs.Int(prefWindowFrameX),
		Y: prefs.Int(prefWindowFrameY),
		W: prefs.Int(prefWindowFrameW),
		H: prefs.Int(prefWindowFrameH),
	}
	if float32(f.W) < minConfiguredWindowWidth || float32(f.H) < minConfiguredWindowHeight {
		return windowFrame{}, false
	}
	return f, true
}

func (w *Window) savedLogicalWindowSize() (width, height int, ok bool) {
	if w == nil || w.app == nil {
		return 0, 0, false
	}
	prefs := w.app.Preferences()
	width = prefs.Int(prefWindowLogicalW)
	height = prefs.Int(prefWindowLogicalH)
	if float32(width) < minConfiguredWindowWidth || float32(height) < minConfiguredWindowHeight {
		return 0, 0, false
	}
	return width, height, true
}

func (w *Window) canRestoreWindowPlacement() bool {
	if w == nil {
		return false
	}
	if f, ok := w.savedWindowFrame(); ok && nativeWindowFrameIsVisible(f) {
		return true
	}
	_, _, ok := w.savedLogicalWindowSize()
	return ok
}

func (w *Window) persistWindowPlacement() {
	if w == nil || w.app == nil || w.guiWin == nil {
		return
	}
	prefs := w.app.Preferences()
	if canvas := w.guiWin.Canvas(); canvas != nil {
		sz := canvas.Size()
		if sz.Width < minConfiguredWindowWidth || sz.Height < minConfiguredWindowHeight {
			return
		}
		prefs.SetInt(prefWindowLogicalW, int(sz.Width))
		prefs.SetInt(prefWindowLogicalH, int(sz.Height))
	}
	if f, ok := nativeWindowFrame(w.guiWin); ok && float32(f.W) >= minConfiguredWindowWidth && float32(f.H) >= minConfiguredWindowHeight {
		prefs.SetInt(prefWindowFrameX, f.X)
		prefs.SetInt(prefWindowFrameY, f.Y)
		prefs.SetInt(prefWindowFrameW, f.W)
		prefs.SetInt(prefWindowFrameH, f.H)
	}
}

func (w *Window) scheduleWindowPlacementRestore() {
	if !w.canRestoreWindowPlacement() {
		return
	}
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			applied := make(chan bool, 1)
			fyne.Do(func() {
				applied <- w.applySavedWindowPlacement()
			})
			if <-applied {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		logrus.Warn("[ui] timed out restoring saved window position")
	}()
}

func (w *Window) applySavedWindowPlacement() bool {
	if w == nil || w.guiWin == nil {
		return false
	}
	if f, ok := w.savedWindowFrame(); ok && nativeWindowFrameIsVisible(f) {
		if nativeSetWindowFrame(w.guiWin, f) {
			logrus.Infof("[ui] restored window to %d,%d (%dx%d)", f.X, f.Y, f.W, f.H)
			return true
		}
	}
	width, height, ok := w.savedLogicalWindowSize()
	if !ok {
		return false
	}
	w.guiWin.Resize(fitWindowSizeToScreen(w.guiWin, fyne.NewSize(float32(width), float32(height))))
	return true
}

func (w *Window) startWindowPlacementAutosave() {
	w.stopWindowPlacementAutosave()
	stop := make(chan struct{})
	w.windowPlacementStop = stop
	go func() {
		ticker := time.NewTicker(windowPlacementSave)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				fyne.Do(w.persistWindowPlacement)
			}
		}
	}()
}

func (w *Window) stopWindowPlacementAutosave() {
	if w.windowPlacementStop == nil {
		return
	}
	close(w.windowPlacementStop)
	w.windowPlacementStop = nil
}
