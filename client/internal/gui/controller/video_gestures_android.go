//go:build android
// +build android

package controller

/*
#cgo LDFLAGS: -landroid -llog -Wl,--allow-multiple-definition

#include <jni.h>

extern void deliverViewportGestureStateFromJNI(jboolean active);
extern void deliverViewportGestureUpdateFromJNI(jfloat scaleFactor, jfloat focusX, jfloat focusY, jfloat panDx, jfloat panDy);
extern void deliverScrollGestureFromJNI(jfloat dy);

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_GestureBridge_onViewportGestureStateChanged(JNIEnv *env, jclass clazz, jboolean active) {
    deliverViewportGestureStateFromJNI(active);
}

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_GestureBridge_onViewportGestureUpdate(JNIEnv *env, jclass clazz, jfloat scaleFactor, jfloat focusX, jfloat focusY, jfloat panDx, jfloat panDy) {
    deliverViewportGestureUpdateFromJNI(scaleFactor, focusX, focusY, panDx, panDy);
}

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_GestureBridge_onScrollGesture(JNIEnv *env, jclass clazz, jfloat dy) {
    deliverScrollGestureFromJNI(dy);
}

void keepViewportGestureJNISymbolsReferenced(void) {
    extern void Java_com_usbridge_client_GestureBridge_onViewportGestureStateChanged(JNIEnv*, jclass, jboolean);
    extern void Java_com_usbridge_client_GestureBridge_onViewportGestureUpdate(JNIEnv*, jclass, jfloat, jfloat, jfloat, jfloat, jfloat);
    extern void Java_com_usbridge_client_GestureBridge_onScrollGesture(JNIEnv*, jclass, jfloat);
    (void)Java_com_usbridge_client_GestureBridge_onViewportGestureStateChanged;
    (void)Java_com_usbridge_client_GestureBridge_onViewportGestureUpdate;
    (void)Java_com_usbridge_client_GestureBridge_onScrollGesture;
}
*/
import "C"

import (
	"time"

	"fyne.io/fyne/v2"
)

// scrollAccumY accumulates sub-threshold centroid-Y deltas for two-finger scroll.
var scrollAccumY float32

func init() {
	C.keepViewportGestureJNISymbolsReferenced()
}

//export deliverViewportGestureStateFromJNI
func deliverViewportGestureStateFromJNI(active C.jboolean) {
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
	scrollAccumY = 0
	vw.lastMultiTouchAt = time.Now()
	vw.cancelLocalTouchState()
}

//export deliverViewportGestureUpdateFromJNI
func deliverViewportGestureUpdateFromJNI(scaleFactor, focusX, focusY, panDx, panDy C.jfloat) {
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

		// Android MotionEvent coordinates are in physical pixels; Fyne works in dp.
		// Divide by canvas scale to convert px → dp before any dp-space arithmetic.
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

//export deliverScrollGestureFromJNI
func deliverScrollGestureFromJNI(dy C.jfloat) {
	vw := activeGestureVideoWidget()
	if vw == nil {
		return
	}
	// Accumulate centroid-Y delta (px).
	scrollAccumY += float32(dy)
	const pixelsPerTick = 20
	ticks := int(scrollAccumY / pixelsPerTick)
	if ticks == 0 {
		return
	}
	scrollAccumY -= float32(ticks) * pixelsPerTick
	ticks = clamp(ticks, -127, 127)
	vw.enqueueMouseScroll(ticks)
}
