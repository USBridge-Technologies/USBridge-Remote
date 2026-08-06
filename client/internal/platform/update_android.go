//go:build android

// Self-update support (internal/update/apply_android.go) calls plain
// MainActivity instance methods through driver.RunNative + JNI, exactly
// like internal/clipboard/backend_android.go does: RunNative hands us a
// fresh JNIEnv/Activity pointer pair per call, so there's nothing to cache
// here (unlike internal/platform/saf_jni_android.c, which is invoked *from*
// Java and so must cache a JavaVM global).
//
// The Android-side logic lives directly on MainActivity (see
// android/app/src/main/java/io/usbridge/client/MainActivity.kt, the
// "Self-update" section) rather than a separate bridge object, mirroring
// the clipboard backend.
package platform

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <jni.h>
#include <android/log.h>

#define LOG_TAG "USUpdateAndroid"
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

static int jni_update_installApk(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *path) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL) return 0;

	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID m = (*env)->GetMethodID(env, cls, "installApk", "(Ljava/lang/String;)Z");
	if (m == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		LOGE("MainActivity.installApk() not found");
		return 0;
	}
	jstring jpath = (*env)->NewStringUTF(env, path);
	jboolean v = (*env)->CallBooleanMethod(env, activity, m, jpath);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		v = JNI_FALSE;
	}
	(*env)->DeleteLocalRef(env, jpath);
	(*env)->DeleteLocalRef(env, cls);
	return v == JNI_TRUE ? 1 : 0;
}

static int jni_update_canRequestPackageInstalls(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL) return 0;

	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID m = (*env)->GetMethodID(env, cls, "canRequestPackageInstalls", "()Z");
	if (m == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		LOGE("MainActivity.canRequestPackageInstalls() not found");
		return 0;
	}
	jboolean v = (*env)->CallBooleanMethod(env, activity, m);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		v = JNI_FALSE;
	}
	(*env)->DeleteLocalRef(env, cls);
	return v == JNI_TRUE ? 1 : 0;
}

static void jni_update_requestInstallPermission(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL) return;

	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID m = (*env)->GetMethodID(env, cls, "requestInstallPermission", "()V");
	if (m == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		LOGE("MainActivity.requestInstallPermission() not found");
		return;
	}
	(*env)->CallVoidMethod(env, activity, m);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
	}
	(*env)->DeleteLocalRef(env, cls);
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"fyne.io/fyne/v2/driver"
)

// InstallAPK hands path (a file this app already owns — see
// MainActivity.kt's installApk doc comment) to the system PackageInstaller
// via a content:// URI, returning whether the intent was launched
// successfully. Android's own signing-certificate check on install is the
// actual security gate here — see internal/update/apply_android.go.
func InstallAPK(path string) (bool, error) {
	var ok bool
	err := driver.RunNative(func(ctx any) error {
		androidCtx, valid := ctx.(*driver.AndroidContext)
		if !valid || androidCtx == nil {
			return fmt.Errorf("update: android context unavailable")
		}
		cPath := C.CString(path)
		defer C.free(unsafe.Pointer(cPath))
		ok = C.jni_update_installApk(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), cPath) != 0
		return nil
	})
	return ok, err
}

// CanRequestPackageInstalls reports whether this app currently holds the
// "install unknown apps" permission — required (Android 8+) before
// InstallAPK's intent can succeed.
func CanRequestPackageInstalls() (bool, error) {
	var ok bool
	err := driver.RunNative(func(ctx any) error {
		androidCtx, valid := ctx.(*driver.AndroidContext)
		if !valid || androidCtx == nil {
			return fmt.Errorf("update: android context unavailable")
		}
		ok = C.jni_update_canRequestPackageInstalls(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx)) != 0
		return nil
	})
	return ok, err
}

// RequestInstallPermission opens this app's "Install unknown apps" Settings
// screen — there is no runtime permission dialog for this one, Settings is
// the only way to grant it.
func RequestInstallPermission() error {
	return driver.RunNative(func(ctx any) error {
		androidCtx, valid := ctx.(*driver.AndroidContext)
		if !valid || androidCtx == nil {
			return fmt.Errorf("update: android context unavailable")
		}
		C.jni_update_requestInstallPermission(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx))
		return nil
	})
}
