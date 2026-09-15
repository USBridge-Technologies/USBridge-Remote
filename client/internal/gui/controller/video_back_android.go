//go:build android
// +build android

package controller

/*
#cgo LDFLAGS: -landroid -llog -Wl,--allow-multiple-definition

#include <jni.h>

extern jboolean deliverSystemBackFromJNI(void);

__attribute__((used))
JNIEXPORT jboolean JNICALL Java_io_usbridge_client_BackBridge_onSystemBack(JNIEnv *env, jclass clazz) {
    return deliverSystemBackFromJNI();
}

void keepSystemBackJNISymbolsReferenced(void) {
    extern jboolean Java_io_usbridge_client_BackBridge_onSystemBack(JNIEnv*, jclass);
    (void)Java_io_usbridge_client_BackBridge_onSystemBack;
}
*/
import "C"

import (
	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func init() {
	C.keepSystemBackJNISymbolsReferenced()
}

//export deliverSystemBackFromJNI
func deliverSystemBackFromJNI() C.jboolean {
	if handleAndroidSystemBack() {
		return 1
	}
	return 0
}

// handleAndroidSystemBack consumes Back for in-app chrome that Vulkan covers
// (no on-screen close control): fullscreen first, then the keyboard stack.
// Peek state on this thread and mutate via fyne.Do — DoAndWait would deadlock
// if CloseAllKeyboards / exitFullscreen posts back to the Android UI thread.
func handleAndroidSystemBack() bool {
	vw := activeGestureVideoWidget()
	if vw == nil {
		return false
	}
	if vw.fullscreenDialog != nil && vw.fullscreenDialog.IsFullscreen() {
		logrus.Info("⬅️ System Back: exiting fullscreen")
		fyne.Do(func() {
			vw.ExitFullscreenIfNeeded()
		})
		return true
	}
	if vw.IsVirtualKeyboardVisible() || vw.IsSystemIMESticky() {
		logrus.Info("⬅️ System Back: dismissing keyboard stack")
		fyne.Do(func() {
			vw.CloseAllKeyboards()
		})
		return true
	}
	if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
		if top := vw.parentWindow.Canvas().Overlays().Top(); top != nil {
			logrus.Info("⬅️ System Back: dismissing Fyne overlay")
			overlay := top
			fyne.Do(func() {
				overlay.Hide()
			})
			return true
		}
	}
	return false
}
