//go:build android

package controller

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
#include <jni.h>
#include <android/log.h>

#define LOG_TAG "USBridgeBrowser"
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

static int jni_openExternalURL(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *url) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (activity == NULL) {
		LOGE("Activity context is null");
		return 0;
	}

	jclass activityClass = (*env)->GetObjectClass(env, activity);
	if (activityClass == NULL) {
		if ((*env)->ExceptionCheck(env)) {
			(*env)->ExceptionClear(env);
		}
		LOGE("Failed to resolve activity class");
		return 0;
	}

	jmethodID openMethod = (*env)->GetMethodID(env, activityClass, "openExternalUrl", "(Ljava/lang/String;)Z");
	if (openMethod == NULL) {
		if ((*env)->ExceptionCheck(env)) {
			(*env)->ExceptionClear(env);
		}
		(*env)->DeleteLocalRef(env, activityClass);
		LOGE("MainActivity.openExternalUrl(String) not found");
		return 0;
	}

	jstring jURL = (*env)->NewStringUTF(env, url);
	jboolean ok = (*env)->CallBooleanMethod(env, activity, openMethod, jURL);
	(*env)->DeleteLocalRef(env, jURL);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		ok = JNI_FALSE;
	}
	(*env)->DeleteLocalRef(env, activityClass);
	return ok == JNI_TRUE ? 1 : 0;
}
*/
import "C"

import (
	"fmt"
	"net/url"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

func openExternalURL(app fyne.App, uri *url.URL) error {
	_ = app
	var opened bool
	err := driver.RunNative(func(context any) error {
		androidCtx, ok := context.(*driver.AndroidContext)
		if !ok || androidCtx == nil {
			return fmt.Errorf("android context unavailable")
		}
		cURL := C.CString(uri.String())
		defer C.free(unsafe.Pointer(cURL))
		if C.jni_openExternalURL(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), cURL) == 1 {
			opened = true
			return nil
		}
		return fmt.Errorf("android activity rejected browser launch")
	})
	if err != nil {
		return err
	}
	if !opened {
		return fmt.Errorf("browser was not opened")
	}
	return nil
}
