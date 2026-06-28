//go:build android
// +build android

package controller

/*
#cgo LDFLAGS: -landroid -llog -Wl,--allow-multiple-definition

#include <jni.h>

extern void deliverGyroEventFromJNI(jfloat rx, jfloat ry, jfloat rz, jfloat dtMs);
extern void deliverVolumeButtonFromJNI(jint button, jboolean pressed);
extern void deliverShakeFromJNI(void);
extern jboolean isGyroMouseModeActiveFromJNI(void);

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_GyroBridge_onGyroEvent(JNIEnv *env, jclass clazz, jfloat rx, jfloat ry, jfloat rz, jfloat dtMs) {
    deliverGyroEventFromJNI(rx, ry, rz, dtMs);
}

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_GyroBridge_onVolumeButton(JNIEnv *env, jclass clazz, jint button, jboolean pressed) {
    deliverVolumeButtonFromJNI(button, pressed);
}

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_GyroBridge_onShakeGesture(JNIEnv *env, jclass clazz) {
    deliverShakeFromJNI();
}

__attribute__((used))
JNIEXPORT jboolean JNICALL Java_com_usbridge_client_GyroBridge_isGyroMouseModeActive(JNIEnv *env, jclass clazz) {
    return isGyroMouseModeActiveFromJNI();
}

void keepGyroJNISymbolsReferenced(void) {
    extern void Java_com_usbridge_client_GyroBridge_onGyroEvent(JNIEnv*, jclass, jfloat, jfloat, jfloat, jfloat);
    extern void Java_com_usbridge_client_GyroBridge_onVolumeButton(JNIEnv*, jclass, jint, jboolean);
    extern void Java_com_usbridge_client_GyroBridge_onShakeGesture(JNIEnv*, jclass);
    extern jboolean Java_com_usbridge_client_GyroBridge_isGyroMouseModeActive(JNIEnv*, jclass);
    (void)Java_com_usbridge_client_GyroBridge_onGyroEvent;
    (void)Java_com_usbridge_client_GyroBridge_onVolumeButton;
    (void)Java_com_usbridge_client_GyroBridge_onShakeGesture;
    (void)Java_com_usbridge_client_GyroBridge_isGyroMouseModeActive;
}
*/
import "C"

import (
	"math"

	"fyne.io/fyne/v2"
)

func init() {
	C.keepGyroJNISymbolsReferenced()
}

// gyroDeadzone is the minimum angular rate (rad/s) below which gyro input is ignored.
// Eliminates cursor drift from sensor noise when phone is stationary.
const gyroDeadzone = 0.03

// gyroSensitivity controls how much the cursor moves per radian of phone rotation.
// 1.5 means rotating 1 radian (≈57°) moves the cursor 1.5 screen widths.
const gyroSensitivity = 1.5

//export deliverGyroEventFromJNI
func deliverGyroEventFromJNI(rx, ry, _ C.jfloat, dtMs C.jfloat) {
	vw := activeGestureVideoWidget()
	if vw == nil || vw.GetMouseInputMode() != mouseModeGyroMouse {
		return
	}

	dtSec := float32(dtMs) / 1000.0
	if dtSec <= 0 || dtSec > 0.1 {
		return
	}

	rawRX := float32(rx)
	rawRY := float32(ry)

	// Apply deadzone: ignore sub-threshold angular rates.
	if math.Abs(float64(rawRX)) < gyroDeadzone {
		rawRX = 0
	}
	if math.Abs(float64(rawRY)) < gyroDeadzone {
		rawRY = 0
	}
	if rawRX == 0 && rawRY == 0 {
		return
	}

	// Capture for closure.
	capturedRX := rawRX
	capturedRY := rawRY
	capturedDt := dtSec

	fyne.Do(func() {
		if vw.GetMouseInputMode() != mouseModeGyroMouse {
			return
		}
		// Read content rect on the main thread to avoid races.
		cw := vw.contentRectW
		ch := vw.contentRectH
		if cw <= 0 || ch <= 0 {
			return
		}
		// Map gyro rotation to cursor delta in content-rect dp units.
		// rx (rotation around X axis = pitch) → cursor Y
		// ry (rotation around Y axis = yaw/roll) → cursor X (negated: tilt right → cursor right)
		rawDx := -capturedRY * capturedDt * cw * gyroSensitivity
		rawDy := -capturedRX * capturedDt * ch * gyroSensitivity

		tw := vw.activeViewportWrapper()
		if tw != nil {
			tw.handleVirtualCursorMove(rawDx, rawDy)
		}
	})
}

//export deliverVolumeButtonFromJNI
func deliverVolumeButtonFromJNI(button C.jint, pressed C.jboolean) {
	vw := activeGestureVideoWidget()
	if vw == nil || vw.GetMouseInputMode() != mouseModeGyroMouse {
		return
	}

	btn := int(button) // 1 = volume up = LMB, 2 = volume down = RMB
	isPressed := pressed != 0

	fyne.Do(func() {
		if vw.GetMouseInputMode() != mouseModeGyroMouse {
			return
		}
		if isPressed {
			vw.enqueueMouseButtonDown(btn)
		} else {
			vw.enqueueMouseButtonUp(btn)
		}
	})
}

//export deliverShakeFromJNI
func deliverShakeFromJNI() {
	vw := activeGestureVideoWidget()
	if vw == nil || vw.GetMouseInputMode() != mouseModeGyroMouse {
		return
	}

	fyne.Do(func() {
		if vw.GetMouseInputMode() != mouseModeGyroMouse {
			return
		}
		// Reset cursor to center
		vw.vcMu.Lock()
		vw.virtualCursorU = 0.5
		vw.virtualCursorV = 0.5
		vw.vcMu.Unlock()
		vw.updateNativeViewportAndCursor()
		vw.sendVirtualCursorToHost()
	})
}

//export isGyroMouseModeActiveFromJNI
func isGyroMouseModeActiveFromJNI() C.jboolean {
	vw := activeGestureVideoWidget()
	if vw == nil {
		return 0
	}
	if vw.GetMouseInputMode() == mouseModeGyroMouse {
		return 1
	}
	return 0
}
