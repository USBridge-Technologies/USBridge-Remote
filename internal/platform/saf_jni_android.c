#include <jni.h>
#include <android/log.h>
#include <stdlib.h>

#define LOG_TAG "SAF_JNI"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

// Вызов Java метода takePersistableUriPermission через JNI
int jni_takePersistableUriPermission(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *uriString) {
    LOGI("jni_takePersistableUriPermission: %s", uriString);

    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject ctx = (jobject)ctx_ptr;

    // Получаем ContentResolver
    jclass contextClass = (*env)->GetObjectClass(env, ctx);
    jmethodID getContentResolver = (*env)->GetMethodID(env, contextClass,
        "getContentResolver", "()Landroid/content/ContentResolver;");

    if (getContentResolver == NULL) {
        LOGE("Failed to find getContentResolver method");
        return -1;
    }

    jobject contentResolver = (*env)->CallObjectMethod(env, ctx, getContentResolver);
    if (contentResolver == NULL) {
        LOGE("Failed to get ContentResolver");
        return -1;
    }

    // Парсим URI
    jclass uriClass = (*env)->FindClass(env, "android/net/Uri");
    if (uriClass == NULL) {
        LOGE("Failed to find Uri class");
        return -1;
    }

    jmethodID parseMethod = (*env)->GetStaticMethodID(env, uriClass,
        "parse", "(Ljava/lang/String;)Landroid/net/Uri;");
    if (parseMethod == NULL) {
        LOGE("Failed to find Uri.parse method");
        return -1;
    }

    jstring jUriString = (*env)->NewStringUTF(env, uriString);
    jobject uri = (*env)->CallStaticObjectMethod(env, uriClass, parseMethod, jUriString);
    if (uri == NULL) {
        LOGE("Failed to parse URI");
        (*env)->DeleteLocalRef(env, jUriString);
        return -1;
    }

    // Получаем флаги
    jclass intentClass = (*env)->FindClass(env, "android/content/Intent");
    if (intentClass == NULL) {
        LOGE("Failed to find Intent class");
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, uri);
        return -1;
    }

    jfieldID readFlag = (*env)->GetStaticFieldID(env, intentClass,
        "FLAG_GRANT_READ_URI_PERMISSION", "I");
    jfieldID writeFlag = (*env)->GetStaticFieldID(env, intentClass,
        "FLAG_GRANT_WRITE_URI_PERMISSION", "I");

    if (readFlag == NULL || writeFlag == NULL) {
        LOGE("Failed to get permission flags");
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, uri);
        return -1;
    }

    jint flags = (*env)->GetStaticIntField(env, intentClass, readFlag)
               | (*env)->GetStaticIntField(env, intentClass, writeFlag);

    // Вызываем takePersistableUriPermission
    jclass resolverClass = (*env)->GetObjectClass(env, contentResolver);
    jmethodID takePermMethod = (*env)->GetMethodID(env, resolverClass,
        "takePersistableUriPermission", "(Landroid/net/Uri;I)V");

    if (takePermMethod == NULL) {
        LOGE("Failed to find takePersistableUriPermission method");
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, uri);
        return -1;
    }

    (*env)->CallVoidMethod(env, contentResolver, takePermMethod, uri, flags);

    // Проверяем ошибки
    if ((*env)->ExceptionCheck(env)) {
        LOGE("Exception occurred during takePersistableUriPermission");
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, uri);
        return -1;
    }

    LOGI("takePersistableUriPermission SUCCESS");
    (*env)->DeleteLocalRef(env, jUriString);
    (*env)->DeleteLocalRef(env, uri);
    return 0;
}

// Получает детальное сообщение об ошибке из exception
static void logExceptionDetails(JNIEnv *env, const char *prefix) {
    if (!(*env)->ExceptionCheck(env)) {
        return;
    }

    jthrowable exception = (*env)->ExceptionOccurred(env);
    if (exception == NULL) {
        return;
    }

    // Очищаем exception чтобы можно было вызывать JNI методы
    (*env)->ExceptionClear(env);

    // Получаем класс Throwable
    jclass throwableClass = (*env)->FindClass(env, "java/lang/Throwable");
    if (throwableClass == NULL) {
        LOGE("%s: Failed to find Throwable class", prefix);
        return;
    }

    // Получаем getMessage()
    jmethodID getMessage = (*env)->GetMethodID(env, throwableClass, "getMessage", "()Ljava/lang/String;");
    if (getMessage == NULL) {
        LOGE("%s: Failed to find getMessage method", prefix);
        return;
    }

    // Вызываем getMessage()
    jstring messageObj = (jstring)(*env)->CallObjectMethod(env, exception, getMessage);
    if (messageObj != NULL) {
        const char *message = (*env)->GetStringUTFChars(env, messageObj, NULL);
        if (message != NULL) {
            LOGE("%s: Exception message: %s", prefix, message);
            (*env)->ReleaseStringUTFChars(env, messageObj, message);
        }
    }

    // Получаем имя класса exception
    jclass exceptionClass = (*env)->GetObjectClass(env, exception);
    jclass classClass = (*env)->FindClass(env, "java/lang/Class");
    if (classClass != NULL) {
        jmethodID getName = (*env)->GetMethodID(env, classClass, "getName", "()Ljava/lang/String;");
        if (getName != NULL) {
            jstring nameObj = (jstring)(*env)->CallObjectMethod(env, exceptionClass, getName);
            if (nameObj != NULL) {
                const char *name = (*env)->GetStringUTFChars(env, nameObj, NULL);
                if (name != NULL) {
                    LOGE("%s: Exception class: %s", prefix, name);
                    (*env)->ReleaseStringUTFChars(env, nameObj, name);
                }
            }
        }
    }

    (*env)->DeleteLocalRef(env, exception);
}

// Открывает файловый дескриптор через SAF
int jni_openFileDescriptor(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *uriString, const char *mode) {
    LOGI("═══════════════════════════════════════════════════════════════");
    LOGI("🔧 [JNI-OPENFD-START] jni_openFileDescriptor called");
    LOGI("📍 [JNI-OPENFD-START] URI: %s", uriString);
    LOGI("📍 [JNI-OPENFD-START] Mode: %s", mode);
    LOGI("═══════════════════════════════════════════════════════════════");

    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject ctx = (jobject)ctx_ptr;

    LOGI("📍 [JNI-OPENFD-1] Getting Context class...");
    // Получаем ContentResolver
    jclass contextClass = (*env)->GetObjectClass(env, ctx);
    if (contextClass == NULL) {
        LOGE("❌ [JNI-OPENFD-1] Failed to get Context class");
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-1] Context class obtained");

    LOGI("📍 [JNI-OPENFD-2] Getting getContentResolver method...");
    jmethodID getContentResolver = (*env)->GetMethodID(env, contextClass,
        "getContentResolver", "()Landroid/content/ContentResolver;");

    if (getContentResolver == NULL) {
        LOGE("❌ [JNI-OPENFD-2] Failed to find getContentResolver method");
        logExceptionDetails(env, "[JNI-OPENFD-2]");
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-2] getContentResolver method found");

    LOGI("📍 [JNI-OPENFD-3] Calling getContentResolver()...");
    jobject contentResolver = (*env)->CallObjectMethod(env, ctx, getContentResolver);
    if (contentResolver == NULL) {
        LOGE("❌ [JNI-OPENFD-3] Failed to get ContentResolver");
        logExceptionDetails(env, "[JNI-OPENFD-3]");
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-3] ContentResolver obtained");

    // Парсим URI
    LOGI("📍 [JNI-OPENFD-4] Finding Uri class...");
    jclass uriClass = (*env)->FindClass(env, "android/net/Uri");
    if (uriClass == NULL) {
        LOGE("❌ [JNI-OPENFD-4] Failed to find Uri class");
        logExceptionDetails(env, "[JNI-OPENFD-4]");
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-4] Uri class found");

    LOGI("📍 [JNI-OPENFD-5] Getting Uri.parse method...");
    jmethodID parseMethod = (*env)->GetStaticMethodID(env, uriClass,
        "parse", "(Ljava/lang/String;)Landroid/net/Uri;");
    if (parseMethod == NULL) {
        LOGE("❌ [JNI-OPENFD-5] Failed to find Uri.parse method");
        logExceptionDetails(env, "[JNI-OPENFD-5]");
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-5] Uri.parse method found");

    LOGI("📍 [JNI-OPENFD-6] Creating Java string for URI...");
    jstring jUriString = (*env)->NewStringUTF(env, uriString);
    if (jUriString == NULL) {
        LOGE("❌ [JNI-OPENFD-6] Failed to create Java string");
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-6] Java string created");

    LOGI("📍 [JNI-OPENFD-7] Parsing URI string...");
    jobject uri = (*env)->CallStaticObjectMethod(env, uriClass, parseMethod, jUriString);
    if (uri == NULL) {
        LOGE("❌ [JNI-OPENFD-7] Failed to parse URI");
        logExceptionDetails(env, "[JNI-OPENFD-7]");
        (*env)->DeleteLocalRef(env, jUriString);
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-7] URI parsed successfully");

    // Открываем ParcelFileDescriptor
    LOGI("📍 [JNI-OPENFD-8] Getting ContentResolver class...");
    jclass resolverClass = (*env)->GetObjectClass(env, contentResolver);
    if (resolverClass == NULL) {
        LOGE("❌ [JNI-OPENFD-8] Failed to get ContentResolver class");
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, uri);
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-8] ContentResolver class obtained");

    LOGI("📍 [JNI-OPENFD-9] Getting openFileDescriptor method...");
    jmethodID openFdMethod = (*env)->GetMethodID(env, resolverClass,
        "openFileDescriptor", "(Landroid/net/Uri;Ljava/lang/String;)Landroid/os/ParcelFileDescriptor;");

    if (openFdMethod == NULL) {
        LOGE("❌ [JNI-OPENFD-9] Failed to find openFileDescriptor method");
        logExceptionDetails(env, "[JNI-OPENFD-9]");
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, uri);
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-9] openFileDescriptor method found");

    LOGI("📍 [JNI-OPENFD-10] Creating Java string for mode '%s'...", mode);
    jstring jMode = (*env)->NewStringUTF(env, mode);
    if (jMode == NULL) {
        LOGE("❌ [JNI-OPENFD-10] Failed to create mode string");
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, uri);
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-10] Mode string created");

    LOGI("📍 [JNI-OPENFD-11] Calling openFileDescriptor(uri, '%s')...", mode);
    LOGI("⚠️  [JNI-OPENFD-11] This may take time for Google Drive files!");
    jobject pfd = (*env)->CallObjectMethod(env, contentResolver, openFdMethod, uri, jMode);

    if (pfd == NULL || (*env)->ExceptionCheck(env)) {
        LOGE("❌ [JNI-OPENFD-11] Failed to open ParcelFileDescriptor");
        if ((*env)->ExceptionCheck(env)) {
            LOGE("❌ [JNI-OPENFD-11] Exception occurred:");
            logExceptionDetails(env, "[JNI-OPENFD-11]");
            (*env)->ExceptionClear(env);
        }
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, jMode);
        (*env)->DeleteLocalRef(env, uri);
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-11] ParcelFileDescriptor opened successfully");

    // Получаем detachFd()
    LOGI("📍 [JNI-OPENFD-12] Getting ParcelFileDescriptor class...");
    jclass pfdClass = (*env)->GetObjectClass(env, pfd);
    if (pfdClass == NULL) {
        LOGE("❌ [JNI-OPENFD-12] Failed to get ParcelFileDescriptor class");
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, jMode);
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, pfd);
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-12] ParcelFileDescriptor class obtained");

    LOGI("📍 [JNI-OPENFD-13] Getting detachFd method...");
    jmethodID detachFdMethod = (*env)->GetMethodID(env, pfdClass, "detachFd", "()I");

    if (detachFdMethod == NULL) {
        LOGE("❌ [JNI-OPENFD-13] Failed to find detachFd method");
        logExceptionDetails(env, "[JNI-OPENFD-13]");
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, jMode);
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, pfd);
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-13] detachFd method found");

    LOGI("📍 [JNI-OPENFD-14] Calling detachFd()...");
    jint fd = (*env)->CallIntMethod(env, pfd, detachFdMethod);

    // Проверяем ошибки
    if ((*env)->ExceptionCheck(env)) {
        LOGE("❌ [JNI-OPENFD-14] Exception during detachFd");
        logExceptionDetails(env, "[JNI-OPENFD-14]");
        (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, jMode);
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, pfd);
        return -1;
    }
    LOGI("✅ [JNI-OPENFD-14] detachFd() returned: %d", fd);

    if (fd < 0) {
        LOGE("❌ [JNI-OPENFD-14] Invalid fd value: %d", fd);
        (*env)->DeleteLocalRef(env, jUriString);
        (*env)->DeleteLocalRef(env, jMode);
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, pfd);
        return -1;
    }

    LOGI("═══════════════════════════════════════════════════════════════");
    LOGI("✅ [JNI-OPENFD-SUCCESS] jni_openFileDescriptor SUCCESS, fd=%d", fd);
    LOGI("═══════════════════════════════════════════════════════════════");

    (*env)->DeleteLocalRef(env, jUriString);
    (*env)->DeleteLocalRef(env, jMode);
    (*env)->DeleteLocalRef(env, uri);
    (*env)->DeleteLocalRef(env, pfd);
    return (int)fd;
}
