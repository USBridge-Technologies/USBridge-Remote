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

// handleAndroidSystemBack consumes Back for in-app chrome after Kotlin has
// already dismissed a visible soft IME. Fullscreen first, then the Control
// keyboard stack, then any Fyne overlay (Add/Edit connection). Peek state
// on this thread and mutate via fyne.Do — DoAndWait would deadlock if
// CloseAllKeyboards / exitFullscreen posts back to the Android UI thread.
func handleAndroidSystemBack() bool {
	vw := activeGestureVideoWidget()
	if vw != nil {
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
	}
	if win := androidBackWindow(); win != nil && win.Canvas() != nil {
		if top := win.Canvas().Overlays().Top(); top != nil {
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

func androidBackWindow() fyne.Window {
	if vw := activeGestureVideoWidget(); vw != nil && vw.parentWindow != nil {
		return vw.parentWindow
	}
	if fyne.CurrentApp() == nil || fyne.CurrentApp().Driver() == nil {
		return nil
	}
	wins := fyne.CurrentApp().Driver().AllWindows()
	if len(wins) == 0 {
		return nil
	}
	return wins[0]
}
