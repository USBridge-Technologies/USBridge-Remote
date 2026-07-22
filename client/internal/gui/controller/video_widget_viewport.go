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

// RefreshViewportGeometry recomputes the touchpad/content rect against the
// viewport wrapper's current (now-visible) size. The pre-set block in
// startVideoWithParamsInternal only takes effect if the Control tab is
// already visible when a device/monitor switch restarts the stream; if the
// switch happened from the Devices tab, the wrapper's size can be stale or
// zero at that point and absolute mouse mapping is left pointing at the old
// monitor's geometry until something else (e.g. a codec change made from the
// Control tab) happens to call UpdateTouchpadAndContentRect again. Call this
// whenever the Control tab becomes visible so the mapping is corrected
// immediately regardless of what triggered the last stream (re)start.
func (vw *VideoWidget) RefreshViewportGeometry() {
	fyne.Do(func() {
		tw := vw.activeViewportWrapper()
		if tw == nil {
			return
		}
		sz := tw.Size()
		if sz.Width <= 0 || sz.Height <= 0 {
			return
		}
		vw.UpdateTouchpadAndContentRect(sz.Width, sz.Height, vw.GetCurrentFrame())
	})
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
