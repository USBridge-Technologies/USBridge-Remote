package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"github.com/sirupsen/logrus"
)

const windowShrinkGuardMargin float32 = 40

type windowResizeGuard struct {
	win *Window
}

func (g *windowResizeGuard) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
}

func (g *windowResizeGuard) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	objects[0].Resize(size)
	objects[0].Move(fyne.NewPos(0, 0))
	if g.win != nil {
		g.win.observeContentResize(size, objects[0].MinSize())
	}
}

func (w *Window) wrapWithResizeGuard(content fyne.CanvasObject) fyne.CanvasObject {
	return container.New(&windowResizeGuard{win: w}, content)
}

func (w *Window) observeContentResize(size, minSize fyne.Size) {
	if w == nil {
		return
	}

	tooSmall := size.Width <= minSize.Width+1 && size.Height <= minSize.Height+1
	hadGoodSize := w.lastGoodWindowSize.Width > 0 && w.lastGoodWindowSize.Height > 0
	shrankALot := w.lastGoodWindowSize.Width > size.Width+windowShrinkGuardMargin ||
		w.lastGoodWindowSize.Height > size.Height+windowShrinkGuardMargin

	if tooSmall && hadGoodSize && shrankALot {
		if w.resizeGuardPending {
			return
		}
		w.resizeGuardPending = true
		restoreTo := w.lastGoodWindowSize
		logrus.Warnf("[ui] window snapped to ~MinSize (%v) after being %v — restoring", size, restoreTo)
		time.AfterFunc(150*time.Millisecond, func() {
			fyne.Do(func() {
				w.resizeGuardPending = false
				if w.guiWin != nil {
					w.guiWin.Resize(fitWindowSizeToScreen(w.guiWin, restoreTo))
				}
			})
		})
		return
	}

	if size.Width >= minConfiguredWindowWidth && size.Height >= minConfiguredWindowHeight && !tooSmall {
		w.lastGoodWindowSize = size
	}
}
