//go:build android

package gui

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
#include <string.h>
#include <jni.h>

static void exportClearExc(JNIEnv *env) {
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
	}
}

// MainActivity.saveToDownloads(path, name, mime): where the file landed, "" on failure.
static char *jni_saveToDownloads(uintptr_t env_ptr, uintptr_t ctx_ptr, const char *path, const char *name, const char *mime) {
	JNIEnv *env = (JNIEnv *)env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (activity == NULL) return NULL;
	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID m = (*env)->GetMethodID(env, cls, "saveToDownloads", "(Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;)Ljava/lang/String;");
	if (m == NULL) { exportClearExc(env); (*env)->DeleteLocalRef(env, cls); return NULL; }
	jstring jp = (*env)->NewStringUTF(env, path);
	jstring jn = (*env)->NewStringUTF(env, name);
	jstring jm = (*env)->NewStringUTF(env, mime);
	jstring res = (jstring)(*env)->CallObjectMethod(env, activity, m, jp, jn, jm);
	char *out = NULL;
	if ((*env)->ExceptionCheck(env)) {
		exportClearExc(env);
	} else if (res != NULL) {
		const char *s = (*env)->GetStringUTFChars(env, res, NULL);
		if (s != NULL) {
			out = strdup(s);
			(*env)->ReleaseStringUTFChars(env, res, s);
		}
		(*env)->DeleteLocalRef(env, res);
	}
	(*env)->DeleteLocalRef(env, jp);
	(*env)->DeleteLocalRef(env, jn);
	(*env)->DeleteLocalRef(env, jm);
	(*env)->DeleteLocalRef(env, cls);
	return out;
}

// MainActivity.shareFile(path, mime, title): 1 when the share sheet was launched.
static int jni_shareFile(uintptr_t env_ptr, uintptr_t ctx_ptr, const char *path, const char *mime, const char *title) {
	JNIEnv *env = (JNIEnv *)env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (activity == NULL) return 0;
	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID m = (*env)->GetMethodID(env, cls, "shareFile", "(Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;)Z");
	if (m == NULL) { exportClearExc(env); (*env)->DeleteLocalRef(env, cls); return 0; }
	jstring jp = (*env)->NewStringUTF(env, path);
	jstring jm = (*env)->NewStringUTF(env, mime);
	jstring jt = (*env)->NewStringUTF(env, title);
	jboolean ok = (*env)->CallBooleanMethod(env, activity, m, jp, jm, jt);
	if ((*env)->ExceptionCheck(env)) {
		exportClearExc(env);
		ok = JNI_FALSE;
	}
	(*env)->DeleteLocalRef(env, jp);
	(*env)->DeleteLocalRef(env, jm);
	(*env)->DeleteLocalRef(env, jt);
	(*env)->DeleteLocalRef(env, cls);
	return ok == JNI_TRUE ? 1 : 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"fyne.io/fyne/v2/driver"
)

func fileExportAvailable() bool { return true }

// saveFileToDownloads copies the cache file at path into the phone's public
// Downloads folder and returns where it landed.
func saveFileToDownloads(path, name, mime string) (string, error) {
	var where string
	err := driver.RunNative(func(context any) error {
		ctx, ok := context.(*driver.AndroidContext)
		if !ok || ctx == nil {
			return fmt.Errorf("android context unavailable")
		}
		cp, cn, cm := C.CString(path), C.CString(name), C.CString(mime)
		defer C.free(unsafe.Pointer(cp))
		defer C.free(unsafe.Pointer(cn))
		defer C.free(unsafe.Pointer(cm))
		res := C.jni_saveToDownloads(C.uintptr_t(ctx.Env), C.uintptr_t(ctx.Ctx), cp, cn, cm)
		if res == nil {
			return fmt.Errorf("saving to Downloads failed")
		}
		defer C.free(unsafe.Pointer(res))
		where = C.GoString(res)
		if where == "" {
			return fmt.Errorf("saving to Downloads failed")
		}
		return nil
	})
	return where, err
}

// shareFile opens the system share sheet for the cache file at path.
func shareFile(path, mime, title string) error {
	return driver.RunNative(func(context any) error {
		ctx, ok := context.(*driver.AndroidContext)
		if !ok || ctx == nil {
			return fmt.Errorf("android context unavailable")
		}
		cp, cm, ct := C.CString(path), C.CString(mime), C.CString(title)
		defer C.free(unsafe.Pointer(cp))
		defer C.free(unsafe.Pointer(cm))
		defer C.free(unsafe.Pointer(ct))
		if C.jni_shareFile(C.uintptr_t(ctx.Env), C.uintptr_t(ctx.Ctx), cp, cm, ct) != 1 {
			return fmt.Errorf("share sheet did not open")
		}
		return nil
	})
}
