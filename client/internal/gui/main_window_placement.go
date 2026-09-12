package gui

import (
	"time"

	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

const (
	prefWindowFrameX    = "window.frame.x"
	prefWindowFrameY    = "window.frame.y"
	prefWindowFrameW    = "window.frame.w"
	prefWindowFrameH    = "window.frame.h"
	prefWindowLogicalW  = "window.logical.w"
	prefWindowLogicalH  = "window.logical.h"
	windowPlacementSave = 3 * time.Second
)

// windowFrame is the native outer window rectangle in physical pixels.
type windowFrame struct {
	X, Y, W, H int
}

// windowFrameVisible reports whether the frame's center sits inside the
// given virtual desktop. Used so a saved position on an unplugged monitor
// does not reopen the window off-screen.
func windowFrameVisible(f windowFrame, vx, vy, vw, vh int) bool {
	if f.W <= 0 || f.H <= 0 || vw <= 0 || vh <= 0 {
		return false
	}
	cx := f.X + f.W/2
	cy := f.Y + f.H/2
	return cx >= vx && cy >= vy && cx < vx+vw && cy < vy+vh
}

func (mw *MainWindow) savedWindowFrame() (windowFrame, bool) {
	if mw == nil || mw.app == nil {
		return windowFrame{}, false
	}
	prefs := mw.app.Preferences()
	f := windowFrame{
		X: prefs.Int(prefWindowFrameX),
		Y: prefs.Int(prefWindowFrameY),
		W: prefs.Int(prefWindowFrameW),
		H: prefs.Int(prefWindowFrameH),
	}
	if f.W <= 0 || f.H <= 0 {
		return windowFrame{}, false
	}
	return f, true
}

func (mw *MainWindow) savedLogicalWindowSize() (width, height int, ok bool) {
	if mw == nil || mw.app == nil {
		return 0, 0, false
	}
	prefs := mw.app.Preferences()
	width = prefs.Int(prefWindowLogicalW)
	height = prefs.Int(prefWindowLogicalH)
	if width < minConfiguredWindowWidth || height < minConfiguredWindowHeight {
		return 0, 0, false
	}
	return width, height, true
}

func (mw *MainWindow) canRestoreWindowPlacement() bool {
	f, ok := mw.savedWindowFrame()
	if !ok {
		return false
	}
	return nativeWindowFrameIsVisible(f)
}

func (mw *MainWindow) persistWindowPlacement() {
	if mw == nil || mw.app == nil || mw.window == nil {
		return
	}
	if view.ForceMobileDesign {
		// Keep the last desktop logical size so leaving Mobile preview
		// restores the wide window, not the phone frame.
		return
	}
	prefs := mw.app.Preferences()
	if f, ok := nativeWindowFrame(mw.window); ok && f.W > 0 && f.H > 0 {
		prefs.SetInt(prefWindowFrameX, f.X)
		prefs.SetInt(prefWindowFrameY, f.Y)
		prefs.SetInt(prefWindowFrameW, f.W)
		prefs.SetInt(prefWindowFrameH, f.H)
	}
	if mw.lastGoodWindowSize.Width >= minConfiguredWindowWidth && mw.lastGoodWindowSize.Height >= minConfiguredWindowHeight {
		prefs.SetInt(prefWindowLogicalW, int(mw.lastGoodWindowSize.Width))
		prefs.SetInt(prefWindowLogicalH, int(mw.lastGoodWindowSize.Height))
		return
	}
	if canvas := mw.window.Canvas(); canvas != nil {
		sz := canvas.Size()
		if sz.Width >= minConfiguredWindowWidth && sz.Height >= minConfiguredWindowHeight {
			prefs.SetInt(prefWindowLogicalW, int(sz.Width))
			prefs.SetInt(prefWindowLogicalH, int(sz.Height))
		}
	}
}

func (mw *MainWindow) scheduleWindowPlacementRestore() {
	if !mw.canRestoreWindowPlacement() {
		return
	}
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			applied := make(chan bool, 1)
			fyne.Do(func() {
				applied <- mw.applySavedWindowPlacement()
			})
			if <-applied {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		logrus.Warn("🪟 [Placement] timed out waiting to restore the last window position")
	}()
}

func (mw *MainWindow) applySavedWindowPlacement() bool {
	f, ok := mw.savedWindowFrame()
	if !ok || !nativeWindowFrameIsVisible(f) {
		return false
	}
	if !nativeMoveWindow(mw.window, f.X, f.Y) {
		return false
	}
	logrus.Infof("🪟 [Placement] restored window to %d,%d (%dx%d)", f.X, f.Y, f.W, f.H)
	return true
}

func (mw *MainWindow) startWindowPlacementAutosave() {
	mw.stopWindowPlacementAutosave()
	stop := make(chan struct{})
	mw.windowPlacementStop = stop
	go func() {
		ticker := time.NewTicker(windowPlacementSave)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				fyne.Do(mw.persistWindowPlacement)
			}
		}
	}()
}

func (mw *MainWindow) stopWindowPlacementAutosave() {
	if mw.windowPlacementStop == nil {
		return
	}
	close(mw.windowPlacementStop)
	mw.windowPlacementStop = nil
}
