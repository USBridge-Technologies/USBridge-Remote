//go:build android
// +build android

package graphics

/*
#cgo LDFLAGS: -landroid -llog -Wl,--allow-multiple-definition

#include <jni.h>

extern void deliverIMEHeightFromJNI(jint imeHeightPx, jint screenHeightPx);

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_KeyboardBridge_onIMEHeightChanged(JNIEnv *env, jclass clazz, jint imeHeightPx, jint screenHeightPx) {
    deliverIMEHeightFromJNI(imeHeightPx, screenHeightPx);
}

// keepIMEBridgeSymbolsReferenced — фиктивная ссылка, чтобы линкер не удалял JNI-символы
void keepIMEBridgeSymbolsReferenced(void) {
    extern void Java_com_usbridge_client_KeyboardBridge_onIMEHeightChanged(JNIEnv*, jclass, jint, jint);
    (void)Java_com_usbridge_client_KeyboardBridge_onIMEHeightChanged;
}
*/
import "C"

import (
	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func init() {
	C.keepIMEBridgeSymbolsReferenced()
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
	if vk == nil {
		return
	}

	fyne.Do(func() {
		if imePx <= 0 || screenPx <= 0 {
			// Клавиатура закрыта
			vk.setIMEOffset(0)
			return
		}

		// Конвертируем пиксели в Fyne-единицы через пропорцию:
		// screenPx пикселей = canvasH Fyne-единиц (при fullscreen теме canvas = весь экран)
		canvasH := float32(0)
		if vk.parentWindow != nil {
			canvasH = vk.parentWindow.Canvas().Size().Height
		}
		if canvasH <= 0 {
			return
		}

		// imeH / canvasH = imePx / screenPx
		imeH := float32(imePx) / float32(screenPx) * canvasH
		logrus.Infof("⌨️ [IME-JNI] imeH=%.0f canvasH=%.0f", imeH, canvasH)
		vk.setIMEOffset(imeH)
	})
}
