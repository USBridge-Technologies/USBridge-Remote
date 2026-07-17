#include <jni.h>
#include <android/log.h>
#include <stdlib.h>
#include <string.h>

#define LOG_TAG "SAF_JNI"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)
#define LOGW(...) __android_log_print(ANDROID_LOG_WARN, LOG_TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

static JavaVM *g_jvm = NULL;
static jobject g_ctx = NULL;

// Gets a detailed error message from the exception
static void logExceptionDetails(JNIEnv *env, const char *prefix) {
    if (!(*env)->ExceptionCheck(env)) {
        return;
    }

    jthrowable exception = (*env)->ExceptionOccurred(env);
    (*env)->ExceptionClear(env);

    jclass exceptionClass = (*env)->GetObjectClass(env, exception);
    jmethodID getMessageMethod = (*env)->GetMethodID(env, exceptionClass, "getMessage", "()Ljava/lang/String;");
    jstring message = (jstring)(*env)->CallObjectMethod(env, exception, getMessageMethod);

    if (message != NULL) {
        const char *cMessage = (*env)->GetStringUTFChars(env, message, NULL);
        LOGE("❌ [JNI-EXCEPTION] %s: %s", prefix, cMessage);
        (*env)->ReleaseStringUTFChars(env, message, cMessage);
        (*env)->DeleteLocalRef(env, message);
    } else {
        LOGE("❌ [JNI-EXCEPTION] %s: (no message)", prefix);
    }

    (*env)->DeleteLocalRef(env, exceptionClass);
    (*env)->DeleteLocalRef(env, exception);
}

// Helper for getting the Env in the current thread
static JNIEnv* get_env() {
    JNIEnv *env;
    if (g_jvm == NULL) return NULL;
    
    jint res = (*g_jvm)->GetEnv(g_jvm, (void**)&env, JNI_VERSION_1_6);
    if (res == JNI_OK) return env;
    
    if (res == JNI_EDETACHED) {
        if ((*g_jvm)->AttachCurrentThread(g_jvm, &env, NULL) != 0) {
            return NULL;
        }
        return env;
    }
    return NULL;
}

// Stores the context and VM (called from Go)
void jni_setContext(uintptr_t jni_vm_ptr, uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
    g_jvm = (JavaVM *)jni_vm_ptr;
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    if (g_ctx != NULL) {
        (*env)->DeleteGlobalRef(env, g_ctx);
    }
    g_ctx = (*env)->NewGlobalRef(env, (jobject)ctx_ptr);
    LOGI("✅ [JNI-SAF] Global context and VM stored");
}

// Finds the NbdBridge class
static jclass get_nbd_bridge_class(JNIEnv *env) {
    jclass cls = (*env)->FindClass(env, "com/usbridge/client/NbdBridge");
    if (cls == NULL) {
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        
        if (g_ctx == NULL) {
            LOGW("⚠️ [JNI-SAF] g_ctx is NULL, falling back to FindClass");
            return (*env)->FindClass(env, "com/usbridge/client/NbdBridge");
        }
        
        jclass activityClass = (*env)->GetObjectClass(env, g_ctx);
        jmethodID getClassLoaderMethod = (*env)->GetMethodID(env, activityClass, "getClassLoader", "()Ljava/lang/ClassLoader;");
        jobject classLoader = (*env)->CallObjectMethod(env, g_ctx, getClassLoaderMethod);
        jclass classLoaderClass = (*env)->FindClass(env, "java/lang/ClassLoader");
        jmethodID loadClassMethod = (*env)->GetMethodID(env, classLoaderClass, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;");
        
        jstring className = (*env)->NewStringUTF(env, "com.usbridge.client.NbdBridge");
        cls = (jclass)(*env)->CallObjectMethod(env, classLoader, loadClassMethod, className);
        
        (*env)->DeleteLocalRef(env, className);
        (*env)->DeleteLocalRef(env, classLoader);
        (*env)->DeleteLocalRef(env, activityClass);
    }
    return cls;
}

// Persists a persistable URI permission
int jni_takePersistableUriPermission(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *uriString) {
    LOGI("🔧 [JNI-SAF] jni_takePersistableUriPermission called for: %s", uriString);
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject ctx = (jobject)ctx_ptr;

    jstring jUriString = (*env)->NewStringUTF(env, uriString);
    jclass uriClass = (*env)->FindClass(env, "android/net/Uri");
    jmethodID parseMethod = (*env)->GetStaticMethodID(env, uriClass, "parse", "(Ljava/lang/String;)Landroid/net/Uri;");
    jobject uri = (*env)->CallStaticObjectMethod(env, uriClass, parseMethod, jUriString);

    jclass contextClass = (*env)->FindClass(env, "android/content/Context");
    jmethodID getContentResolverMethod = (*env)->GetMethodID(env, contextClass, "getContentResolver", "()Landroid/content/ContentResolver;");
    jobject contentResolver = (*env)->CallObjectMethod(env, ctx, getContentResolverMethod);

    jclass contentResolverClass = (*env)->FindClass(env, "android/content/ContentResolver");
    jmethodID takePermissionMethod = (*env)->GetMethodID(env, contentResolverClass, "takePersistableUriPermission", "(Landroid/net/Uri;I)V");

    (*env)->CallVoidMethod(env, contentResolver, takePermissionMethod, uri, 3);

    (*env)->DeleteLocalRef(env, jUriString);
    (*env)->DeleteLocalRef(env, uriClass);
    (*env)->DeleteLocalRef(env, uri);
    (*env)->DeleteLocalRef(env, contextClass);
    (*env)->DeleteLocalRef(env, contentResolver);
    (*env)->DeleteLocalRef(env, contentResolverClass);

    if ((*env)->ExceptionCheck(env)) {
        logExceptionDetails(env, "takePersistableUriPermission");
        return -1;
    }

    LOGI("✅ [JNI-SAF] Persistable permission SUCCESS");
    return 0;
}

// Opens a ParcelFileDescriptor and returns the FD
int jni_openFileDescriptor(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *uriString, const char *mode) {
    LOGI("🔧 [JNI-SAF] jni_openFileDescriptor called for: %s, mode: %s", uriString, mode);
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject ctx = (jobject)ctx_ptr;

    jstring jUriString = (*env)->NewStringUTF(env, uriString);
    jclass uriClass = (*env)->FindClass(env, "android/net/Uri");
    jmethodID parseMethod = (*env)->GetStaticMethodID(env, uriClass, "parse", "(Ljava/lang/String;)Landroid/net/Uri;");
    jobject uri = (*env)->CallStaticObjectMethod(env, uriClass, parseMethod, jUriString);

    jclass contextClass = (*env)->FindClass(env, "android/content/Context");
    jmethodID getContentResolverMethod = (*env)->GetMethodID(env, contextClass, "getContentResolver", "()Landroid/content/ContentResolver;");
    jobject contentResolver = (*env)->CallObjectMethod(env, ctx, getContentResolverMethod);

    jstring jMode = (*env)->NewStringUTF(env, mode);
    jclass contentResolverClass = (*env)->FindClass(env, "android/content/ContentResolver");
    jmethodID openPFDMethod = (*env)->GetMethodID(env, contentResolverClass, "openFileDescriptor", "(Landroid/net/Uri;Ljava/lang/String;)Landroid/os/ParcelFileDescriptor;");
    jobject pfd = (*env)->CallObjectMethod(env, contentResolver, openPFDMethod, uri, jMode);

    if (pfd == NULL) {
        LOGE("❌ [JNI-SAF] openFileDescriptor returned NULL");
        logExceptionDetails(env, "openFileDescriptor");
        return -1;
    }

    jclass pfdClass = (*env)->FindClass(env, "android/os/ParcelFileDescriptor");
    // detachFd() transfers fd ownership to native code so the Java GC
    // won't close it and fdsan won't report an ownership conflict.
    jmethodID detachFdMethod = (*env)->GetMethodID(env, pfdClass, "detachFd", "()I");
    jint fd = (*env)->CallIntMethod(env, pfd, detachFdMethod);

    (*env)->DeleteLocalRef(env, jUriString);
    (*env)->DeleteLocalRef(env, uriClass);
    (*env)->DeleteLocalRef(env, uri);
    (*env)->DeleteLocalRef(env, contextClass);
    (*env)->DeleteLocalRef(env, contentResolver);
    (*env)->DeleteLocalRef(env, jMode);
    (*env)->DeleteLocalRef(env, contentResolverClass);
    (*env)->DeleteLocalRef(env, pfd);
    (*env)->DeleteLocalRef(env, pfdClass);

    LOGI("✅ [JNI-OPENFD-SUCCESS] jni_openFileDescriptor SUCCESS, fd=%d", fd);

    return (int)fd;
}

// Launches the SAF picker
int jni_startSAFPicker() {
    JNIEnv *env = get_env();
    if (env == NULL) return -1;

    jclass nbdBridgeClass = get_nbd_bridge_class(env);
    if (nbdBridgeClass == NULL) return -1;

    jmethodID method = (*env)->GetStaticMethodID(env, nbdBridgeClass, "startSAFPicker", "()V");
    if (method == NULL) {
        (*env)->DeleteLocalRef(env, nbdBridgeClass);
        return -1;
    }

    (*env)->CallStaticVoidMethod(env, nbdBridgeClass, method);
    (*env)->DeleteLocalRef(env, nbdBridgeClass);
    return 0;
}

int jni_hasSAFResult() {
    JNIEnv *env = get_env();
    if (env == NULL) return 0;

    jclass cls = get_nbd_bridge_class(env);
    if (cls == NULL) return 0;

    jfieldID fid = (*env)->GetStaticFieldID(env, cls, "hasNewResult", "Z");
    if (fid == NULL) {
        (*env)->DeleteLocalRef(env, cls);
        return 0;
    }
    jboolean res = (*env)->GetStaticBooleanField(env, cls, fid);
    (*env)->DeleteLocalRef(env, cls);
    return res ? 1 : 0;
}

char* jni_getSAFFileName() {
    JNIEnv *env = get_env();
    if (env == NULL) return NULL;

    jclass cls = get_nbd_bridge_class(env);
    if (cls == NULL) return NULL;

    jfieldID fid = (*env)->GetStaticFieldID(env, cls, "lastFileName", "Ljava/lang/String;");
    if (fid == NULL) {
        (*env)->DeleteLocalRef(env, cls);
        return NULL;
    }
    jstring jStr = (jstring)(*env)->GetStaticObjectField(env, cls, fid);
    if (jStr == NULL) {
        (*env)->DeleteLocalRef(env, cls);
        return NULL;
    }
    const char *cStr = (*env)->GetStringUTFChars(env, jStr, NULL);
    char *result = strdup(cStr);
    (*env)->ReleaseStringUTFChars(env, jStr, cStr);
    (*env)->DeleteLocalRef(env, jStr);
    (*env)->DeleteLocalRef(env, cls);
    return result;
}

char* jni_getSAFUri() {
    JNIEnv *env = get_env();
    if (env == NULL) return NULL;

    jclass cls = get_nbd_bridge_class(env);
    if (cls == NULL) return NULL;

    jfieldID fid = (*env)->GetStaticFieldID(env, cls, "lastUri", "Ljava/lang/String;");
    if (fid == NULL) {
        (*env)->DeleteLocalRef(env, cls);
        return NULL;
    }
    jstring jStr = (jstring)(*env)->GetStaticObjectField(env, cls, fid);
    if (jStr == NULL) {
        (*env)->DeleteLocalRef(env, cls);
        return NULL;
    }
    const char *cStr = (*env)->GetStringUTFChars(env, jStr, NULL);
    char *result = strdup(cStr);
    (*env)->ReleaseStringUTFChars(env, jStr, cStr);
    (*env)->DeleteLocalRef(env, jStr);
    (*env)->DeleteLocalRef(env, cls);
    return result;
}

int jni_getSAFFd() {
    JNIEnv *env = get_env();
    if (env == NULL) return -1;

    jclass cls = get_nbd_bridge_class(env);
    if (cls == NULL) return -1;

    jfieldID fid = (*env)->GetStaticFieldID(env, cls, "lastFd", "I");
    if (fid == NULL) {
        (*env)->DeleteLocalRef(env, cls);
        return -1;
    }
    jint fd = (*env)->GetStaticIntField(env, cls, fid);
    (*env)->DeleteLocalRef(env, cls);
    return (int)fd;
}

long jni_getSAFSize() {
    JNIEnv *env = get_env();
    if (env == NULL) return 0;

    jclass cls = get_nbd_bridge_class(env);
    if (cls == NULL) return 0;

    jfieldID fid = (*env)->GetStaticFieldID(env, cls, "lastSize", "J");
    if (fid == NULL) {
        (*env)->DeleteLocalRef(env, cls);
        return 0;
    }
    jlong size = (*env)->GetStaticLongField(env, cls, fid);
    (*env)->DeleteLocalRef(env, cls);
    return (long)size;
}

void jni_clearSAFResult() {
    JNIEnv *env = get_env();
    if (env == NULL) return;

    jclass cls = get_nbd_bridge_class(env);
    if (cls == NULL) return;

    jmethodID method = (*env)->GetStaticMethodID(env, cls, "clearSAFResult", "()V");
    if (method != NULL) {
        (*env)->CallStaticVoidMethod(env, cls, method);
    }
    (*env)->DeleteLocalRef(env, cls);
}
