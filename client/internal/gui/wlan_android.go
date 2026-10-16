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

static jobject g_wifi_lock = NULL;

static int jni_clear(JNIEnv *env) {
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionDescribe(env);
		(*env)->ExceptionClear(env);
		return 1;
	}
	return 0;
}

// jni_wifi_lock holds (on) or releases a WifiManager.WifiLock:
// WIFI_MODE_FULL_LOW_LATENCY (4) on Android 10+, WIFI_MODE_FULL_HIGH_PERF
// (3) before that. Returns 1 on success.
static int jni_wifi_lock(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, int on) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (!on) {
		if (g_wifi_lock != NULL) {
			jclass lc = (*env)->GetObjectClass(env, g_wifi_lock);
			jmethodID held = (*env)->GetMethodID(env, lc, "isHeld", "()Z");
			jmethodID release = (*env)->GetMethodID(env, lc, "release", "()V");
			if (held && release && (*env)->CallBooleanMethod(env, g_wifi_lock, held)) {
				(*env)->CallVoidMethod(env, g_wifi_lock, release);
			}
			jni_clear(env);
			(*env)->DeleteLocalRef(env, lc);
			(*env)->DeleteGlobalRef(env, g_wifi_lock);
			g_wifi_lock = NULL;
			LOGI("Wi-Fi low latency lock released");
		}
		return 1;
	}
	if (g_wifi_lock != NULL) {
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
	jint mode = sdk >= 29 ? 4 : 3;

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

	jclass wmClass = (*env)->GetObjectClass(env, wm);
	jmethodID create = (*env)->GetMethodID(env, wmClass, "createWifiLock", "(ILjava/lang/String;)Landroid/net/wifi/WifiManager$WifiLock;");
	jstring tag = (*env)->NewStringUTF(env, "USBridge stream");
	jobject lock = create ? (*env)->CallObjectMethod(env, wm, create, mode, tag) : NULL;
	(*env)->DeleteLocalRef(env, tag);
	(*env)->DeleteLocalRef(env, wmClass);
	(*env)->DeleteLocalRef(env, wm);
	if (jni_clear(env) || lock == NULL) {
		LOGE("createWifiLock failed");
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
		LOGE("WifiLock.acquire failed (WAKE_LOCK permission?)");
		return 0;
	}
	g_wifi_lock = (*env)->NewGlobalRef(env, lock);
	(*env)->DeleteLocalRef(env, lock);
	LOGI("Wi-Fi lock held (mode %d, sdk %d)", mode, sdk);
	return 1;
}
*/
import "C"

import (
	"fmt"

	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

// Android's Wi-Fi also scans in the background and saves power between
// packets, which on a stream are the same periodic stutters as on a
// desktop. While a stream runs the client holds a WifiLock in
// WIFI_MODE_FULL_LOW_LATENCY (Android 10+; HIGH_PERF before): power saving
// off and scans held back while the app is in the foreground with the
// screen on. Moonlight on Android does the same. No prompt -- WAKE_LOCK is
// a normal permission.
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
// once it stops. Called from updateStatus.
func (mw *MainWindow) syncAndroidWiFiLock(isStreaming bool) {
	if mw.androidWiFiLock.Load() == isStreaming {
		return
	}
	if err := setAndroidWiFiLock(isStreaming); err != nil {
		logrus.Warnf("[wlan] %v", err)
		return
	}
	mw.androidWiFiLock.Store(isStreaming)
	logrus.Infof("[wlan] Wi-Fi low latency lock: %v", isStreaming)
}
