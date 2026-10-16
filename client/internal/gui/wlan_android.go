//go:build android

package gui

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
#include <jni.h>
#include <android/log.h>

#define LOG_TAG "USBridgeWiFi"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

// WifiManager.WIFI_MODE_FULL_HIGH_PERF / WIFI_MODE_FULL_LOW_LATENCY.
#define WIFI_MODE_FULL_HIGH_PERF   3
#define WIFI_MODE_FULL_LOW_LATENCY 4

static jobject g_lock_high_perf = NULL;
static jobject g_lock_low_latency = NULL;

static int jni_clear(JNIEnv *env) {
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionDescribe(env);
		(*env)->ExceptionClear(env);
		return 1;
	}
	return 0;
}

static void release_lock(JNIEnv *env, jobject *slot) {
	if (*slot == NULL) {
		return;
	}
	jclass lc = (*env)->GetObjectClass(env, *slot);
	jmethodID held = (*env)->GetMethodID(env, lc, "isHeld", "()Z");
	jmethodID release = (*env)->GetMethodID(env, lc, "release", "()V");
	if (held && release && (*env)->CallBooleanMethod(env, *slot, held)) {
		(*env)->CallVoidMethod(env, *slot, release);
	}
	jni_clear(env);
	(*env)->DeleteLocalRef(env, lc);
	(*env)->DeleteGlobalRef(env, *slot);
	*slot = NULL;
}

// acquire_lock creates and acquires a non-reference-counted WifiLock of
// mode on wm into *slot. Returns 1 on success.
static int acquire_lock(JNIEnv *env, jobject wm, jint mode, const char *tagText, jobject *slot) {
	if (*slot != NULL) {
		return 1;
	}
	jclass wmClass = (*env)->GetObjectClass(env, wm);
	jmethodID create = (*env)->GetMethodID(env, wmClass, "createWifiLock", "(ILjava/lang/String;)Landroid/net/wifi/WifiManager$WifiLock;");
	jstring tag = (*env)->NewStringUTF(env, tagText);
	jobject lock = create ? (*env)->CallObjectMethod(env, wm, create, mode, tag) : NULL;
	(*env)->DeleteLocalRef(env, tag);
	(*env)->DeleteLocalRef(env, wmClass);
	if (jni_clear(env) || lock == NULL) {
		LOGE("createWifiLock(%d) failed", mode);
		return 0;
	}
	jclass lockClass = (*env)->GetObjectClass(env, lock);
	jmethodID refCounted = (*env)->GetMethodID(env, lockClass, "setReferenceCounted", "(Z)V");
	jmethodID acquire = (*env)->GetMethodID(env, lockClass, "acquire", "()V");
	if (refCounted) {
		(*env)->CallVoidMethod(env, lock, refCounted, JNI_FALSE);
	}
	if (acquire) {
		(*env)->CallVoidMethod(env, lock, acquire);
	}
	(*env)->DeleteLocalRef(env, lockClass);
	if (jni_clear(env) || acquire == NULL) {
		(*env)->DeleteLocalRef(env, lock);
		LOGE("WifiLock(%d).acquire failed (WAKE_LOCK permission?)", mode);
		return 0;
	}
	*slot = (*env)->NewGlobalRef(env, lock);
	(*env)->DeleteLocalRef(env, lock);
	LOGI("Wi-Fi lock held (mode %d)", mode);
	return 1;
}

// jni_wifi_lock holds (on) or releases the stream's WifiLocks, the same pair
// Moonlight holds: WIFI_MODE_FULL_HIGH_PERF always, plus
// WIFI_MODE_FULL_LOW_LATENCY on Android 10+. Returns 1 when at least one
// lock is held (on) or both are released (off).
static int jni_wifi_lock(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, int on) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (!on) {
		release_lock(env, &g_lock_low_latency);
		release_lock(env, &g_lock_high_perf);
		LOGI("Wi-Fi locks released");
		return 1;
	}
	if (activity == NULL) {
		return 0;
	}

	int sdk = 0;
	jclass ver = (*env)->FindClass(env, "android/os/Build$VERSION");
	if (ver != NULL) {
		jfieldID f = (*env)->GetStaticFieldID(env, ver, "SDK_INT", "I");
		if (f != NULL) {
			sdk = (*env)->GetStaticIntField(env, ver, f);
		}
		(*env)->DeleteLocalRef(env, ver);
	}
	jni_clear(env);

	jclass actClass = (*env)->GetObjectClass(env, activity);
	jmethodID getApp = (*env)->GetMethodID(env, actClass, "getApplicationContext", "()Landroid/content/Context;");
	jobject appCtx = getApp ? (*env)->CallObjectMethod(env, activity, getApp) : NULL;
	(*env)->DeleteLocalRef(env, actClass);
	if (jni_clear(env) || appCtx == NULL) {
		LOGE("no application context");
		return 0;
	}
	jclass ctxClass = (*env)->GetObjectClass(env, appCtx);
	jmethodID getSvc = (*env)->GetMethodID(env, ctxClass, "getSystemService", "(Ljava/lang/String;)Ljava/lang/Object;");
	jstring wifiName = (*env)->NewStringUTF(env, "wifi");
	jobject wm = getSvc ? (*env)->CallObjectMethod(env, appCtx, getSvc, wifiName) : NULL;
	(*env)->DeleteLocalRef(env, wifiName);
	(*env)->DeleteLocalRef(env, ctxClass);
	(*env)->DeleteLocalRef(env, appCtx);
	if (jni_clear(env) || wm == NULL) {
		LOGE("no WifiManager");
		return 0;
	}

	int ok = acquire_lock(env, wm, WIFI_MODE_FULL_HIGH_PERF, "USBridge high perf", &g_lock_high_perf);
	if (sdk >= 29) {
		ok |= acquire_lock(env, wm, WIFI_MODE_FULL_LOW_LATENCY, "USBridge low latency", &g_lock_low_latency);
	}
	(*env)->DeleteLocalRef(env, wm);
	return ok;
}
*/
import "C"

import (
	"fmt"

	"usbridge-client/internal/gui/controller"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

// Android's Wi-Fi also scans in the background and saves power between
// packets, which on a stream are the same periodic stutters as on a
// desktop. While a stream runs the client holds the same pair of WifiLocks
// Moonlight on Android does: WIFI_MODE_FULL_HIGH_PERF, plus
// WIFI_MODE_FULL_LOW_LATENCY on Android 10+ (power saving off, scans held
// back while the app is in the foreground with the screen on). No prompt
// -- WAKE_LOCK is a normal permission. What apps cannot change (location
// Wi-Fi scanning, the vendor's switch-to-mobile-data) the Wi-Fi tips
// dialog explains (controller/wifi_tips.go).
func setAndroidWiFiLock(on bool) error {
	return driver.RunNative(func(context any) error {
		androidCtx, ok := context.(*driver.AndroidContext)
		if !ok || androidCtx == nil {
			return fmt.Errorf("android context unavailable")
		}
		on1 := C.int(0)
		if on {
			on1 = 1
		}
		if C.jni_wifi_lock(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), on1) != 1 {
			return fmt.Errorf("Wi-Fi lock (on=%v) failed", on)
		}
		return nil
	})
}

// syncAndroidWiFiLock follows the stream: held while streaming, released
// once it stops. Called from updateStatus. The first stream of a run also
// brings up the Wi-Fi tips when Android's location Wi-Fi scanning is on.
func (mw *MainWindow) syncAndroidWiFiLock(isStreaming bool) {
	if mw.androidWiFiLock.Load() == isStreaming {
		return
	}
	if err := setAndroidWiFiLock(isStreaming); err != nil {
		logrus.Warnf("[wlan] %v", err)
		return
	}
	mw.androidWiFiLock.Store(isStreaming)
	logrus.Infof("[wlan] Wi-Fi high perf / low latency locks: %v", isStreaming)
	if isStreaming {
		fyne.Do(func() { controller.MaybeShowWiFiTipsOnStream(mw.window) })
	}
}
