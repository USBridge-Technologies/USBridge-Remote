//go:build android
// +build android

package graphics

/*
#cgo LDFLAGS: -landroid -llog -Wl,--allow-multiple-definition

#include <jni.h>
#include <stdint.h>
#include <android/log.h>

#define IME_LOG_TAG "USBridgeIME"
#define IME_LOGI(...) __android_log_print(ANDROID_LOG_INFO, IME_LOG_TAG, __VA_ARGS__)
#define IME_LOGE(...) __android_log_print(ANDROID_LOG_ERROR, IME_LOG_TAG, __VA_ARGS__)

extern void deliverIMEHeightFromJNI(jint imeHeightPx, jint screenHeightPx);
extern void deliverLanguageFromJNI(char* lang);
extern void deliverIMETextFromJNI(jint deleteCount, char* text);
extern void deliverIMEUserDismissedFromJNI(void);

__attribute__((used))
JNIEXPORT void JNICALL Java_io_usbridge_client_KeyboardBridge_onIMEHeightChanged(JNIEnv *env, jclass clazz, jint imeHeightPx, jint screenHeightPx) {
    deliverIMEHeightFromJNI(imeHeightPx, screenHeightPx);
}

__attribute__((used))
JNIEXPORT void JNICALL Java_io_usbridge_client_KeyboardBridge_onLanguageChanged(JNIEnv *env, jclass clazz, jstring lang) {
    const char *nativeString = (*env)->GetStringUTFChars(env, lang, 0);
    deliverLanguageFromJNI((char*)nativeString);
    (*env)->ReleaseStringUTFChars(env, lang, nativeString);
}

__attribute__((used))
JNIEXPORT void JNICALL Java_io_usbridge_client_KeyboardBridge_onIMETextInput(JNIEnv *env, jclass clazz, jint deleteCount, jstring text) {
    const char *nativeString = (*env)->GetStringUTFChars(env, text, 0);
    deliverIMETextFromJNI(deleteCount, (char*)nativeString);
    (*env)->ReleaseStringUTFChars(env, text, nativeString);
}

__attribute__((used))
JNIEXPORT void JNICALL Java_io_usbridge_client_KeyboardBridge_onIMEUserDismissed(JNIEnv *env, jclass clazz) {
    deliverIMEUserDismissedFromJNI();
}

// keepIMEBridgeSymbolsReferenced - dummy reference to prevent the linker from removing JNI symbols
void keepIMEBridgeSymbolsReferenced(void) {
    extern void Java_io_usbridge_client_KeyboardBridge_onIMEHeightChanged(JNIEnv*, jclass, jint, jint);
    (void)Java_io_usbridge_client_KeyboardBridge_onIMEHeightChanged;

    extern void Java_io_usbridge_client_KeyboardBridge_onLanguageChanged(JNIEnv*, jclass, jstring);
    (void)Java_io_usbridge_client_KeyboardBridge_onLanguageChanged;

    extern void Java_io_usbridge_client_KeyboardBridge_onIMETextInput(JNIEnv*, jclass, jint, jstring);
    (void)Java_io_usbridge_client_KeyboardBridge_onIMETextInput;

    extern void Java_io_usbridge_client_KeyboardBridge_onIMEUserDismissed(JNIEnv*, jclass);
    (void)Java_io_usbridge_client_KeyboardBridge_onIMEUserDismissed;
}

static void jni_setStickyIME(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, int enabled) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL) {
		IME_LOGE("setStickyIME: null env/activity");
		return;
	}
	jclass cls = (*env)->GetObjectClass(env, activity);
	if (cls == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		IME_LOGE("setStickyIME: GetObjectClass failed");
		return;
	}
	jmethodID mid = (*env)->GetMethodID(env, cls, "setStickyIME", "(Z)V");
	if (mid == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		IME_LOGE("MainActivity.setStickyIME(Z)V not found");
		return;
	}
	(*env)->CallVoidMethod(env, activity, mid, enabled ? JNI_TRUE : JNI_FALSE);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		IME_LOGE("setStickyIME call threw");
	} else {
		IME_LOGI("setStickyIME(%d) ok", enabled);
	}
	(*env)->DeleteLocalRef(env, cls);
}
*/
import "C"

import (
	"fmt"
	"sync"
	"time"
	"usbridge-client/internal/input"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

var (
	lastIMEH        float32 // last nav-bar/IME margin in Fyne dp units
	pendingIMEPx    int     // raw px value pending Fyne canvas initialization
	pendingScreenPx int

	imeTextHandlerMu sync.Mutex
	imeTextHandler   func(deleteCount int, text string)

	imeUserDismissedMu      sync.Mutex
	imeUserDismissedHandler func()
)

// SetIMETextHandler registers the sticky soft-IME text sink (VideoWidget).
func SetIMETextHandler(fn func(deleteCount int, text string)) {
	imeTextHandlerMu.Lock()
	imeTextHandler = fn
	imeTextHandlerMu.Unlock()
}

// SetIMEUserDismissedHandler registers the Android Back / user-dismiss sink
// for the sticky soft-IME + special-keys stack.
func SetIMEUserDismissedHandler(fn func()) {
	imeUserDismissedMu.Lock()
	imeUserDismissedHandler = fn
	imeUserDismissedMu.Unlock()
}

//export deliverIMETextFromJNI
func deliverIMETextFromJNI(deleteCount C.jint, textStr *C.char) {
	del := int(deleteCount)
	text := C.GoString(textStr)
	logrus.Infof("⌨️ [IME-TEXT-JNI] del=%d add=%q", del, text)
	imeTextHandlerMu.Lock()
	fn := imeTextHandler
	imeTextHandlerMu.Unlock()
	if fn == nil {
		return
	}
	fyne.Do(func() {
		fn(del, text)
	})
}

//export deliverIMEUserDismissedFromJNI
func deliverIMEUserDismissedFromJNI() {
	logrus.Info("⌨️ [IME-JNI] user dismissed soft IME (Back)")
	imeUserDismissedMu.Lock()
	fn := imeUserDismissedHandler
	imeUserDismissedMu.Unlock()
	if fn == nil {
		return
	}
	fyne.Do(fn)
}

// GetLastIMEH returns the last cached IME margin (including NavBar)
func GetLastIMEH() float32 {
	return lastIMEH
}

func init() {
	C.keepIMEBridgeSymbolsReferenced()
}

// SetStickySystemIME keeps the Android soft keyboard open until disabled.
func SetStickySystemIME(enabled bool) {
	flag := 0
	if enabled {
		flag = 1
	}
	err := driver.RunNative(func(ctx any) error {
		androidCtx, ok := ctx.(*driver.AndroidContext)
		if !ok || androidCtx == nil {
			return fmt.Errorf("android context unavailable")
		}
		C.jni_setStickyIME(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), C.int(flag))
		return nil
	})
	if err != nil {
		logrus.Warnf("⌨️ [IME] SetStickySystemIME(%v): %v", enabled, err)
	}
}

// RegisterAsIMETarget registers this VirtualKeyboard as a receiver of native IME events.
func (vk *VirtualKeyboard) RegisterAsIMETarget() {
	activeIMEKeyboardMu.Lock()
	activeIMEKeyboardTarget = vk
	activeIMEKeyboardMu.Unlock()

	// Apply the last known nav-bar margin so the panel is correctly positioned.
	// lastIMEH is 0 until deliverIMEHeightFromJNI fires; the retry in that function
	// ensures the correct value arrives within ~300ms of app start — well before
	// the user can reach the connected-device screen.
	fyne.Do(func() {
		vk.setIMEOffset(lastIMEH)
	})
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
		applyIMEHeight(imePx, screenPx)
	})
}

// applyIMEHeight converts raw pixel values to Fyne dp and updates the keyboard spacer.
// Must be called on the Fyne main thread (inside fyne.Do).
// If the canvas is not yet ready (app still starting up), stores the values and retries
// after a short delay so the first-ever nav-bar height is never silently dropped.
func applyIMEHeight(imePx, screenPx int) {
	if screenPx <= 0 {
		return
	}

	vk := activeIMEKeyboard()
	canvasH := float32(0)
	if vk != nil && vk.parentWindow != nil {
		canvasH = vk.parentWindow.Canvas().Size().Height
	} else if fyne.CurrentApp() != nil && len(fyne.CurrentApp().Driver().AllWindows()) > 0 {
		canvasH = fyne.CurrentApp().Driver().AllWindows()[0].Canvas().Size().Height
	}

	if canvasH <= 0 {
		// Fyne canvas not ready yet — store raw values and retry once the window exists.
		pendingIMEPx = imePx
		pendingScreenPx = screenPx
		go func() {
			time.Sleep(300 * time.Millisecond)
			fyne.Do(func() {
				if pendingIMEPx == 0 {
					return
				}
				p, s := pendingIMEPx, pendingScreenPx
				pendingIMEPx, pendingScreenPx = 0, 0
				applyIMEHeight(p, s)
			})
		}()
		return
	}

	pendingIMEPx, pendingScreenPx = 0, 0
	calculatedIMEH := float32(imePx) / float32(screenPx) * canvasH
	lastIMEH = calculatedIMEH
	logrus.Infof("⌨️ [IME-JNI] lastIMEH=%.0f canvasH=%.0f", lastIMEH, canvasH)

	if vk != nil {
		vk.setIMEOffset(calculatedIMEH)
	}
}

// deliverLanguageFromJNI is called from JNI (KeyboardBridge.onLanguageChanged).
//
//export deliverLanguageFromJNI
func deliverLanguageFromJNI(langStr *C.char) {
	goLang := C.GoString(langStr)
	logrus.Infof("⌨️ [IME-JNI] onLanguageChanged: %s", goLang)
	input.SetCurrentLanguage(goLang)
}
