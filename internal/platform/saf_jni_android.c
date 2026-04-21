#include <jni.h>
#include <android/log.h>
#include <stdlib.h>

#define LOG_TAG "SAF_JNI"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)
#define LOGW(...) __android_log_print(ANDROID_LOG_WARN, LOG_TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

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
        (*env)->DeleteLocalRef(env, exception);
        return;
    }

    // Получаем getMessage()
    jmethodID getMessage = (*env)->GetMethodID(env, throwableClass, "getMessage", "()Ljava/lang/String;");
    if (getMessage != NULL) {
        // Вызываем getMessage()
        jstring messageObj = (jstring)(*env)->CallObjectMethod(env, exception, getMessage);
        if (messageObj != NULL) {
            const char *message = (*env)->GetStringUTFChars(env, messageObj, NULL);
            if (message != NULL) {
                LOGE("%s: Exception message: %s", prefix, message);
                (*env)->ReleaseStringUTFChars(env, messageObj, message);
            }
            (*env)->DeleteLocalRef(env, messageObj);
        }
    }

    // Получаем имя класса exception
    jclass exceptionClass = (*env)->GetObjectClass(env, exception);
    jclass classClass = (*env)->FindClass(env, "java/lang/Class");
    if (classClass != NULL && exceptionClass != NULL) {
        jmethodID getName = (*env)->GetMethodID(env, classClass, "getName", "()Ljava/lang/String;");
        if (getName != NULL) {
            jstring nameObj = (jstring)(*env)->CallObjectMethod(env, exceptionClass, getName);
            if (nameObj != NULL) {
                const char *name = (*env)->GetStringUTFChars(env, nameObj, NULL);
                if (name != NULL) {
                    LOGE("%s: Exception class: %s", prefix, name);
                    (*env)->ReleaseStringUTFChars(env, nameObj, name);
                }
                (*env)->DeleteLocalRef(env, nameObj);
            }
        }
    }

    if (classClass) (*env)->DeleteLocalRef(env, classClass);
    if (exceptionClass) (*env)->DeleteLocalRef(env, exceptionClass);
    if (throwableClass) (*env)->DeleteLocalRef(env, throwableClass);
    (*env)->DeleteLocalRef(env, exception);
}

// Вызов Java метода takePersistableUriPermission через JNI
int jni_takePersistableUriPermission(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *uriString) {
    LOGI("jni_takePersistableUriPermission: %s", uriString);

    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject ctx = (jobject)ctx_ptr;

    // Получаем ContentResolver
    jclass contextClass = (*env)->GetObjectClass(env, ctx);
    if (contextClass == NULL) {
        LOGE("Failed to get Context class");
        return -1;
    }

    jmethodID getContentResolver = (*env)->GetMethodID(env, contextClass,
        "getContentResolver", "()Landroid/content/ContentResolver;");

    if (getContentResolver == NULL) {
        LOGE("Failed to find getContentResolver method");
        (*env)->DeleteLocalRef(env, contextClass);
        return -1;
    }

    jobject contentResolver = (*env)->CallObjectMethod(env, ctx, getContentResolver);
    (*env)->DeleteLocalRef(env, contextClass);

    if (contentResolver == NULL) {
        LOGE("Failed to get ContentResolver");
        return -1;
    }

    // Парсим URI
    jclass uriClass = (*env)->FindClass(env, "android/net/Uri");
    if (uriClass == NULL) {
        LOGE("Failed to find Uri class");
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    jmethodID parseMethod = (*env)->GetStaticMethodID(env, uriClass,
        "parse", "(Ljava/lang/String;)Landroid/net/Uri;");
    if (parseMethod == NULL) {
        LOGE("Failed to find Uri.parse method");
        (*env)->DeleteLocalRef(env, uriClass);
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    jstring jUriString = (*env)->NewStringUTF(env, uriString);
    jobject uri = (*env)->CallStaticObjectMethod(env, uriClass, parseMethod, jUriString);
    (*env)->DeleteLocalRef(env, uriClass);
    (*env)->DeleteLocalRef(env, jUriString);

    if (uri == NULL) {
        LOGE("Failed to parse URI");
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    // Получаем флаги
    jclass intentClass = (*env)->FindClass(env, "android/content/Intent");
    if (intentClass == NULL) {
        LOGE("Failed to find Intent class");
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    jfieldID readFlag = (*env)->GetStaticFieldID(env, intentClass,
        "FLAG_GRANT_READ_URI_PERMISSION", "I");
    jfieldID writeFlag = (*env)->GetStaticFieldID(env, intentClass,
        "FLAG_GRANT_WRITE_URI_PERMISSION", "I");

    if (readFlag == NULL || writeFlag == NULL) {
        LOGE("Failed to get permission flags");
        (*env)->DeleteLocalRef(env, intentClass);
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    jint flags = (*env)->GetStaticIntField(env, intentClass, readFlag)
               | (*env)->GetStaticIntField(env, intentClass, writeFlag);
    (*env)->DeleteLocalRef(env, intentClass);

    // Вызываем takePersistableUriPermission
    jclass resolverClass = (*env)->GetObjectClass(env, contentResolver);
    jmethodID takePermMethod = (*env)->GetMethodID(env, resolverClass,
        "takePersistableUriPermission", "(Landroid/net/Uri;I)V");

    if (takePermMethod == NULL) {
        LOGE("Failed to find takePersistableUriPermission method");
        (*env)->DeleteLocalRef(env, resolverClass);
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    (*env)->CallVoidMethod(env, contentResolver, takePermMethod, uri, flags);
    (*env)->DeleteLocalRef(env, resolverClass);

    // Проверяем ошибки
    if ((*env)->ExceptionCheck(env)) {
        LOGE("Exception occurred during takePersistableUriPermission");
        logExceptionDetails(env, "takePersistableUriPermission");
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    LOGI("takePersistableUriPermission SUCCESS");
    (*env)->DeleteLocalRef(env, uri);
    (*env)->DeleteLocalRef(env, contentResolver);
    return 0;
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

    // Получаем ContentResolver
    jclass contextClass = (*env)->GetObjectClass(env, ctx);
    if (contextClass == NULL) {
        LOGE("❌ [JNI-OPENFD-1] Failed to get Context class");
        return -1;
    }

    jmethodID getContentResolver = (*env)->GetMethodID(env, contextClass,
        "getContentResolver", "()Landroid/content/ContentResolver;");

    if (getContentResolver == NULL) {
        LOGE("❌ [JNI-OPENFD-2] Failed to find getContentResolver method");
        logExceptionDetails(env, "[JNI-OPENFD-2]");
        (*env)->DeleteLocalRef(env, contextClass);
        return -1;
    }

    jobject contentResolver = (*env)->CallObjectMethod(env, ctx, getContentResolver);
    (*env)->DeleteLocalRef(env, contextClass);

    if (contentResolver == NULL) {
        LOGE("❌ [JNI-OPENFD-3] Failed to get ContentResolver");
        logExceptionDetails(env, "[JNI-OPENFD-3]");
        return -1;
    }

    // Парсим URI
    jclass uriClass = (*env)->FindClass(env, "android/net/Uri");
    if (uriClass == NULL) {
        LOGE("❌ [JNI-OPENFD-4] Failed to find Uri class");
        logExceptionDetails(env, "[JNI-OPENFD-4]");
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    jmethodID parseMethod = (*env)->GetStaticMethodID(env, uriClass,
        "parse", "(Ljava/lang/String;)Landroid/net/Uri;");
    if (parseMethod == NULL) {
        LOGE("❌ [JNI-OPENFD-5] Failed to find Uri.parse method");
        logExceptionDetails(env, "[JNI-OPENFD-5]");
        (*env)->DeleteLocalRef(env, uriClass);
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    jstring jUriString = (*env)->NewStringUTF(env, uriString);
    if (jUriString == NULL) {
        LOGE("❌ [JNI-OPENFD-6] Failed to create Java string");
        (*env)->DeleteLocalRef(env, uriClass);
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    jobject uri = (*env)->CallStaticObjectMethod(env, uriClass, parseMethod, jUriString);
    (*env)->DeleteLocalRef(env, uriClass);
    (*env)->DeleteLocalRef(env, jUriString);

    if (uri == NULL) {
        LOGE("❌ [JNI-OPENFD-7] Failed to parse URI");
        logExceptionDetails(env, "[JNI-OPENFD-7]");
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    // Открываем ParcelFileDescriptor
    jclass resolverClass = (*env)->GetObjectClass(env, contentResolver);
    if (resolverClass == NULL) {
        LOGE("❌ [JNI-OPENFD-8] Failed to get ContentResolver class");
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    jmethodID openFdMethod = (*env)->GetMethodID(env, resolverClass,
        "openFileDescriptor", "(Landroid/net/Uri;Ljava/lang/String;)Landroid/os/ParcelFileDescriptor;");

    if (openFdMethod == NULL) {
        LOGE("❌ [JNI-OPENFD-9] Failed to find openFileDescriptor method");
        logExceptionDetails(env, "[JNI-OPENFD-9]");
        (*env)->DeleteLocalRef(env, resolverClass);
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    jstring jMode = (*env)->NewStringUTF(env, mode);
    if (jMode == NULL) {
        LOGE("❌ [JNI-OPENFD-10] Failed to create mode string");
        (*env)->DeleteLocalRef(env, resolverClass);
        (*env)->DeleteLocalRef(env, uri);
        (*env)->DeleteLocalRef(env, contentResolver);
        return -1;
    }

    LOGI("⚠️  [JNI-OPENFD-11] Calling openFileDescriptor(uri, '%s')...", mode);
    jobject pfd = (*env)->CallObjectMethod(env, contentResolver, openFdMethod, uri, jMode);
    (*env)->DeleteLocalRef(env, resolverClass);
    (*env)->DeleteLocalRef(env, jMode);
    (*env)->DeleteLocalRef(env, uri);
    (*env)->DeleteLocalRef(env, contentResolver);

    if (pfd == NULL || (*env)->ExceptionCheck(env)) {
        LOGE("❌ [JNI-OPENFD-11] Failed to open ParcelFileDescriptor");
        logExceptionDetails(env, "[JNI-OPENFD-11]");
        return -1;
    }

    // Получаем detachFd()
    jclass pfdClass = (*env)->GetObjectClass(env, pfd);
    if (pfdClass == NULL) {
        LOGE("❌ [JNI-OPENFD-12] Failed to get ParcelFileDescriptor class");
        (*env)->DeleteLocalRef(env, pfd);
        return -1;
    }

    jmethodID detachFdMethod = (*env)->GetMethodID(env, pfdClass, "detachFd", "()I");
    if (detachFdMethod == NULL) {
        LOGE("❌ [JNI-OPENFD-13] Failed to find detachFd method");
        logExceptionDetails(env, "[JNI-OPENFD-13]");
        (*env)->DeleteLocalRef(env, pfdClass);
        (*env)->DeleteLocalRef(env, pfd);
        return -1;
    }

    jint fd = (*env)->CallIntMethod(env, pfd, detachFdMethod);
    (*env)->DeleteLocalRef(env, pfdClass);
    (*env)->DeleteLocalRef(env, pfd);

    // Проверяем ошибки
    if ((*env)->ExceptionCheck(env)) {
        LOGE("❌ [JNI-OPENFD-14] Exception during detachFd");
        logExceptionDetails(env, "[JNI-OPENFD-14]");
        return -1;
    }

    if (fd < 0) {
        LOGE("❌ [JNI-OPENFD-14] Invalid fd value: %d", fd);
        return -1;
    }

    LOGI("═══════════════════════════════════════════════════════════════");
    LOGI("✅ [JNI-OPENFD-SUCCESS] jni_openFileDescriptor SUCCESS, fd=%d", fd);
    LOGI("═══════════════════════════════════════════════════════════════");

    return (int)fd;
}

// Запускает SAF пикер через NbdBridge.startSAFPicker()
int jni_startSAFPicker(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
    LOGI("🔧 [JNI-SAF] jni_startSAFPicker called");
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject ctx = (jobject)ctx_ptr;

    // В Android при вызове из сторонних потоков FindClass может не найти классы приложения.
    // Самый надежный способ - получить класс через объект, который у нас уже есть,
    // но NbdBridge - это статический объект (Kotlin object).
    // Попробуем сначала стандартный поиск.
    jclass nbdBridgeClass = (*env)->FindClass(env, "com/usbridge/client/NbdBridge");
    
    if (nbdBridgeClass == NULL) {
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        LOGW("⚠️ [JNI-SAF] Direct FindClass failed, trying via Activity ClassLoader");
        
        // Попытка найти класс через ClassLoader активности
        jclass activityClass = (*env)->GetObjectClass(env, ctx);
        jclass classClass = (*env)->FindClass(env, "java/lang/Class");
        jmethodID getClassLoaderMethod = (*env)->GetMethodID(env, activityClass, "getClassLoader", "()Ljava/lang/ClassLoader;");
        jobject classLoader = (*env)->CallObjectMethod(env, ctx, getClassLoaderMethod);
        jclass classLoaderClass = (*env)->FindClass(env, "java/lang/ClassLoader");
        jmethodID loadClassMethod = (*env)->GetMethodID(env, classLoaderClass, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;");
        
        jstring className = (*env)->NewStringUTF(env, "com.usbridge.client.NbdBridge");
        nbdBridgeClass = (jclass)(*env)->CallObjectMethod(env, classLoader, loadClassMethod, className);
        
        (*env)->DeleteLocalRef(env, className);
        (*env)->DeleteLocalRef(env, classLoader);
        (*env)->DeleteLocalRef(env, activityClass);
    }

    if (nbdBridgeClass == NULL) {
        LOGE("❌ [JNI-SAF] Failed to find NbdBridge class even via ClassLoader");
        if ((*env)->ExceptionCheck(env)) logExceptionDetails(env, "findNbdBridge");
        return -1;
    }

    jmethodID startPickerMethod = (*env)->GetStaticMethodID(env, nbdBridgeClass, "startSAFPicker", "()V");
    if (startPickerMethod == NULL) {
        LOGE("❌ [JNI-SAF] Failed to find startSAFPicker method");
        (*env)->DeleteLocalRef(env, nbdBridgeClass);
        return -1;
    }

    (*env)->CallStaticVoidMethod(env, nbdBridgeClass, startPickerMethod);
    (*env)->DeleteLocalRef(env, nbdBridgeClass);

    if ((*env)->ExceptionCheck(env)) {
        LOGE("❌ [JNI-SAF] Exception in startSAFPicker");
        logExceptionDetails(env, "startSAFPicker");
        return -1;
    }

    LOGI("✅ [JNI-SAF] startSAFPicker call successful");
    return 0;
}
