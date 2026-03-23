//go:build android

package service

/*
#cgo LDFLAGS: -landroid -llog

#include <jni.h>
#include <android/log.h>
#include <stdlib.h>

#define LOG_TAG "USBridgeVPN"
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

static int jni_requestVpnPermission(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (activity == NULL) {
		LOGE("Activity context is null");
		return 0;
	}
	jclass activityClass = (*env)->GetObjectClass(env, activity);
	if (activityClass == NULL) {
		LOGE("Failed to resolve activity class");
		return 0;
	}
	jmethodID requestMethod = (*env)->GetMethodID(env, activityClass, "requestVpnPermission", "()V");
	if (requestMethod == NULL) {
		LOGE("MainActivity.requestVpnPermission() not found");
		(*env)->DeleteLocalRef(env, activityClass);
		return 0;
	}
	(*env)->CallVoidMethod(env, activity, requestMethod);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, activityClass);
		return 0;
	}
	(*env)->DeleteLocalRef(env, activityClass);
	return 1;
}

static int jni_getVpnPermissionState(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (activity == NULL) {
		return -2;
	}
	jclass activityClass = (*env)->GetObjectClass(env, activity);
	if (activityClass == NULL) {
		return -2;
	}
	jmethodID stateMethod = (*env)->GetMethodID(env, activityClass, "getVpnPermissionState", "()I");
	if (stateMethod == NULL) {
		(*env)->DeleteLocalRef(env, activityClass);
		return -2;
	}
	jint state = (*env)->CallIntMethod(env, activity, stateMethod);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, activityClass);
		return -2;
	}
	(*env)->DeleteLocalRef(env, activityClass);
	return state;
}

static jclass findVpnServiceClassWithContext(JNIEnv *env, jobject context) {
	if (context == NULL) {
		LOGE("Context is null while resolving UsbridgeVpnService");
		return NULL;
	}

	jclass contextClass = (*env)->GetObjectClass(env, context);
	if (contextClass == NULL) {
		if ((*env)->ExceptionCheck(env)) {
			(*env)->ExceptionClear(env);
		}
		LOGE("Failed to resolve Context class");
		return NULL;
	}

	jmethodID getClassLoader = (*env)->GetMethodID(env, contextClass, "getClassLoader", "()Ljava/lang/ClassLoader;");
	if (getClassLoader == NULL) {
		if ((*env)->ExceptionCheck(env)) {
			(*env)->ExceptionClear(env);
		}
		(*env)->DeleteLocalRef(env, contextClass);
		LOGE("Context.getClassLoader() not found");
		return NULL;
	}

	jobject classLoader = (*env)->CallObjectMethod(env, context, getClassLoader);
	if ((*env)->ExceptionCheck(env) || classLoader == NULL) {
		(*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, contextClass);
		LOGE("Failed to get application ClassLoader");
		return NULL;
	}

	jclass classLoaderClass = (*env)->FindClass(env, "java/lang/ClassLoader");
	if (classLoaderClass == NULL) {
		if ((*env)->ExceptionCheck(env)) {
			(*env)->ExceptionClear(env);
		}
		(*env)->DeleteLocalRef(env, classLoader);
		(*env)->DeleteLocalRef(env, contextClass);
		LOGE("java/lang/ClassLoader not found");
		return NULL;
	}

	jmethodID loadClass = (*env)->GetMethodID(env, classLoaderClass, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;");
	if (loadClass == NULL) {
		if ((*env)->ExceptionCheck(env)) {
			(*env)->ExceptionClear(env);
		}
		(*env)->DeleteLocalRef(env, classLoaderClass);
		(*env)->DeleteLocalRef(env, classLoader);
		(*env)->DeleteLocalRef(env, contextClass);
		LOGE("ClassLoader.loadClass() not found");
		return NULL;
	}

	jstring className = (*env)->NewStringUTF(env, "com.usbridge.client.UsbridgeVpnService");
	jobject loadedClass = (*env)->CallObjectMethod(env, classLoader, loadClass, className);
	(*env)->DeleteLocalRef(env, className);
	(*env)->DeleteLocalRef(env, classLoaderClass);
	(*env)->DeleteLocalRef(env, classLoader);
	(*env)->DeleteLocalRef(env, contextClass);

	if ((*env)->ExceptionCheck(env) || loadedClass == NULL) {
		(*env)->ExceptionClear(env);
		LOGE("ClassLoader failed to load com.usbridge.client.UsbridgeVpnService");
		return NULL;
	}

	return (jclass)loadedClass;
}

static int jni_startVpnService(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject context = (jobject)ctx_ptr;
	jclass cls = findVpnServiceClassWithContext(env, context);
	if (cls == NULL) {
		return 0;
	}
	jmethodID method = (*env)->GetStaticMethodID(env, cls, "startVpnService", "(Landroid/content/Context;)V");
	if (method == NULL) {
		(*env)->DeleteLocalRef(env, cls);
		return 0;
	}
	(*env)->CallStaticVoidMethod(env, cls, method, context);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		return 0;
	}
	(*env)->DeleteLocalRef(env, cls);
	return 1;
}

static int jni_isVpnServiceReady(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject context = (jobject)ctx_ptr;
	jclass cls = findVpnServiceClassWithContext(env, context);
	if (cls == NULL) {
		return 0;
	}
	jmethodID method = (*env)->GetStaticMethodID(env, cls, "isServiceReady", "()Z");
	if (method == NULL) {
		(*env)->DeleteLocalRef(env, cls);
		return 0;
	}
	jboolean ready = (*env)->CallStaticBooleanMethod(env, cls, method);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		return 0;
	}
	(*env)->DeleteLocalRef(env, cls);
	return ready ? 1 : 0;
}

static int jni_establishVpnTun(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *session_name, const char *client_cidr, const char *routes, jint mtu) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject context = (jobject)ctx_ptr;
	jclass cls = findVpnServiceClassWithContext(env, context);
	if (cls == NULL) {
		return -1;
	}
	jmethodID method = (*env)->GetStaticMethodID(env, cls, "establishTunnel", "(Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;I)I");
	if (method == NULL) {
		(*env)->DeleteLocalRef(env, cls);
		return -1;
	}
	jstring jSession = (*env)->NewStringUTF(env, session_name);
	jstring jClientCIDR = (*env)->NewStringUTF(env, client_cidr);
	jstring jRoutes = (*env)->NewStringUTF(env, routes);
	jint fd = (*env)->CallStaticIntMethod(env, cls, method, jSession, jClientCIDR, jRoutes, mtu);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		fd = -1;
	}
	(*env)->DeleteLocalRef(env, jRoutes);
	(*env)->DeleteLocalRef(env, jClientCIDR);
	(*env)->DeleteLocalRef(env, jSession);
	(*env)->DeleteLocalRef(env, cls);
	return fd;
}

static int jni_protectVpnSocket(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, jint fd) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject context = (jobject)ctx_ptr;
	jclass cls = findVpnServiceClassWithContext(env, context);
	if (cls == NULL) {
		return 0;
	}
	jmethodID method = (*env)->GetStaticMethodID(env, cls, "protectSocket", "(I)Z");
	if (method == NULL) {
		(*env)->DeleteLocalRef(env, cls);
		return 0;
	}
	jboolean ok = (*env)->CallStaticBooleanMethod(env, cls, method, fd);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		return 0;
	}
	(*env)->DeleteLocalRef(env, cls);
	return ok ? 1 : 0;
}

static int jni_stopVpnService(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject context = (jobject)ctx_ptr;
	jclass cls = findVpnServiceClassWithContext(env, context);
	if (cls == NULL) {
		return 0;
	}
	jmethodID method = (*env)->GetStaticMethodID(env, cls, "stopVpnService", "()V");
	if (method == NULL) {
		(*env)->DeleteLocalRef(env, cls);
		return 0;
	}
	(*env)->CallStaticVoidMethod(env, cls, method);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, cls);
		return 0;
	}
	(*env)->DeleteLocalRef(env, cls);
	return 1;
}
*/
import "C"

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unsafe"

	"fyne.io/fyne/v2/driver"
	"golang.zx2c4.com/wireguard/conn"
	wgtun "golang.zx2c4.com/wireguard/tun"

	"usbridge-client/internal/models"
)

const (
	androidVPNPollInterval      = 100 * time.Millisecond
	androidVPNPermissionTimeout = 60 * time.Second
	androidVPNServiceTimeout    = 15 * time.Second
)

func (s *userspaceWireGuardService) createTUNDevice(resp *models.WireGuardBootstrapResponse, mtu int) (wgtun.Device, conn.Bind, string, error) {
	if err := requestAndroidVPNPermission(); err != nil {
		return nil, nil, "", err
	}
	if err := startAndroidVPNService(); err != nil {
		return nil, nil, "", err
	}

	fd, err := establishAndroidVPNTunnel(firstNonEmpty(resp.InterfaceName, s.ifaceName, "USBridge"), resp.ClientAddressCIDR, normalizeAllowedRoutes(resp), mtu)
	if err != nil {
		return nil, nil, "", err
	}

	tunDevice, ifaceName, err := wgtun.CreateUnmonitoredTUNFromFD(fd)
	if err != nil {
		_ = closeAndroidVPNFD(fd)
		return nil, nil, "", fmt.Errorf("failed to create WireGuard TUN interface from Android VpnService fd: %w", err)
	}
	return tunDevice, conn.NewDefaultBind(), ifaceName, nil
}

func (s *userspaceWireGuardService) afterDeviceUp(bind conn.Bind, _ *models.WireGuardBootstrapResponse, _ int, _ []string) error {
	fdProvider, ok := bind.(conn.PeekLookAtSocketFd)
	if !ok {
		return fmt.Errorf("wireguard bind does not expose socket fds on Android")
	}
	for _, peek := range []func() (int, error){fdProvider.PeekLookAtSocketFd4, fdProvider.PeekLookAtSocketFd6} {
		fd, err := peek()
		if err != nil || fd <= 0 {
			continue
		}
		if err := protectAndroidVPNSocket(fd); err != nil {
			return err
		}
	}
	return nil
}

func requestAndroidVPNPermission() error {
	if err := runAndroidNative(func(androidCtx *driver.AndroidContext) error {
		if result := C.jni_requestVpnPermission(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx)); result == 0 {
			return fmt.Errorf("MainActivity.requestVpnPermission() is unavailable")
		}
		return nil
	}); err != nil {
		return err
	}

	deadline := time.Now().Add(androidVPNPermissionTimeout)
	for time.Now().Before(deadline) {
		state := 0
		err := runAndroidNative(func(androidCtx *driver.AndroidContext) error {
			state = int(C.jni_getVpnPermissionState(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx)))
			return nil
		})
		if err != nil {
			return err
		}
		switch state {
		case 1:
			return nil
		case -1:
			return fmt.Errorf("WireGuard VPN permission was denied by Android")
		case -2:
			return fmt.Errorf("failed to query Android VPN permission state")
		}
		time.Sleep(androidVPNPollInterval)
	}
	return fmt.Errorf("timed out waiting for Android VPN permission")
}

func startAndroidVPNService() error {
	if err := runAndroidNative(func(androidCtx *driver.AndroidContext) error {
		if result := C.jni_startVpnService(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx)); result == 0 {
			return fmt.Errorf("failed to request Android VPN service startup")
		}
		return nil
	}); err != nil {
		return err
	}

	deadline := time.Now().Add(androidVPNServiceTimeout)
	for time.Now().Before(deadline) {
		ready := false
		err := runAndroidNative(func(androidCtx *driver.AndroidContext) error {
			ready = C.jni_isVpnServiceReady(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx)) == 1
			return nil
		})
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		time.Sleep(androidVPNPollInterval)
	}
	return fmt.Errorf("timed out waiting for Android VPN service startup")
}

func establishAndroidVPNTunnel(sessionName, clientCIDR string, routes []string, mtu int) (int, error) {
	var fd int
	routesString := strings.Join(routes, "\n")
	err := runAndroidNative(func(androidCtx *driver.AndroidContext) error {
		cSession := C.CString(sessionName)
		cClientCIDR := C.CString(clientCIDR)
		cRoutes := C.CString(routesString)
		defer C.free(unsafe.Pointer(cSession))
		defer C.free(unsafe.Pointer(cClientCIDR))
		defer C.free(unsafe.Pointer(cRoutes))

		fd = int(C.jni_establishVpnTun(
			C.uintptr_t(androidCtx.Env),
			C.uintptr_t(androidCtx.Ctx),
			cSession,
			cClientCIDR,
			cRoutes,
			C.int(mtu),
		))
		return nil
	})
	if err != nil {
		return 0, err
	}
	if fd < 0 {
		return 0, fmt.Errorf("Android VpnService.Builder.establish() failed")
	}
	return fd, nil
}

func protectAndroidVPNSocket(fd int) error {
	return runAndroidNative(func(androidCtx *driver.AndroidContext) error {
		if result := C.jni_protectVpnSocket(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), C.int(fd)); result == 0 {
			return fmt.Errorf("failed to protect WireGuard UDP socket fd=%d from Android VPN routing", fd)
		}
		return nil
	})
}

func stopAndroidVPNService() error {
	return runAndroidNative(func(androidCtx *driver.AndroidContext) error {
		C.jni_stopVpnService(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx))
		return nil
	})
}

func runAndroidNative(fn func(*driver.AndroidContext) error) error {
	return driver.RunNative(func(ctx any) error {
		androidCtx, ok := ctx.(*driver.AndroidContext)
		if !ok {
			return fmt.Errorf("failed to get Android native context")
		}
		return fn(androidCtx)
	})
}

func closeAndroidVPNFD(fd int) error {
	file := os.NewFile(uintptr(fd), "android-vpn-fd")
	if file == nil {
		return nil
	}
	return file.Close()
}
