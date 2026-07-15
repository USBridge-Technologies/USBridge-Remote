package controller

import (
	"time"

	"fyne.io/fyne/v2"
)

func (vw *VideoWidget) activeViewportWrapper() *TouchpadWrapper {
	if vw.fullscreenDialog != nil && vw.fullscreenDialog.isFullscreen && vw.fullscreenDialog.touchpadWrapper != nil {
		return vw.fullscreenDialog.touchpadWrapper
	}
	return vw.touchpadWrapper
}

func (vw *VideoWidget) refreshViewportViews() {
	fyne.Do(func() {
		if vw.touchpadWrapper != nil {
			vw.touchpadWrapper.Refresh()
		}
		if vw.fullscreenDialog != nil && vw.fullscreenDialog.touchpadWrapper != nil {
			vw.fullscreenDialog.touchpadWrapper.Refresh()
		}
	})
}

func (vw *VideoWidget) cancelLocalTouchState() {
	vw.CancelTouchDownDelay()
	vw.touchActive = false
	vw.dragButton = 0
	vw.isDragging = false
	vw.scrollDragAxis = ""
	if vw.lmbHeld {
		vw.lmbHeld = false
		vw.enqueueMouseButtonUp(1)
	}
	vw.resetRelativeMoveAccumulator()
}

func (vw *VideoWidget) shouldIgnoreTouchInput() bool {
	return vw.multiTouchActive || time.Since(vw.lastMultiTouchAt) < 180*time.Millisecond
}
