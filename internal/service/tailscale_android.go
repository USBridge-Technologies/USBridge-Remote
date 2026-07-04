//go:build android

package service

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <jni.h>
#include <android/log.h>

#define LOG_TAG "USTailscaleAndroid"
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

static char *jni_getStringMethod(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *method_name) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL) {
		LOGE("JNIEnv is null");
		return NULL;
	}
	if (activity == NULL) {
		LOGE("Activity context is null");
		return NULL;
	}

	jclass activityClass = (*env)->GetObjectClass(env, activity);
	if (activityClass == NULL) {
		if ((*env)->ExceptionCheck(env)) {
			(*env)->ExceptionClear(env);
		}
		LOGE("Failed to resolve activity class");
		return NULL;
	}

	jmethodID method = (*env)->GetMethodID(env, activityClass, method_name, "()Ljava/lang/String;");
	if (method == NULL) {
		if ((*env)->ExceptionCheck(env)) {
			(*env)->ExceptionClear(env);
		}
		(*env)->DeleteLocalRef(env, activityClass);
		LOGE("MainActivity.%s() not found", method_name);
		return NULL;
	}

	jstring result = (jstring)(*env)->CallObjectMethod(env, activity, method);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		result = NULL;
	}
	(*env)->DeleteLocalRef(env, activityClass);
	if (result == NULL) {
		return NULL;
	}
	const char *utf = (*env)->GetStringUTFChars(env, result, NULL);
	if (utf == NULL) {
		(*env)->DeleteLocalRef(env, result);
		return NULL;
	}
	char *dup = strdup(utf);
	(*env)->ReleaseStringUTFChars(env, result, utf);
	(*env)->DeleteLocalRef(env, result);
	return dup;
}

static int jni_openExternalUrl(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *url) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL) return 0;

	jclass activityClass = (*env)->GetObjectClass(env, activity);
	jmethodID method = (*env)->GetMethodID(env, activityClass, "openExternalUrl", "(Ljava/lang/String;)Z");
	if (method == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, activityClass);
		return 0;
	}

	jstring jUrl = (*env)->NewStringUTF(env, url);
	jboolean result = (*env)->CallBooleanMethod(env, activity, method, jUrl);

	if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
	(*env)->DeleteLocalRef(env, jUrl);
	(*env)->DeleteLocalRef(env, activityClass);
	return result == JNI_TRUE ? 1 : 0;
}

static void jni_tsRequestVpnPermission(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
	JNIEnv *env = (JNIEnv *)jni_env_ptr;
	jobject activity = (jobject)ctx_ptr;
	if (env == NULL || activity == NULL) return;

	jclass activityClass = (*env)->GetObjectClass(env, activity);
	jmethodID method = (*env)->GetMethodID(env, activityClass, "requestVpnPermission", "()V");
	if (method == NULL) {
		if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
		(*env)->DeleteLocalRef(env, activityClass);
		return;
	}

	(*env)->CallVoidMethod(env, activity, method);
	if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
	(*env)->DeleteLocalRef(env, activityClass);
}
*/
import "C"

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"unsafe"

	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
	"tailscale.com/net/netmon"
)

var tailscaleAndroidInitOnce sync.Once

func initAndroidTailscaleUserspace() {
	tailscaleAndroidInitOnce.Do(func() {
		netmon.RegisterInterfaceGetter(getAndroidInterfaces)
		refreshAndroidDefaultRouteInterface()
	})
}

func refreshAndroidDefaultRouteInterface() {
	ifName, err := getAndroidString("getActiveNetworkInterfaceName")
	if err != nil {
		logrus.WithError(err).Warn("tailscale android: failed to get active network interface")
		return
	}
	ifName = strings.TrimSpace(ifName)
	logrus.Infof("tailscale android: active network interface=%q", ifName)
	netmon.UpdateLastKnownDefaultRouteInterface(ifName)
}

func getAndroidInterfaces() ([]netmon.Interface, error) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🔥 PANIC in getAndroidInterfaces: %v", r)
		}
	}()
	raw, err := getAndroidString("getInterfacesAsString")
	if err != nil {
		return nil, err
	}
	logrus.Infof("📡 [Tailscale/JNI] Raw interfaces from Android: %q", raw)
	if raw == "" {
		logrus.Warn("⚠️ [Tailscale/JNI] Received EMPTY interface list from Android! Local discovery will be broken.")
	}
	return parseAndroidInterfaces(raw), nil
}

func getAndroidNativeLibraryDir() (string, error) {
	return getAndroidString("getNativeLibraryDir")
}

func getAndroidFilesDir() (string, error) {
	return getAndroidString("getFilesDirAbsolutePath")
}

func getAndroidCacheDir() (string, error) {
	return getAndroidString("getCacheDirAbsolutePath")
}

func openAndroidExternalUrl(url string) (bool, error) {
	var opened bool
	err := driver.RunNative(func(context any) error {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("🔥 PANIC in openAndroidExternalUrl callback: %v", r)
			}
		}()
		androidCtx, ok := context.(*driver.AndroidContext)
		if !ok || androidCtx == nil {
			return fmt.Errorf("android context unavailable")
		}
		cUrl := C.CString(url)
		defer C.free(unsafe.Pointer(cUrl))

		res := C.jni_openExternalUrl(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), cUrl)
		opened = (res != 0)
		return nil
	})
	return opened, err
}

func requestAndroidVpnPermission() error {
	return driver.RunNative(func(context any) error {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("🔥 PANIC in requestAndroidVpnPermission callback: %v", r)
			}
		}()
		androidCtx, ok := context.(*driver.AndroidContext)
		if !ok || androidCtx == nil {
			return fmt.Errorf("android context unavailable")
		}
		C.jni_tsRequestVpnPermission(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx))
		return nil
	})
}

func getAndroidString(methodName string) (string, error) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🔥 PANIC in getAndroidString(%s): %v", methodName, r)
		}
	}()
	var out string
	logrus.Debugf("tailscale android: calling JNI method %s", methodName)
	err := driver.RunNative(func(context any) error {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("🔥 PANIC in getAndroidString(%s) callback: %v", methodName, r)
			}
		}()
		androidCtx, ok := context.(*driver.AndroidContext)
		if !ok || androidCtx == nil {
			return fmt.Errorf("android context unavailable")
		}
		cMethod := C.CString(methodName)
		defer C.free(unsafe.Pointer(cMethod))
		cstr := C.jni_getStringMethod(C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx), cMethod)
		if cstr == nil {
			logrus.Warnf("tailscale android: JNI method %s returned NULL", methodName)
			return nil
		}
		defer C.free(unsafe.Pointer(cstr))
		out = C.GoString(cstr)
		return nil
	})
	if err != nil {
		logrus.WithError(err).Errorf("tailscale android: JNI method %s failed", methodName)
		return "", err
	}
	return out, nil
}

func parseAndroidInterfaces(raw string) []netmon.Interface {
	var ifaces []netmon.Interface
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 2 {
			continue
		}

		var (
			name         string
			index, mtu   int
			up           bool
			broadcast    bool
			loopback     bool
			pointToPoint bool
			multicast    bool
		)
		if _, err := fmt.Sscanf(parts[0], "%s %d %d %t %t %t %t %t", &name, &index, &mtu, &up, &broadcast, &loopback, &pointToPoint, &multicast); err != nil {
			continue
		}

		iface := netmon.Interface{
			Interface: &net.Interface{
				Name:  name,
				Index: index,
				MTU:   mtu,
			},
			AltAddrs: []net.Addr{},
		}
		if up {
			iface.Flags |= net.FlagUp
		}
		if broadcast {
			iface.Flags |= net.FlagBroadcast
		}
		if loopback {
			iface.Flags |= net.FlagLoopback
		}
		if pointToPoint {
			iface.Flags |= net.FlagPointToPoint
		}
		if multicast {
			iface.Flags |= net.FlagMulticast
		}

		for _, addrText := range strings.Fields(parts[1]) {
			addrText = strings.TrimSpace(addrText)
			if addrText == "" {
				continue
			}
			addrText = strings.ReplaceAll(addrText, "%"+name, "")
			if _, prefix, err := net.ParseCIDR(addrText); err == nil {
				iface.AltAddrs = append(iface.AltAddrs, prefix)
				continue
			}
			if addr, err := netip.ParseAddr(addrText); err == nil {
				bits := 32
				if addr.Is6() {
					bits = 128
				}
				pfx := netip.PrefixFrom(addr, bits)
				iface.AltAddrs = append(iface.AltAddrs, prefixToIPNet(pfx))
			}
		}
		ifaces = append(ifaces, iface)
	}
	return ifaces
}

func prefixToIPNet(pfx netip.Prefix) *net.IPNet {
	addr := pfx.Addr()
	if addr.Is4() {
		ip := addr.As4()
		return &net.IPNet{IP: net.IP(ip[:]), Mask: net.CIDRMask(pfx.Bits(), 32)}
	}
	ip := addr.As16()
	return &net.IPNet{IP: net.IP(ip[:]), Mask: net.CIDRMask(pfx.Bits(), 128)}
}
