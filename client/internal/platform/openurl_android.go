//go:build android

package platform

/*
#include <jni.h>
#include <stdlib.h>
#include <string.h>

// MainActivity.takeOpenedUri(): the usbridge:// link onNewIntent saw since
// the last call, or null. NULL here also when the activity has no such
// method (an older Java side).
static char* jni_takeOpenedUri(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (activity == NULL) return NULL;

	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID m = (*env)->GetMethodID(env, cls, "takeOpenedUri", "()Ljava/lang/String;");
	(*env)->DeleteLocalRef(env, cls);
	if (m == NULL) {
		(*env)->ExceptionClear(env);
		return NULL;
	}
	jstring s = (jstring)(*env)->CallObjectMethod(env, activity, m);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		return NULL;
	}
	if (s == NULL) return NULL;
	const char *utf = (*env)->GetStringUTFChars(env, s, NULL);
	char *out = utf ? strdup(utf) : NULL;
	if (utf) (*env)->ReleaseStringUTFChars(env, s, utf);
	(*env)->DeleteLocalRef(env, s);
	return out;
}
*/
import "C"

import (
	"unsafe"

	"fyne.io/fyne/v2/driver"
)

// TakeOpenedURL returns (once) a usbridge:// link opened while the app was
// already running, "" if none came since the last call. The link the app
// was started with comes through GetIntentDataURI.
func TakeOpenedURL() string {
	var out string
	_ = driver.RunNative(func(ctx any) error {
		a, ok := ctx.(*driver.AndroidContext)
		if !ok {
			return nil
		}
		if s := C.jni_takeOpenedUri(C.uintptr_t(a.Env), C.uintptr_t(a.Ctx)); s != nil {
			out = C.GoString(s)
			C.free(unsafe.Pointer(s))
		}
		return nil
	})
	return out
}
