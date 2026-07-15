#include <jni.h>
#include <android/log.h>
#include <stdlib.h>

#define LOG_TAG "GStreamerJNI"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)

static JavaVM *g_gst_java_vm = NULL;
static jobject g_gst_class_loader = NULL;

void gst_android_set_java_vm_and_context(JavaVM *vm, JNIEnv *env, jobject context) {
    if (!vm || !env || !context) return;
    g_gst_java_vm = vm;
    
    if (g_gst_class_loader) {
        (*env)->DeleteGlobalRef(env, g_gst_class_loader);
        g_gst_class_loader = NULL;
    }
    
    jclass ctx_class = (*env)->GetObjectClass(env, context);
    if (!ctx_class) return;
    
    jmethodID get_class = (*env)->GetMethodID(env, ctx_class, "getClass", "()Ljava/lang/Class;");
    if (!get_class) {
        (*env)->DeleteLocalRef(env, ctx_class);
        return;
    }
    
    jobject cls = (*env)->CallObjectMethod(env, context, get_class);
    (*env)->DeleteLocalRef(env, ctx_class);
    if (!cls) return;
    
    jclass class_class = (*env)->GetObjectClass(env, cls);
    if (!class_class) {
        (*env)->DeleteLocalRef(env, cls);
        return;
    }
    
    jmethodID get_class_loader = (*env)->GetMethodID(env, class_class, "getClassLoader", "()Ljava/lang/ClassLoader;");
    if (!get_class_loader) {
        (*env)->DeleteLocalRef(env, class_class);
        (*env)->DeleteLocalRef(env, cls);
        return;
    }
    
    jobject loader = (*env)->CallObjectMethod(env, cls, get_class_loader);
    (*env)->DeleteLocalRef(env, class_class);
    (*env)->DeleteLocalRef(env, cls);
    
    if (loader) {
        g_gst_class_loader = (*env)->NewGlobalRef(env, loader);
        (*env)->DeleteLocalRef(env, loader);
    }
    
    LOGI("gst_android: VM=%p, class_loader=%p", (void*)vm, (void*)g_gst_class_loader);
}

// ЭТИ ФУНКЦИИ ДОЛЖНЫ БЫТЬ ВИДИМЫ ДЛЯ dlsym(RTLD_DEFAULT)
// В Android gomobile bind/build они будут экспортированы из libgojni.so

JavaVM* gst_android_get_java_vm(void) {
    return g_gst_java_vm;
}

jobject gst_android_get_application_class_loader(void) {
    return g_gst_class_loader;
}
