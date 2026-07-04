//go:build ios

package controller

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework UIKit -framework CoreGraphics

extern void initVideoGesturesObserver(void);
*/
import "C"

import (
	"time"

	"fyne.io/fyne/v2"
)

var iosScrollAccumY float32

func init() {
	C.initVideoGesturesObserver()
}

//export deliverViewportGestureStateFromObjC
func deliverViewportGestureStateFromObjC(active C.int) {
	vw := activeGestureVideoWidget()
	if vw == nil {
		return
	}

	isActive := active != 0
	vw.multiTouchActive = isActive
	if isActive {
		vw.lastMultiTouchAt = time.Now()
		vw.cancelLocalTouchState()
		return
	}
	iosScrollAccumY = 0
	vw.lastMultiTouchAt = time.Now()
	vw.cancelLocalTouchState()
}

//export deliverViewportGestureUpdateFromObjC
func deliverViewportGestureUpdateFromObjC(scaleFactor, focusX, focusY, panDx, panDy C.float) {
	vw := activeGestureVideoWidget()
	if vw == nil || !fyne.CurrentDevice().IsMobile() {
		return
	}

	wrapper := vw.activeViewportWrapper()
	if wrapper == nil {
		return
	}

	fyne.Do(func() {
		wrapperSize := wrapper.Size()
		if wrapperSize.Width <= 0 || wrapperSize.Height <= 0 {
			return
		}

		// iOS touches are in points (which match Fyne's dp). No physical pixel scaling needed usually,
		// but let's check Fyne canvas scale just in case. Fyne on iOS uses 1:1 points to dp.
		scale := float32(1)
		if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
			scale = vw.parentWindow.Canvas().Scale()
		}
		if scale <= 0 {
			scale = 1
		}

		absPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(wrapper)
		localFocusX := float32(focusX)/scale - absPos.X
		localFocusY := float32(focusY)/scale - absPos.Y

		vw.UpdateTouchpadAndContentRect(wrapperSize.Width, wrapperSize.Height, vw.GetCurrentFrame())
		vw.applyViewportGesture(float32(scaleFactor), localFocusX, localFocusY, float32(panDx)/scale, float32(panDy)/scale)
		vw.updateNativeViewportAndCursor()
		vw.refreshViewportViews()
	})
}

//export deliverScrollGestureFromObjC
func deliverScrollGestureFromObjC(dy C.float) {
	vw := activeGestureVideoWidget()
	if vw == nil {
		return
	}
	iosScrollAccumY += float32(dy)
	const pixelsPerTick = 20
	ticks := int(iosScrollAccumY / pixelsPerTick)
	if ticks == 0 {
		return
	}
	iosScrollAccumY -= float32(ticks) * pixelsPerTick
	ticks = clamp(ticks, -127, 127)
	vw.enqueueMouseScroll(ticks)
}
