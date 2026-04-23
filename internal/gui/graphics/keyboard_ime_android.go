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

// keepIMEBridgeSymbolsReferenced — фиктивная ссылка, чтобы линкер не удалял JNI-символы
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
	lastIMEH float32 // Кэшируем последнее значение отступа в Fyne-единицах
)

// GetLastIMEH возвращает последний кэшированный отступ IME (включая NavBar)
func GetLastIMEH() float32 {
	return lastIMEH
}

func init() {
	C.keepIMEBridgeSymbolsReferenced()
}

// RegisterAsIMETarget регистрирует этот VirtualKeyboard как получателя нативных IME-событий.
func (vk *VirtualKeyboard) RegisterAsIMETarget() {
	activeIMEKeyboardMu.Lock()
	activeIMEKeyboardTarget = vk
	activeIMEKeyboardMu.Unlock()

	// Сразу применяем последний известный отступ, чтобы верстка встала на место до первого клика
	if lastIMEH > 0 {
		fyne.Do(func() {
			vk.setIMEOffset(lastIMEH)
		})
	}
}

// deliverIMEHeightFromJNI вызывается из JNI (KeyboardBridge.onIMEHeightChanged).
// Получает точную высоту IME в пикселях и высоту экрана, конвертирует в Fyne-единицы
// пропорционально (не нужно знать DPI — используем соотношение высот).
//
//export deliverIMEHeightFromJNI
func deliverIMEHeightFromJNI(imeHeightPx C.jint, screenHeightPx C.jint) {
	imePx := int(imeHeightPx)
	screenPx := int(screenHeightPx)

	logrus.Infof("⌨️ [IME-JNI] imeHeightPx=%d screenHeightPx=%d", imePx, screenPx)

	vk := activeIMEKeyboard()
	
	fyne.Do(func() {
		if screenPx <= 0 {
			return
		}

		canvasH := float32(0)
		if vk != nil && vk.parentWindow != nil {
			canvasH = vk.parentWindow.Canvas().Size().Height
		} else if fyne.CurrentApp() != nil && len(fyne.CurrentApp().Driver().AllWindows()) > 0 {
			// Пытаемся найти хоть какое-то окно для получения масштаба
			canvasH = fyne.CurrentApp().Driver().AllWindows()[0].Canvas().Size().Height
		}

		if canvasH <= 0 {
			// Если окон еще нет, мы не можем рассчитать Fyne-единицы.
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

// deliverLanguageFromJNI вызывается из JNI (KeyboardBridge.onLanguageChanged).
//
//export deliverLanguageFromJNI
func deliverLanguageFromJNI(langStr *C.char) {
	goLang := C.GoString(langStr)
	logrus.Infof("⌨️ [IME-JNI] onLanguageChanged: %s", goLang)
	input.SetCurrentLanguage(goLang)
}
