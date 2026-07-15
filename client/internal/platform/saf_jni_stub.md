# JNI Implementation Guide for SAF (Storage Access Framework)

This file contains instructions for implementing JNI methods to work with Android SAF.

## Needs to be implemented

### 1. TakePersistableUriPermission

**Goal:** Persist permanent access permission to a content:// URI

**Java/Kotlin code (add to MainActivity or FyneActivity):**

```java
import android.content.Intent;
import android.net.Uri;

public class SAFHelper {
    private Activity activity;

    public SAFHelper(Activity activity) {
        this.activity = activity;
    }

    public void takePersistableUriPermission(String uriString) throws Exception {
        Uri uri = Uri.parse(uriString);

        final int takeFlags = Intent.FLAG_GRANT_READ_URI_PERMISSION
                            | Intent.FLAG_GRANT_WRITE_URI_PERMISSION;

        activity.getContentResolver().takePersistableUriPermission(uri, takeFlags);
    }
}
```

**CGO wrapper (add to saf_android.go):**

```go
/*
#cgo LDFLAGS: -landroid -llog

#include <jni.h>
#include <android/log.h>
#include <stdlib.h>

// Calls the Java takePersistableUriPermission method via JNI
int jni_takePersistableUriPermission(JNIEnv *env, jobject activity, const char *uriString) {
    __android_log_print(ANDROID_LOG_INFO, "SAF_JNI", "takePersistableUriPermission: %s", uriString);

    // Get the Activity class
    jclass activityClass = (*env)->GetObjectClass(env, activity);

    // Get the ContentResolver
    jmethodID getContentResolver = (*env)->GetMethodID(env, activityClass,
        "getContentResolver", "()Landroid/content/ContentResolver;");
    jobject contentResolver = (*env)->CallObjectMethod(env, activity, getContentResolver);

    // Parse the URI
    jclass uriClass = (*env)->FindClass(env, "android/net/Uri");
    jmethodID parseMethod = (*env)->GetStaticMethodID(env, uriClass,
        "parse", "(Ljava/lang/String;)Landroid/net/Uri;");
    jstring jUriString = (*env)->NewStringUTF(env, uriString);
    jobject uri = (*env)->CallStaticObjectMethod(env, uriClass, parseMethod, jUriString);

    // Get the flags
    jclass intentClass = (*env)->FindClass(env, "android/content/Intent");
    jfieldID readFlag = (*env)->GetStaticFieldID(env, intentClass,
        "FLAG_GRANT_READ_URI_PERMISSION", "I");
    jfieldID writeFlag = (*env)->GetStaticFieldID(env, intentClass,
        "FLAG_GRANT_WRITE_URI_PERMISSION", "I");
    jint flags = (*env)->GetStaticIntField(env, intentClass, readFlag)
               | (*env)->GetStaticIntField(env, intentClass, writeFlag);

    // Call takePersistableUriPermission
    jclass resolverClass = (*env)->GetObjectClass(env, contentResolver);
    jmethodID takePermMethod = (*env)->GetMethodID(env, resolverClass,
        "takePersistableUriPermission", "(Landroid/net/Uri;I)V");
    (*env)->CallVoidMethod(env, contentResolver, takePermMethod, uri, flags);

    // Check for errors
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
        return -1;
    }

    __android_log_print(ANDROID_LOG_INFO, "SAF_JNI", "takePersistableUriPermission SUCCESS");
    return 0;
}
*/
import "C"
```

### 2. OpenFileDescriptor

**Goal:** Open a file descriptor via SAF for direct access

**Java/Kotlin code:**

```java
import android.os.ParcelFileDescriptor;

public class SAFHelper {
    public int openFileDescriptor(String uriString, String mode) throws Exception {
        Uri uri = Uri.parse(uriString);

        ParcelFileDescriptor pfd = activity.getContentResolver()
            .openFileDescriptor(uri, mode);

        if (pfd == null) {
            throw new Exception("Failed to open file descriptor");
        }

        // Detach the FD so Go can manage it
        int fd = pfd.detachFd();

        return fd;
    }
}
```

**CGO wrapper:**

```go
/*
int jni_openFileDescriptor(JNIEnv *env, jobject activity, const char *uriString, const char *mode) {
    __android_log_print(ANDROID_LOG_INFO, "SAF_JNI", "openFileDescriptor: %s, mode: %s", uriString, mode);

    // Get the ContentResolver
    jclass activityClass = (*env)->GetObjectClass(env, activity);
    jmethodID getContentResolver = (*env)->GetMethodID(env, activityClass,
        "getContentResolver", "()Landroid/content/ContentResolver;");
    jobject contentResolver = (*env)->CallObjectMethod(env, activity, getContentResolver);

    // Parse the URI
    jclass uriClass = (*env)->FindClass(env, "android/net/Uri");
    jmethodID parseMethod = (*env)->GetStaticMethodID(env, uriClass,
        "parse", "(Ljava/lang/String;)Landroid/net/Uri;");
    jstring jUriString = (*env)->NewStringUTF(env, uriString);
    jobject uri = (*env)->CallStaticObjectMethod(env, uriClass, parseMethod, jUriString);

    // Open the ParcelFileDescriptor
    jclass resolverClass = (*env)->GetObjectClass(env, contentResolver);
    jmethodID openFdMethod = (*env)->GetMethodID(env, resolverClass,
        "openFileDescriptor", "(Landroid/net/Uri;Ljava/lang/String;)Landroid/os/ParcelFileDescriptor;");
    jstring jMode = (*env)->NewStringUTF(env, mode);
    jobject pfd = (*env)->CallObjectMethod(env, contentResolver, openFdMethod, uri, jMode);

    if (pfd == NULL) {
        __android_log_print(ANDROID_LOG_ERROR, "SAF_JNI", "Failed to open ParcelFileDescriptor");
        return -1;
    }

    // Get detachFd()
    jclass pfdClass = (*env)->GetObjectClass(env, pfd);
    jmethodID detachFdMethod = (*env)->GetMethodID(env, pfdClass, "detachFd", "()I");
    jint fd = (*env)->CallIntMethod(env, pfd, detachFdMethod);

    // Check for errors
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
        return -1;
    }

    __android_log_print(ANDROID_LOG_INFO, "SAF_JNI", "openFileDescriptor SUCCESS, fd=%d", fd);
    return (int)fd;
}
*/
import "C"
```

## Integration with Fyne

Fyne provides access to the Android context via `app.Driver().RunNative()`:

```go
app.Driver().RunNative(func(ctx any) {
    // ctx is the android.app.Activity or android.content.Context
    // JNI functions can be called here
})
```

## Alternative approach: using gomobile

If CGO/JNI is too complex, gomobile bind can be used instead:

```go
// saf_gomobile.go
package safhelper

import (
    "golang.org/x/mobile/app"
    "golang.org/x/mobile/bind/java"
)

func TakePersistableUriPermission(uriString string) error {
    return java.Do(func(env *java.Env) error {
        // Java code here
        return nil
    })
}
```

## How to test

1. Build the app for Android
2. Run logcat to view the logs:
   ```bash
   adb logcat | grep -E "SAF|NBD"
   ```
3. In the app, tap "Add image" and select a file
4. Check the logs for [SAF-STEP-X] messages
5. If you see "JNI method not implemented" — the CGO code above needs to be added

## Useful links

- Android SAF documentation: https://developer.android.com/guide/topics/providers/document-provider
- CGO documentation: https://golang.org/cmd/cgo/
- Fyne mobile: https://docs.fyne.io/started/mobile
- JNI specification: https://docs.oracle.com/javase/8/docs/technotes/guides/jni/spec/jniTOC.html
