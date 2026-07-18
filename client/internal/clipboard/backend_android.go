//go:build android

// Package clipboard's Android backend calls plain MainActivity instance
// methods through driver.RunNative + JNI, exactly like
// internal/service/tailscale_android.go does: RunNative hands us a fresh
// JNIEnv/Activity pointer pair per call, so (unlike
// internal/platform/saf_jni_android.c, which is invoked *from* Java and so
// must cache a JavaVM global) there's nothing to cache here.
//
// The Android-side logic lives directly on MainActivity (see
// android/app/src/main/java/com/usbridge/client/MainActivity.kt, the
// "Clipboard bridge" section) rather than a separate bridge object, mirroring
// how tailscale_android.go calls plain instance methods like
// getCacheDirAbsolutePath().
package clipboard

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <jni.h>
#include <android/log.h>

#define LOG_TAG "USClipboardAndroid"
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

static char *jni_clip_getString(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *method_name) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL) return NULL;

	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID m = (*env)->GetMethodID(env, cls, method_name, "()Ljava/lang/String;");
	if (m == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		LOGE("MainActivity.%s() not found", method_name);
		return NULL;
	}
	jstring js = (jstring)(*env)->CallObjectMethod(env, activity, m);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		js = NULL;
	}
	(*env)->DeleteLocalRef(env, cls);
	if (js == NULL) return NULL;
	const char *utf = (*env)->GetStringUTFChars(env, js, NULL);
	char *dup = utf ? strdup(utf) : NULL;
	if (utf) (*env)->ReleaseStringUTFChars(env, js, utf);
	(*env)->DeleteLocalRef(env, js);
	return dup;
}

static int jni_clip_getInt(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *method_name) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL) return -1;

	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID m = (*env)->GetMethodID(env, cls, method_name, "()I");
	if (m == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		return -1;
	}
	jint v = (*env)->CallIntMethod(env, activity, m);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		v = -1;
	}
	(*env)->DeleteLocalRef(env, cls);
	return (int)v;
}

static int jni_clip_setText(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *text) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL) return 0;

	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID m = (*env)->GetMethodID(env, cls, "clipboardSetText", "(Ljava/lang/String;)Z");
	if (m == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		return 0;
	}
	jstring jtext = (*env)->NewStringUTF(env, text);
	jboolean v = (*env)->CallBooleanMethod(env, activity, m, jtext);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		v = JNI_FALSE;
	}
	(*env)->DeleteLocalRef(env, jtext);
	(*env)->DeleteLocalRef(env, cls);
	return v == JNI_TRUE ? 1 : 0;
}

static int jni_clip_setFile(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *path, const char *mime) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL) return 0;

	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID m = (*env)->GetMethodID(env, cls, "clipboardSetFile", "(Ljava/lang/String;Ljava/lang/String;)Z");
	if (m == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		return 0;
	}
	jstring jpath = (*env)->NewStringUTF(env, path);
	jstring jmime = (*env)->NewStringUTF(env, mime);
	jboolean v = (*env)->CallBooleanMethod(env, activity, m, jpath, jmime);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		v = JNI_FALSE;
	}
	(*env)->DeleteLocalRef(env, jpath);
	(*env)->DeleteLocalRef(env, jmime);
	(*env)->DeleteLocalRef(env, cls);
	return v == JNI_TRUE ? 1 : 0;
}
*/
import "C"

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"fyne.io/fyne/v2/driver"
)

type androidBackend struct {
	cacheDirOnce sync.Once
	cacheDirPath string
	cacheDirErr  error
}

// NewBackend returns the native Android clipboard backend. winHandle is
// unused — Android clipboard access goes through the Activity handed to us
// per-call by driver.RunNative, not through a fyne.Window.
func NewBackend(winHandle any) Backend { return &androidBackend{} }

func (b *androidBackend) ChangeStamp() (string, error) {
	n, err := b.callInt("clipboardChangeCount")
	if err != nil {
		return "", err
	}
	return strconv.Itoa(n), nil
}

// Read resolves the Android primary clip. Note: since Android 10 (API 29),
// the platform blocks clipboard reads from apps that aren't in the
// foreground for privacy reasons — clipboardRead() will report "none" (not
// an error) while USBridge is backgrounded, same as every other remote-
// clipboard app on Android. This is an OS restriction, not a bug here.
func (b *androidBackend) Read() (Content, bool, error) {
	kind, err := b.callString("clipboardRead")
	if err != nil || kind == "" || kind == "none" {
		return Content{}, false, err
	}

	switch kind {
	case "text":
		text, err := b.callString("clipboardReadText")
		if err != nil || text == "" {
			return Content{}, false, err
		}
		return Content{Kind: KindText, Text: text}, true, nil

	case "image", "file":
		fd, err := b.callInt("clipboardReadFd")
		if err != nil || fd < 0 {
			return Content{}, false, err
		}
		name, _ := b.callString("clipboardReadFileName")
		if name == "" {
			name = "file"
		}
		f := os.NewFile(uintptr(fd), name)
		data, readErr := io.ReadAll(f)
		f.Close()
		if readErr != nil {
			return Content{}, false, readErr
		}

		if kind == "image" {
			mimeType, _ := b.callString("clipboardReadMime")
			if mimeType != "image/png" {
				// Normalize to PNG (e.g. a JPEG shared from Gallery/Photos)
				// so Content.Image's "always PNG" contract holds regardless
				// of what the source app put on the clipboard.
				if img, _, decErr := image.Decode(bytes.NewReader(data)); decErr == nil {
					var buf bytes.Buffer
					if png.Encode(&buf, img) == nil {
						data = buf.Bytes()
					}
				}
			}
			return Content{Kind: KindImage, Image: data}, true, nil
		}
		return Content{Kind: KindFile, Files: []FileItem{{Name: name, Data: data}}}, true, nil
	}

	return Content{}, false, nil
}

func (b *androidBackend) Write(content Content) error {
	switch content.Kind {
	case KindText:
		ok, err := b.callBoolText(content.Text)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("clipboard: set text failed")
		}
		return nil

	case KindImage:
		if len(content.Image) == 0 {
			return fmt.Errorf("clipboard: empty image")
		}
		path, err := b.writeTempFile("clipboard.png", content.Image)
		if err != nil {
			return err
		}
		ok, err := b.callBoolFile(path, "image/png")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("clipboard: set image failed")
		}
		return nil

	case KindFile:
		if len(content.Files) == 0 {
			return fmt.Errorf("clipboard: no files")
		}
		// Android's ClipData carries one URI per clipboard set here — a
		// multi-file USBridge payload degrades to its first file, since most
		// Android apps only look at clip item 0 on paste anyway.
		f := content.Files[0]
		name := sanitizeFileName(f.Name)
		path, err := b.writeTempFile(name, f.Data)
		if err != nil {
			return err
		}
		mimeType := mime.TypeByExtension(filepath.Ext(name))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		ok, err := b.callBoolFile(path, mimeType)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("clipboard: set file failed")
		}
		return nil

	default:
		return fmt.Errorf("clipboard: unsupported kind %q", content.Kind)
	}
}

func (b *androidBackend) callString(method string) (string, error) {
	var out string
	err := driver.RunNative(func(ctx any) error {
		androidCtx, ok := ctx.(*driver.AndroidContext)
		if !ok || androidCtx == nil {
			return fmt.Errorf("clipboard: android context unavailable")
		}
		cMethod := C.CString(method)
		defer C.free(unsafe.Pointer(cMethod))
		cstr := C.jni_clip_getString(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), cMethod)
		if cstr == nil {
			return nil
		}
		defer C.free(unsafe.Pointer(cstr))
		out = C.GoString(cstr)
		return nil
	})
	return out, err
}

func (b *androidBackend) callInt(method string) (int, error) {
	var out = -1
	err := driver.RunNative(func(ctx any) error {
		androidCtx, ok := ctx.(*driver.AndroidContext)
		if !ok || androidCtx == nil {
			return fmt.Errorf("clipboard: android context unavailable")
		}
		cMethod := C.CString(method)
		defer C.free(unsafe.Pointer(cMethod))
		out = int(C.jni_clip_getInt(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), cMethod))
		return nil
	})
	return out, err
}

func (b *androidBackend) callBoolText(text string) (bool, error) {
	var ok bool
	err := driver.RunNative(func(ctx any) error {
		androidCtx, valid := ctx.(*driver.AndroidContext)
		if !valid || androidCtx == nil {
			return fmt.Errorf("clipboard: android context unavailable")
		}
		cText := C.CString(text)
		defer C.free(unsafe.Pointer(cText))
		ok = C.jni_clip_setText(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), cText) != 0
		return nil
	})
	return ok, err
}

func (b *androidBackend) callBoolFile(path, mimeType string) (bool, error) {
	var ok bool
	err := driver.RunNative(func(ctx any) error {
		androidCtx, valid := ctx.(*driver.AndroidContext)
		if !valid || androidCtx == nil {
			return fmt.Errorf("clipboard: android context unavailable")
		}
		cPath := C.CString(path)
		defer C.free(unsafe.Pointer(cPath))
		cMime := C.CString(mimeType)
		defer C.free(unsafe.Pointer(cMime))
		ok = C.jni_clip_setFile(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), cPath, cMime) != 0
		return nil
	})
	return ok, err
}

// cacheDir returns (and creates once) a subdirectory of the app's cache dir
// to stage outgoing clipboard blobs in — the existing FileProvider's
// file_paths.xml already covers the whole cache dir ("cache-path path=.")
// so anything written here can be handed out as a content:// URI.
func (b *androidBackend) cacheDir() (string, error) {
	b.cacheDirOnce.Do(func() {
		dir, err := b.callString("getCacheDirAbsolutePath")
		if err != nil {
			b.cacheDirErr = err
			return
		}
		if dir == "" {
			b.cacheDirErr = fmt.Errorf("clipboard: android cache dir unavailable")
			return
		}
		dir = filepath.Join(dir, "usbridge-clipboard")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			b.cacheDirErr = err
			return
		}
		b.cacheDirPath = dir
	})
	return b.cacheDirPath, b.cacheDirErr
}

func (b *androidBackend) writeTempFile(name string, data []byte) (string, error) {
	dir, err := b.cacheDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), name))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
