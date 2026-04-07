//go:build android
// +build android

package controller

/*
#cgo LDFLAGS: -landroid -llog -Wl,--allow-multiple-definition

#include <jni.h>

extern void deliverViewportGestureStateFromJNI(jboolean active);
extern void deliverViewportGestureUpdateFromJNI(jfloat scaleFactor, jfloat focusX, jfloat focusY, jfloat panDx, jfloat panDy);

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_GestureBridge_onViewportGestureStateChanged(JNIEnv *env, jclass clazz, jboolean active) {
    deliverViewportGestureStateFromJNI(active);
}

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_GestureBridge_onViewportGestureUpdate(JNIEnv *env, jclass clazz, jfloat scaleFactor, jfloat focusX, jfloat focusY, jfloat panDx, jfloat panDy) {
    deliverViewportGestureUpdateFromJNI(scaleFactor, focusX, focusY, panDx, panDy);
}

void keepViewportGestureJNISymbolsReferenced(void) {
    extern void Java_com_usbridge_client_GestureBridge_onViewportGestureStateChanged(JNIEnv*, jclass, jboolean);
    extern void Java_com_usbridge_client_GestureBridge_onViewportGestureUpdate(JNIEnv*, jclass, jfloat, jfloat, jfloat, jfloat, jfloat);
    (void)Java_com_usbridge_client_GestureBridge_onViewportGestureStateChanged;
    (void)Java_com_usbridge_client_GestureBridge_onViewportGestureUpdate;
}
*/
import "C"

import (
	"time"

	"fyne.io/fyne/v2"
)

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

		absPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(wrapper)
		localFocusX := float32(focusX) - absPos.X
		localFocusY := float32(focusY) - absPos.Y

		vw.UpdateTouchpadAndContentRect(wrapperSize.Width, wrapperSize.Height, vw.GetCurrentFrame())
		vw.applyViewportGesture(float32(scaleFactor), localFocusX, localFocusY, float32(panDx), float32(panDy))
		vw.refreshViewportViews()
	})
}
