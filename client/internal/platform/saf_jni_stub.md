# JNI Implementation Guide for SAF (Storage Access Framework)

Этот файл содержит инструкции по реализации JNI методов для работы с Android SAF.

## Необходимо реализовать

### 1. TakePersistableUriPermission

**Цель:** Сохранить постоянное разрешение на доступ к content:// URI

**Java/Kotlin код (добавить в MainActivity или FyneActivity):**

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

**CGO обертка (добавить в saf_android.go):**

```go
/*
#cgo LDFLAGS: -landroid -llog

#include <jni.h>
#include <android/log.h>
#include <stdlib.h>

// Вызов Java метода takePersistableUriPermission через JNI
int jni_takePersistableUriPermission(JNIEnv *env, jobject activity, const char *uriString) {
    __android_log_print(ANDROID_LOG_INFO, "SAF_JNI", "takePersistableUriPermission: %s", uriString);

    // Получаем класс Activity
    jclass activityClass = (*env)->GetObjectClass(env, activity);

    // Получаем ContentResolver
    jmethodID getContentResolver = (*env)->GetMethodID(env, activityClass,
        "getContentResolver", "()Landroid/content/ContentResolver;");
    jobject contentResolver = (*env)->CallObjectMethod(env, activity, getContentResolver);

    // Парсим URI
    jclass uriClass = (*env)->FindClass(env, "android/net/Uri");
    jmethodID parseMethod = (*env)->GetStaticMethodID(env, uriClass,
        "parse", "(Ljava/lang/String;)Landroid/net/Uri;");
    jstring jUriString = (*env)->NewStringUTF(env, uriString);
    jobject uri = (*env)->CallStaticObjectMethod(env, uriClass, parseMethod, jUriString);

    // Получаем флаги
    jclass intentClass = (*env)->FindClass(env, "android/content/Intent");
    jfieldID readFlag = (*env)->GetStaticFieldID(env, intentClass,
        "FLAG_GRANT_READ_URI_PERMISSION", "I");
    jfieldID writeFlag = (*env)->GetStaticFieldID(env, intentClass,
        "FLAG_GRANT_WRITE_URI_PERMISSION", "I");
    jint flags = (*env)->GetStaticIntField(env, intentClass, readFlag)
               | (*env)->GetStaticIntField(env, intentClass, writeFlag);

    // Вызываем takePersistableUriPermission
    jclass resolverClass = (*env)->GetObjectClass(env, contentResolver);
    jmethodID takePermMethod = (*env)->GetMethodID(env, resolverClass,
        "takePersistableUriPermission", "(Landroid/net/Uri;I)V");
    (*env)->CallVoidMethod(env, contentResolver, takePermMethod, uri, flags);

    // Проверяем ошибки
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

**Цель:** Открыть файловый дескриптор через SAF для прямого доступа

**Java/Kotlin код:**

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

        // Отсоединяем FD чтобы Go мог им управлять
        int fd = pfd.detachFd();

        return fd;
    }
}
```

**CGO обертка:**

```go
/*
int jni_openFileDescriptor(JNIEnv *env, jobject activity, const char *uriString, const char *mode) {
    __android_log_print(ANDROID_LOG_INFO, "SAF_JNI", "openFileDescriptor: %s, mode: %s", uriString, mode);

    // Получаем ContentResolver
    jclass activityClass = (*env)->GetObjectClass(env, activity);
    jmethodID getContentResolver = (*env)->GetMethodID(env, activityClass,
        "getContentResolver", "()Landroid/content/ContentResolver;");
    jobject contentResolver = (*env)->CallObjectMethod(env, activity, getContentResolver);

    // Парсим URI
    jclass uriClass = (*env)->FindClass(env, "android/net/Uri");
    jmethodID parseMethod = (*env)->GetStaticMethodID(env, uriClass,
        "parse", "(Ljava/lang/String;)Landroid/net/Uri;");
    jstring jUriString = (*env)->NewStringUTF(env, uriString);
    jobject uri = (*env)->CallStaticObjectMethod(env, uriClass, parseMethod, jUriString);

    // Открываем ParcelFileDescriptor
    jclass resolverClass = (*env)->GetObjectClass(env, contentResolver);
    jmethodID openFdMethod = (*env)->GetMethodID(env, resolverClass,
        "openFileDescriptor", "(Landroid/net/Uri;Ljava/lang/String;)Landroid/os/ParcelFileDescriptor;");
    jstring jMode = (*env)->NewStringUTF(env, mode);
    jobject pfd = (*env)->CallObjectMethod(env, contentResolver, openFdMethod, uri, jMode);

    if (pfd == NULL) {
        __android_log_print(ANDROID_LOG_ERROR, "SAF_JNI", "Failed to open ParcelFileDescriptor");
        return -1;
    }

    // Получаем detachFd()
    jclass pfdClass = (*env)->GetObjectClass(env, pfd);
    jmethodID detachFdMethod = (*env)->GetMethodID(env, pfdClass, "detachFd", "()I");
    jint fd = (*env)->CallIntMethod(env, pfd, detachFdMethod);

    // Проверяем ошибки
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

## Интеграция с Fyne

Fyne предоставляет доступ к Android context через `app.Driver().RunNative()`:

```go
app.Driver().RunNative(func(ctx any) {
    // ctx это android.app.Activity или android.content.Context
    // Здесь можно вызывать JNI функции
})
```

## Альтернативный подход: Использование gomobile

Если CGO/JNI слишком сложно, можно использовать gomobile bind:

```go
// saf_gomobile.go
package safhelper

import (
    "golang.org/x/mobile/app"
    "golang.org/x/mobile/bind/java"
)

func TakePersistableUriPermission(uriString string) error {
    return java.Do(func(env *java.Env) error {
        // Java код здесь
        return nil
    })
}
```

## Как протестировать

1. Соберите приложение для Android
2. Запустите logcat для просмотра логов:
   ```bash
   adb logcat | grep -E "SAF|NBD"
   ```
3. В приложении нажмите "Добавить образ" и выберите файл
4. Проверьте логи на наличие сообщений [SAF-STEP-X]
5. Если видите "JNI метод не реализован" - нужно добавить CGO код выше

## Полезные ссылки

- Android SAF документация: https://developer.android.com/guide/topics/providers/document-provider
- CGO документация: https://golang.org/cmd/cgo/
- Fyne mobile: https://docs.fyne.io/started/mobile
- JNI спецификация: https://docs.oracle.com/javase/8/docs/technotes/guides/jni/spec/jniTOC.html
