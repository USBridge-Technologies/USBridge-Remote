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
JNIEXPORT void JNICALL Java_io_usbridge_client_QRResultBridge_deliverQRResult(JNIEnv *env, jclass clazz, jstring contents) {
    if (contents == NULL) return;
    const char* utf = (*env)->GetStringUTFChars(env, contents, NULL);
    if (utf) {
        deliverQRResultFromJNI((char*)utf);
        (*env)->ReleaseStringUTFChars(env, contents, utf);
    }
}

__attribute__((used))
JNIEXPORT void JNICALL Java_io_usbridge_client_QRResultBridge_deliverQRCancel(JNIEnv *env, jclass clazz) {
    deliverQRCancelFromJNI();
}

// keepJNISymbolsReferenced - dummy reference so linker doesn't remove JNI symbols (they are called only from Java)
void keepJNISymbolsReferenced(void) {
    extern void Java_io_usbridge_client_QRResultBridge_deliverQRResult(JNIEnv*, jclass, jstring);
    extern void Java_io_usbridge_client_QRResultBridge_deliverQRCancel(JNIEnv*, jclass);
    (void)Java_io_usbridge_client_QRResultBridge_deliverQRResult;
    (void)Java_io_usbridge_client_QRResultBridge_deliverQRCancel;
}

// Returns 1 if launchQRScanner is called, 0 if method is not found (Gradle build required)
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

	"usbridge-client/androidbridge"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

func init() {
	// Dummy call - keeps JNI symbols in .so (linker doesn't remove them as "unused")
	C.keepJNISymbolsReferenced()
}

// deliverQRResultFromJNI called from JNI (QRResultBridge.deliverQRResult) - passed to main app without libgojni
//
//export deliverQRResultFromJNI
func deliverQRResultFromJNI(contents *C.char) {
	if contents != nil {
		androidbridge.SetQRResultFromJNI(C.GoString(contents))
	}
}

// deliverQRCancelFromJNI called from JNI (QRResultBridge.deliverQRCancel)
//
//export deliverQRCancelFromJNI
func deliverQRCancelFromJNI() {
	androidbridge.SetQRResultCancelledFromJNI()
}

// ShowCameraScannerNative shows camera window for scanning QR (like in Telegram)
func (qs *QRScanner) ShowCameraScannerNative(parent fyne.Window) {
	qs.window = parent
	session := qs.beginScanSession()

	logrus.Info("📷 Android: starting QR scanner (camera window)")

	// Clear previous result
	androidbridge.ClearQRResult()

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

	// Start polling OUTSIDE RunNative callback
	if launchSuccess {
		logrus.Info("📷 Starting polling goroutine...")
		go qs.pollQRResult(parent, session)
	}
}

// pollQRResult polls androidbridge (result comes through JNI -> QRResultBridge -> main app)
func (qs *QRScanner) pollQRResult(parent fyne.Window, session uint64) {
	logrus.Info("📷 [POLL] Starting QR result polling...")

	for i := 0; i < 600; i++ { // max 60 seconds
		time.Sleep(100 * time.Millisecond)
		if !qs.isScanSessionActive(session) {
			logrus.Debug("📷 [POLL] Scan session changed, stopping stale polling")
			return
		}

		ready := androidbridge.IsQRResultReady()
		if !ready {
			continue
		}

		result := androidbridge.GetQRResult()
		if result == nil {
			continue
		}
		androidbridge.ClearQRResult()

		qs.applyQRResult(result, parent, session)
		return
	}
}

func (qs *QRScanner) applyQRResult(result *androidbridge.QRScanResult, parent fyne.Window, session uint64) {
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

// scanImageData decodes PNG/JPEG (from camera) and scans QR code
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
