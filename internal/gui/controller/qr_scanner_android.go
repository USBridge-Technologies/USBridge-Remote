//go:build android
// +build android

package controller

/*
#cgo LDFLAGS: -landroid -llog -Wl,--allow-multiple-definition

#include <jni.h>
#include <android/log.h>
#include <stdlib.h>
#include <string.h>

#define LOG_TAG "QRScanner"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

extern void deliverQRResultFromJNI(char* contents);
extern void deliverQRCancelFromJNI(void);

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_QRResultBridge_deliverQRResult(JNIEnv *env, jclass clazz, jstring contents) {
    if (contents == NULL) return;
    const char* utf = (*env)->GetStringUTFChars(env, contents, NULL);
    if (utf) {
        deliverQRResultFromJNI((char*)utf);
        (*env)->ReleaseStringUTFChars(env, contents, utf);
    }
}

__attribute__((used))
JNIEXPORT void JNICALL Java_com_usbridge_client_QRResultBridge_deliverQRCancel(JNIEnv *env, jclass clazz) {
    deliverQRCancelFromJNI();
}

// keepJNISymbolsReferenced — фиктивная ссылка, чтобы линкер не удалял JNI-символы (они вызываются только из Java)
void keepJNISymbolsReferenced(void) {
    extern void Java_com_usbridge_client_QRResultBridge_deliverQRResult(JNIEnv*, jclass, jstring);
    extern void Java_com_usbridge_client_QRResultBridge_deliverQRCancel(JNIEnv*, jclass);
    (void)Java_com_usbridge_client_QRResultBridge_deliverQRResult;
    (void)Java_com_usbridge_client_QRResultBridge_deliverQRCancel;
}

// Возвращает 1 если launchQRScanner вызван, 0 если метод не найден (нужна Gradle-сборка)
int jni_launchQRScanner(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject activity = (jobject)ctx_ptr;

    if (activity == NULL) {
        LOGE("Activity is null");
        return 0;
    }

    jclass activityClass = (*env)->GetObjectClass(env, activity);
    if (activityClass == NULL) {
        LOGE("Failed to get activity class");
        return 0;
    }

    jmethodID launchQRScanner = (*env)->GetMethodID(env, activityClass, "launchQRScanner", "()V");
    if (launchQRScanner == NULL) {
        LOGE("launchQRScanner not found - MainActivity required (Gradle build)");
        (*env)->DeleteLocalRef(env, activityClass);
        return 0;
    }

    LOGI("Calling launchQRScanner()...");
    (*env)->CallVoidMethod(env, activity, launchQRScanner);

    if ((*env)->ExceptionCheck(env)) {
        LOGE("Exception calling launchQRScanner");
        (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, activityClass);
        return 0;
    }

    (*env)->DeleteLocalRef(env, activityClass);
    return 1;
}
*/
import "C"
import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/nbdbridge"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

func init() {
	// Фиктивный вызов — сохраняет JNI-символы в .so (линкер не удаляет их как «неиспользуемые»)
	C.keepJNISymbolsReferenced()
}

// deliverQRResultFromJNI вызывается из JNI (QRResultBridge.deliverQRResult) — передача в main app без libgojni
//
//export deliverQRResultFromJNI
func deliverQRResultFromJNI(contents *C.char) {
	if contents != nil {
		nbdbridge.SetQRResultFromJNI(C.GoString(contents))
	}
}

// deliverQRCancelFromJNI вызывается из JNI (QRResultBridge.deliverQRCancel)
//
//export deliverQRCancelFromJNI
func deliverQRCancelFromJNI() {
	nbdbridge.SetQRResultCancelledFromJNI()
}

// ShowCameraScannerNative показывает окно камеры для сканирования QR (как в Telegram)
func (qs *QRScanner) ShowCameraScannerNative(parent fyne.Window) {
	qs.window = parent
	session := qs.beginScanSession()

	logrus.Info("📷 Android: starting QR scanner (camera window)")

	// Очищаем предыдущий результат
	nbdbridge.ClearQRResult()

	var launchSuccess bool

	err := driver.RunNative(func(ctx any) error {
		androidCtx, ok := ctx.(*driver.AndroidContext)
		if !ok {
			logrus.Error("❌ Failed to get AndroidContext")
			return nil
		}

		result := C.jni_launchQRScanner(
			C.uintptr_t(androidCtx.Env),
			C.uintptr_t(androidCtx.Ctx),
		)
		if result == 0 {
			logrus.Error("❌ MainActivity.launchQRScanner not found. Use the Gradle build: scripts/build_all_android.sh gradle")
			return fmt.Errorf("QR scanner unavailable: MainActivity required. Build with: scripts/build_all_android.sh gradle")
		}
		logrus.Info("📷 QR scanner started successfully")
		launchSuccess = true
		return nil
	})

	if err != nil {
		logrus.Errorf("Error launching QR scanner: %v", err)
		fyne.Do(func() {
			view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorLaunchingQRScanner, err), parent)
		})
		return
	}

	// Запускаем polling ВНЕ RunNative callback
	if launchSuccess {
		logrus.Info("📷 Starting polling goroutine...")
		go qs.pollQRResult(parent, session)
	}
}

// pollQRResult опрашивает nbdbridge (результат приходит через JNI → QRResultBridge → main app)
func (qs *QRScanner) pollQRResult(parent fyne.Window, session uint64) {
	logrus.Info("📷 [POLL] Starting QR result polling...")

	for i := 0; i < 600; i++ { // максимум 60 секунд
		time.Sleep(100 * time.Millisecond)
		if !qs.isScanSessionActive(session) {
			logrus.Debug("📷 [POLL] Scan session changed, stopping stale polling")
			return
		}

		ready := nbdbridge.IsQRResultReady()
		if i%50 == 0 {
			logrus.Infof("📷 [POLL] Iteration %d, ready=%v", i, ready)
		}

		if !ready {
			continue
		}

		result := nbdbridge.GetQRResult()
		if result == nil {
			continue
		}
		nbdbridge.ClearQRResult()

		qs.applyQRResult(result, parent, session)
		return
	}

	logrus.Warn("📷 [POLL] ⚠️ Timed out waiting for QR result (60 sec)")
}

func (qs *QRScanner) applyQRResult(result *nbdbridge.QRScanResult, parent fyne.Window, session uint64) {
	if !qs.tryHandleScanResult(session) {
		logrus.Debug("📷 [POLL] Ignoring stale or duplicate QR result")
		return
	}

	logrus.Infof("📷 [POLL] QR result received: contents=%q, imageLen=%d, cancelled=%v",
		result.Contents, len(result.ImageData), result.Cancelled)

	if result.Cancelled {
		logrus.Info("📷 [POLL] User cancelled scanning")
		return
	}

	if result.Contents != "" {
		contentsCopy := strings.Clone(result.Contents)
		logrus.Infof("📷 [POLL] ✅ QR scanned: %s", contentsCopy)
		fyne.Do(func() {
			qs.parseAndApply(contentsCopy, parent)
		})
		return
	}

	if len(result.ImageData) > 0 {
		dataCopy := make([]byte, len(result.ImageData))
		copy(dataCopy, result.ImageData)
		logrus.Infof("📷 [POLL] Camera image received: %d bytes", len(dataCopy))
		fyne.Do(func() {
			qs.scanImageData(dataCopy, parent)
		})
		return
	}

	logrus.Warn("📷 [POLL] Empty result")
}

// scanImageData декодирует PNG/JPEG (с камеры) и сканирует QR код
func (qs *QRScanner) scanImageData(data []byte, parent fyne.Window) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		logrus.Errorf("Failed to decode image: %v", err)
		fyne.Do(func() {
			view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorDecodingImage, err), parent)
		})
		return
	}
	qs.scanQRCode(img, parent)
}
