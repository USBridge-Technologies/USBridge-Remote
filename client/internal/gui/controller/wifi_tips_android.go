//go:build android

package controller

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
#include <jni.h>
#include <android/log.h>

#define LOG_TAG "USBridgeWiFiTips"
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

static int tips_clear(JNIEnv *env) {
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		return 1;
	}
	return 0;
}

// jni_scan_always_available is WifiManager.isScanAlwaysAvailable(): the
// "Wi-Fi scanning" switch under Location services. 1 on, 0 off, -1 unknown.
static int jni_scan_always_available(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (activity == NULL) {
		return -1;
	}
	jclass actClass = (*env)->GetObjectClass(env, activity);
	jmethodID getSvc = (*env)->GetMethodID(env, actClass, "getSystemService", "(Ljava/lang/String;)Ljava/lang/Object;");
	jstring wifiName = (*env)->NewStringUTF(env, "wifi");
	jobject wm = getSvc ? (*env)->CallObjectMethod(env, activity, getSvc, wifiName) : NULL;
	(*env)->DeleteLocalRef(env, wifiName);
	(*env)->DeleteLocalRef(env, actClass);
	if (tips_clear(env) || wm == NULL) {
		return -1;
	}
	jclass wmClass = (*env)->GetObjectClass(env, wm);
	jmethodID m = (*env)->GetMethodID(env, wmClass, "isScanAlwaysAvailable", "()Z");
	int result = -1;
	if (m != NULL) {
		jboolean on = (*env)->CallBooleanMethod(env, wm, m);
		if (!tips_clear(env)) {
			result = on ? 1 : 0;
		}
	}
	tips_clear(env);
	(*env)->DeleteLocalRef(env, wmClass);
	(*env)->DeleteLocalRef(env, wm);
	return result;
}

// jni_open_settings starts an Activity for the settings Intent action.
// Returns 1 when one was found.
static int jni_open_settings(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *action) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (activity == NULL) {
		return 0;
	}
	jclass intentClass = (*env)->FindClass(env, "android/content/Intent");
	if (tips_clear(env) || intentClass == NULL) {
		return 0;
	}
	jmethodID ctor = (*env)->GetMethodID(env, intentClass, "<init>", "(Ljava/lang/String;)V");
	jmethodID addFlags = (*env)->GetMethodID(env, intentClass, "addFlags", "(I)Landroid/content/Intent;");
	jstring jAction = (*env)->NewStringUTF(env, action);
	jobject intent = ctor ? (*env)->NewObject(env, intentClass, ctor, jAction) : NULL;
	(*env)->DeleteLocalRef(env, jAction);
	if (tips_clear(env) || intent == NULL) {
		(*env)->DeleteLocalRef(env, intentClass);
		return 0;
	}
	if (addFlags) {
		jobject r = (*env)->CallObjectMethod(env, intent, addFlags, (jint)0x10000000); // FLAG_ACTIVITY_NEW_TASK
		if (r) (*env)->DeleteLocalRef(env, r);
	}
	jclass actClass = (*env)->GetObjectClass(env, activity);
	jmethodID start = (*env)->GetMethodID(env, actClass, "startActivity", "(Landroid/content/Intent;)V");
	int ok = 0;
	if (start) {
		(*env)->CallVoidMethod(env, activity, start, intent);
		ok = tips_clear(env) ? 0 : 1; // ActivityNotFoundException: try the next one
	}
	tips_clear(env);
	(*env)->DeleteLocalRef(env, actClass);
	(*env)->DeleteLocalRef(env, intent);
	(*env)->DeleteLocalRef(env, intentClass);
	return ok;
}
*/
import "C"

import (
	"unsafe"

	"fyne.io/fyne/v2/driver"
)

func androidWiFiScanAlwaysOn() int {
	result := -1
	_ = driver.RunNative(func(context any) error {
		if ctx, ok := context.(*driver.AndroidContext); ok && ctx != nil {
			result = int(C.jni_scan_always_available(C.uintptr_t(ctx.Env), C.uintptr_t(ctx.Ctx)))
		}
		return nil
	})
	return result
}

// openAndroidSettings opens the first of actions this device has.
func openAndroidSettings(actions []string) bool {
	opened := false
	_ = driver.RunNative(func(context any) error {
		ctx, ok := context.(*driver.AndroidContext)
		if !ok || ctx == nil {
			return nil
		}
		for _, a := range actions {
			cs := C.CString(a)
			r := C.jni_open_settings(C.uintptr_t(ctx.Env), C.uintptr_t(ctx.Ctx), cs)
			C.free(unsafe.Pointer(cs))
			if r == 1 {
				opened = true
				return nil
			}
		}
		return nil
	})
	return opened
}
