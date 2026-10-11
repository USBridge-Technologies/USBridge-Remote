//go:build android

package analytics

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
#include <jni.h>
#include <android/log.h>

static void jni_log_analytics(uintptr_t jni_env_ptr, uintptr_t ctx_ptr,
	const char *name, const char *k1, const char *v1, const char *k2, const char *v2) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL || name == NULL) return;

	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID m = (*env)->GetMethodID(env, cls, "logAnalyticsEvent",
		"(Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;)V");
	if (m == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		return;
	}
	jstring jn = (*env)->NewStringUTF(env, name);
	jstring jk1 = (*env)->NewStringUTF(env, k1 ? k1 : "");
	jstring jv1 = (*env)->NewStringUTF(env, v1 ? v1 : "");
	jstring jk2 = (*env)->NewStringUTF(env, k2 ? k2 : "");
	jstring jv2 = (*env)->NewStringUTF(env, v2 ? v2 : "");
	(*env)->CallVoidMethod(env, activity, m, jn, jk1, jv1, jk2, jv2);
	if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
	(*env)->DeleteLocalRef(env, jn);
	(*env)->DeleteLocalRef(env, jk1);
	(*env)->DeleteLocalRef(env, jv1);
	(*env)->DeleteLocalRef(env, jk2);
	(*env)->DeleteLocalRef(env, jv2);
	(*env)->DeleteLocalRef(env, cls);
}
*/
import "C"

import (
	"unsafe"

	"fyne.io/fyne/v2/driver"
)

func emit(name string, a, b param) {
	// Off the caller's goroutine: logEvent is local and batched, and
	// RunNative must not run nested on the UI thread that is saving
	// or connecting.
	go func() {
		defer func() { _ = recover() }()
		_ = driver.RunNative(func(ctx any) error {
			androidCtx, ok := ctx.(*driver.AndroidContext)
			if !ok || androidCtx == nil || androidCtx.Env == 0 || androidCtx.Ctx == 0 {
				return nil
			}
			cname := C.CString(name)
			ck1 := C.CString(a.key)
			cv1 := C.CString(a.value)
			ck2 := C.CString(b.key)
			cv2 := C.CString(b.value)
			defer C.free(unsafe.Pointer(cname))
			defer C.free(unsafe.Pointer(ck1))
			defer C.free(unsafe.Pointer(cv1))
			defer C.free(unsafe.Pointer(ck2))
			defer C.free(unsafe.Pointer(cv2))
			C.jni_log_analytics(
				C.uintptr_t(androidCtx.Env),
				C.uintptr_t(androidCtx.Ctx),
				cname, ck1, cv1, ck2, cv2,
			)
			return nil
		})
	}()
}
