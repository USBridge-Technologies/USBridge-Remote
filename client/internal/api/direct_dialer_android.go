//go:build android

package api

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
#include <stdint.h>
#include <jni.h>
#include <android/log.h>

#define LOG_TAG "USDirectDialerAndroid"
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

// Calls MainActivity.bindSocketToBestNetwork(int fd, String destHost) -> boolean.
// See the Kotlin implementation for why this is needed: a plain BSD source-IP
// bind (net.Dialer.LocalAddr) isn't enough on Android, since ConnectivityManager
// routes each app's sockets through a per-UID default network regardless of the
// bound address once more than one network (e.g. Wi-Fi + mobile data) is up.
static int jni_bindSocketToBestNetwork(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, int fd, const char *dest_host) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL) {
		LOGE("JNIEnv or activity is null");
		return 0;
	}

	jclass activityClass = (*env)->GetObjectClass(env, activity);
	if (activityClass == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		LOGE("Failed to resolve activity class");
		return 0;
	}

	jmethodID method = (*env)->GetMethodID(env, activityClass, "bindSocketToBestNetwork", "(ILjava/lang/String;)Z");
	if (method == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, activityClass);
		LOGE("MainActivity.bindSocketToBestNetwork() not found");
		return 0;
	}

	jstring jDestHost = (*env)->NewStringUTF(env, dest_host);
	jboolean result = (*env)->CallBooleanMethod(env, activity, method, (jint)fd, jDestHost);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		result = JNI_FALSE;
	}
	(*env)->DeleteLocalRef(env, jDestHost);
	(*env)->DeleteLocalRef(env, activityClass);
	return result == JNI_TRUE ? 1 : 0;
}
*/
import "C"

import (
	"net"
	"syscall"
	"unsafe"

	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

// buildDirectDialer returns a net.Dialer whose sockets are bound, via
// Android's ConnectivityManager.Network.bindSocket, to whichever network can
// actually route to destHost. See direct_dialer_darwin.go for the same
// problem on macOS (IP_BOUND_IF) and why loopback destinations are exempt.
func buildDirectDialer(destHost string) *net.Dialer {
	if isLoopbackHost(destHost) {
		return &net.Dialer{}
	}

	host := destHost
	if h, _, err := net.SplitHostPort(destHost); err == nil {
		host = h
	}

	return &net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				if !bindSocketToBestNetwork(int(fd), host) {
					logrus.Debugf("⚠️ [Direct] bindSocketToBestNetwork failed for %s (falling back to default routing)", host)
				}
			})
		},
	}
}

func bindSocketToBestNetwork(fd int, destHost string) bool {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🔥 PANIC in bindSocketToBestNetwork: %v", r)
		}
	}()
	var bound bool
	err := driver.RunNative(func(context any) error {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("🔥 PANIC in bindSocketToBestNetwork callback: %v", r)
			}
		}()
		androidCtx, ok := context.(*driver.AndroidContext)
		if !ok || androidCtx == nil {
			return nil
		}
		cHost := C.CString(destHost)
		defer C.free(unsafe.Pointer(cHost))
		bound = C.jni_bindSocketToBestNetwork(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), C.int(fd), cHost) != 0
		return nil
	})
	if err != nil {
		logrus.WithError(err).Debug("⚠️ [Direct] bindSocketToBestNetwork: android context unavailable")
		return false
	}
	return bound
}
