//go:build android

#include <android/log.h>
#include <jni.h>
#include <stdint.h>
#include <stdlib.h>

#define LOG_FATAL(...) __android_log_print(ANDROID_LOG_FATAL, "Fyne", __VA_ARGS__)
#define LOG_WARN(...)  __android_log_print(ANDROID_LOG_WARN,  "Fyne", __VA_ARGS__)

// Cached JNI references – initialised once from the first JNI call.
static jclass    activity_class        = NULL;
static jmethodID clear_nodes_method    = 0;
static jmethodID add_node_method       = 0;
static jmethodID commit_nodes_method   = 0;
static jmethodID setup_access_method   = 0;

// Populate the method-ID cache.  ctx is the GoNativeActivity jobject so
// GetObjectClass gives us the correct application class-loader class.
static void ensureMethodsCached(JNIEnv *env, jobject ctx) {
	if (activity_class != NULL) {
		return;
	}

	jclass clazz = (*env)->GetObjectClass(env, ctx);
	if (clazz == NULL) {
		LOG_FATAL("accessibility: cannot get activity class");
		return;
	}
	activity_class = (jclass)(*env)->NewGlobalRef(env, clazz);

	clear_nodes_method = (*env)->GetStaticMethodID(env, activity_class,
		"clearAccessibilityNodes", "()V");
	if (clear_nodes_method == 0) {
		LOG_WARN("accessibility: clearAccessibilityNodes not found");
		(*env)->ExceptionClear(env);
	}

	add_node_method = (*env)->GetStaticMethodID(env, activity_class,
		"addAccessibilityNode", "(IILjava/lang/String;IIIII)V");
	if (add_node_method == 0) {
		LOG_WARN("accessibility: addAccessibilityNode not found");
		(*env)->ExceptionClear(env);
	}

	commit_nodes_method = (*env)->GetStaticMethodID(env, activity_class,
		"commitAccessibilityNodes", "()V");
	if (commit_nodes_method == 0) {
		LOG_WARN("accessibility: commitAccessibilityNodes not found");
		(*env)->ExceptionClear(env);
	}

	setup_access_method = (*env)->GetStaticMethodID(env, activity_class,
		"setupAccessibility", "()V");
	if (setup_access_method == 0) {
		LOG_WARN("accessibility: setupAccessibility not found");
		(*env)->ExceptionClear(env);
	}
}

void clearAccessibilityNodes(uintptr_t jni_env, uintptr_t ctx) {
	JNIEnv *env = (JNIEnv *)jni_env;
	ensureMethodsCached(env, (jobject)ctx);
	if (clear_nodes_method == 0) {
		return;
	}
	(*env)->CallStaticVoidMethod(env, activity_class, clear_nodes_method);
	if ((*env)->ExceptionOccurred(env)) {
		(*env)->ExceptionClear(env);
	}
}

void addAccessibilityNode(uintptr_t jni_env, uintptr_t ctx,
	int id, int role, const char *label,
	int x, int y, int width, int height, int parent_id) {
	JNIEnv *env = (JNIEnv *)jni_env;
	ensureMethodsCached(env, (jobject)ctx);
	if (add_node_method == 0) {
		return;
	}
	jstring jlabel = (*env)->NewStringUTF(env, label ? label : "");
	(*env)->CallStaticVoidMethod(env, activity_class, add_node_method,
		(jint)id, (jint)role, jlabel,
		(jint)x, (jint)y, (jint)width, (jint)height, (jint)parent_id);
	(*env)->DeleteLocalRef(env, jlabel);
	if ((*env)->ExceptionOccurred(env)) {
		(*env)->ExceptionClear(env);
	}
}

void commitAccessibilityNodes(uintptr_t jni_env, uintptr_t ctx) {
	JNIEnv *env = (JNIEnv *)jni_env;
	ensureMethodsCached(env, (jobject)ctx);
	if (commit_nodes_method == 0) {
		return;
	}
	(*env)->CallStaticVoidMethod(env, activity_class, commit_nodes_method);
	if ((*env)->ExceptionOccurred(env)) {
		(*env)->ExceptionClear(env);
	}
}

void setupAccessibility(uintptr_t jni_env, uintptr_t ctx) {
	JNIEnv *env = (JNIEnv *)jni_env;
	ensureMethodsCached(env, (jobject)ctx);
	if (setup_access_method == 0) {
		return;
	}
	(*env)->CallStaticVoidMethod(env, activity_class, setup_access_method);
	if ((*env)->ExceptionOccurred(env)) {
		(*env)->ExceptionClear(env);
	}
}
