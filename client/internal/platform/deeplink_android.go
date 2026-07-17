//go:build android
// +build android

package platform

/*
#cgo LDFLAGS: -landroid -llog

#include <jni.h>
#include <android/log.h>
#include <stdlib.h>
#include <string.h>

#define LOG_TAG "DeepLink"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

// Get intent data URI
char* jni_getIntentDataUri(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject activity = (jobject)ctx_ptr;

    if (activity == NULL) {
        LOGE("❌ [DEEPLINK] Activity context is null");
        return NULL;
    }

    // Get activity class
    jclass activityClass = (*env)->GetObjectClass(env, activity);
    if (activityClass == NULL) {
        LOGE("❌ [DEEPLINK] Failed to get activity class");
        return NULL;
    }

    // Get getIntent method
    jmethodID getIntentMethod = (*env)->GetMethodID(env, activityClass, "getIntent", "()Landroid/content/Intent;");
    if (getIntentMethod == NULL) {
        LOGE("❌ [DEEPLINK] Failed to find getIntent method");
        (*env)->DeleteLocalRef(env, activityClass);
        return NULL;
    }

    // Call getIntent
    jobject intent = (*env)->CallObjectMethod(env, activity, getIntentMethod);
    if (intent == NULL) {
        LOGI("⚠️ [DEEPLINK] No intent available");
        (*env)->DeleteLocalRef(env, activityClass);
        return NULL;
    }

    // Get Intent class
    jclass intentClass = (*env)->GetObjectClass(env, intent);
    if (intentClass == NULL) {
        LOGE("❌ [DEEPLINK] Failed to get Intent class");
        (*env)->DeleteLocalRef(env, intent);
        (*env)->DeleteLocalRef(env, activityClass);
        return NULL;
    }

    // Get getData method
    jmethodID getDataMethod = (*env)->GetMethodID(env, intentClass, "getData", "()Landroid/net/Uri;");
    if (getDataMethod == NULL) {
        LOGE("❌ [DEEPLINK] Failed to find getData method");
        (*env)->DeleteLocalRef(env, intentClass);
        (*env)->DeleteLocalRef(env, intent);
        (*env)->DeleteLocalRef(env, activityClass);
        return NULL;
    }

    // Call getData
    jobject uri = (*env)->CallObjectMethod(env, intent, getDataMethod);
    if (uri == NULL) {
        LOGI("ℹ️ [DEEPLINK] No data URI in intent");
        (*env)->DeleteLocalRef(env, intentClass);
        (*env)->DeleteLocalRef(env, intent);
        (*env)->DeleteLocalRef(env, activityClass);
        return NULL;
    }

    // Get Uri class
    jclass uriClass = (*env)->GetObjectClass(env, uri);
    if (uriClass == NULL) {
        LOGE("❌ [DEEPLINK] Failed to get Uri class");
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, intentClass);
        (*env)->DeleteLocalRef(env, intent);
        (*env)->DeleteLocalRef(env, activityClass);
        return NULL;
    }

    // Get toString method
    jmethodID toStringMethod = (*env)->GetMethodID(env, uriClass, "toString", "()Ljava/lang/String;");
    if (toStringMethod == NULL) {
        LOGE("❌ [DEEPLINK] Failed to find toString method");
        (*env)->DeleteLocalRef(env, uriClass);
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, intentClass);
        (*env)->DeleteLocalRef(env, intent);
        (*env)->DeleteLocalRef(env, activityClass);
        return NULL;
    }

    // Call toString
    jstring uriString = (jstring)(*env)->CallObjectMethod(env, uri, toStringMethod);
    if (uriString == NULL) {
        LOGE("❌ [DEEPLINK] Failed to convert URI to string");
        (*env)->DeleteLocalRef(env, uriClass);
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, intentClass);
        (*env)->DeleteLocalRef(env, intent);
        (*env)->DeleteLocalRef(env, activityClass);
        return NULL;
    }

    // Convert jstring to C string
    const char *uriCStr = (*env)->GetStringUTFChars(env, uriString, NULL);
    if (uriCStr == NULL) {
        LOGE("❌ [DEEPLINK] Failed to get UTF chars");
        (*env)->DeleteLocalRef(env, uriString);
        (*env)->DeleteLocalRef(env, uriClass);
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, intentClass);
        (*env)->DeleteLocalRef(env, intent);
        (*env)->DeleteLocalRef(env, activityClass);
        return NULL;
    }

    // Make a copy
    char *result = strdup(uriCStr);

    LOGI("✅ [DEEPLINK] Got URI: %s", result);

    // Clean up
    (*env)->ReleaseStringUTFChars(env, uriString, uriCStr);
    (*env)->DeleteLocalRef(env, uriString);
    (*env)->DeleteLocalRef(env, uriClass);
    (*env)->DeleteLocalRef(env, uri);
    (*env)->DeleteLocalRef(env, intentClass);
    (*env)->DeleteLocalRef(env, intent);
    (*env)->DeleteLocalRef(env, activityClass);

    return result;
}
*/
import "C"
import (
	"unsafe"

	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

// GetIntentDataURI retrieves the URI from the Intent (for deep links)
func GetIntentDataURI() (string, error) {
	var result string

	err := driver.RunNative(func(ctx any) error {
		androidCtx, ok := ctx.(*driver.AndroidContext)
		if !ok {
			return nil
		}

		// Get the URI via JNI
		cUri := C.jni_getIntentDataUri(
			C.uintptr_t(androidCtx.Env),
			C.uintptr_t(androidCtx.Ctx),
		)

		if cUri == nil {
			return nil
		}

		// Convert to a Go string
		result = C.GoString(cUri)

		// Free the C memory
		C.free(unsafe.Pointer(cUri))

		logrus.Infof("📱 [DEEPLINK] Got intent URI: %s", result)
		return nil
	})

	if err != nil {
		return "", err
	}

	return result, nil
}
