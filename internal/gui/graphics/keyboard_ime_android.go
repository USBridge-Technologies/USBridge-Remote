//go:build android
// +build android

package graphics

/*
#cgo LDFLAGS: -landroid -llog -Wl,--allow-multiple-definition

#include <jni.h>

extern void deliverIMEHeightFromJNI(jint imeHeightPx, jint screenHeightPx);
extern void deliverLanguageFromJNI(char* lang);

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_KeyboardBridge_onIMEHeightChanged(JNIEnv *env, jclass clazz, jint imeHeightPx, jint screenHeightPx) {
    deliverIMEHeightFromJNI(imeHeightPx, screenHeightPx);
}

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_KeyboardBridge_onLanguageChanged(JNIEnv *env, jclass clazz, jstring lang) {
    const char *nativeString = (*env)->GetStringUTFChars(env, lang, 0);
    deliverLanguageFromJNI((char*)nativeString);
    (*env)->ReleaseStringUTFChars(env, lang, nativeString);
}

// keepIMEBridgeSymbolsReferenced - dummy reference to prevent the linker from removing JNI symbols
void keepIMEBridgeSymbolsReferenced(void) {
    extern void Java_com_usbridge_client_KeyboardBridge_onIMEHeightChanged(JNIEnv*, jclass, jint, jint);
    (void)Java_com_usbridge_client_KeyboardBridge_onIMEHeightChanged;

    extern void Java_com_usbridge_client_KeyboardBridge_onLanguageChanged(JNIEnv*, jclass, jstring);
    (void)Java_com_usbridge_client_KeyboardBridge_onLanguageChanged;
}
*/
import "C"

import (
	"usbridge-client/internal/input"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

var (
	lastIMEH float32 // Cache the last margin value in Fyne units
)

// GetLastIMEH returns the last cached IME margin (including NavBar)
func GetLastIMEH() float32 {
	return lastIMEH
}

func init() {
	C.keepIMEBridgeSymbolsReferenced()
}

// RegisterAsIMETarget registers this VirtualKeyboard as a receiver of native IME events.
func (vk *VirtualKeyboard) RegisterAsIMETarget() {
	activeIMEKeyboardMu.Lock()
	activeIMEKeyboardTarget = vk
	activeIMEKeyboardMu.Unlock()

	// Immediately apply the last known margin so the layout falls into place before the first click
	if lastIMEH > 0 {
		fyne.Do(func() {
			vk.setIMEOffset(lastIMEH)
		})
	} else {
		fyne.Do(func() {
			vk.setIMEOffset(36)
		})
	}
}

// deliverIMEHeightFromJNI is called from JNI (KeyboardBridge.onIMEHeightChanged).
// Receives the exact IME height in pixels and the screen height, converts to Fyne units
// proportionally (no need to know DPI - we use the height ratio).
//
//export deliverIMEHeightFromJNI
func deliverIMEHeightFromJNI(imeHeightPx C.jint, screenHeightPx C.jint) {
	imePx := int(imeHeightPx)
	screenPx := int(screenHeightPx)

	logrus.Infof("⌨️ [IME-JNI] imeHeightPx=%d screenHeightPx=%d", imePx, screenPx)

	fyne.Do(func() {
		vk := activeIMEKeyboard()
		
		if screenPx <= 0 {
			return
		}

		canvasH := float32(0)
		if vk != nil && vk.parentWindow != nil {
			canvasH = vk.parentWindow.Canvas().Size().Height
		} else if fyne.CurrentApp() != nil && len(fyne.CurrentApp().Driver().AllWindows()) > 0 {
			// Try to find any window to get the scale
			canvasH = fyne.CurrentApp().Driver().AllWindows()[0].Canvas().Size().Height
		}

		if canvasH <= 0 {
			// If there are no windows yet, we cannot calculate Fyne units.
			return
		}

		calculatedIMEH := float32(imePx) / float32(screenPx) * canvasH
		lastIMEH = calculatedIMEH
		logrus.Infof("⌨️ [IME-JNI] lastIMEH=%.0f canvasH=%.0f", lastIMEH, canvasH)

		if vk != nil {
			vk.setIMEOffset(calculatedIMEH)
		}
	})
}

// deliverLanguageFromJNI is called from JNI (KeyboardBridge.onLanguageChanged).
//
//export deliverLanguageFromJNI
func deliverLanguageFromJNI(langStr *C.char) {
	goLang := C.GoString(langStr)
	logrus.Infof("⌨️ [IME-JNI] onLanguageChanged: %s", goLang)
	input.SetCurrentLanguage(goLang)
}
