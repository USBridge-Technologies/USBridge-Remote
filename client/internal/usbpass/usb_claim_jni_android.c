#include <jni.h>
#include <android/log.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

#define LOG_TAG "USB_CLAIM_JNI"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)
#define LOGW(...) __android_log_print(ANDROID_LOG_WARN, LOG_TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

// Same env/ctx-per-call convention as usb_jni_android.c and
// saf_jni_android.c: Go's driver.RunNative attaches the calling goroutine's
// OS thread to the JVM fresh on every call (see golang.org/x/mobile's
// mobileinit.RunOnJVM), so there is no long-lived JNIEnv* to cache -- only
// the jlong handles below (JNI global refs, valid across threads/envs for
// the life of the process) persist between calls.

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
        LOGE("❌ [JNI-USB-CLAIM-EXCEPTION] %s: %s", prefix, cMessage);
        (*env)->ReleaseStringUTFChars(env, message, cMessage);
        (*env)->DeleteLocalRef(env, message);
    } else {
        LOGE("❌ [JNI-USB-CLAIM-EXCEPTION] %s: (no message)", prefix);
    }
    (*env)->DeleteLocalRef(env, exceptionClass);
    (*env)->DeleteLocalRef(env, exception);
}

// getUsbManager and findDeviceByName are shared setup used by every function
// below -- UsbManager.getDeviceList() is keyed by exactly the deviceName
// string list_usb_android.go handed back as InstanceID, so this is an exact
// lookup, not a VID:PID scan (unlike gousb's Linux/Windows path, which has
// to disambiguate multiple identical-VID:PID sticks by bus/addr instead).
static jobject getUsbManager(JNIEnv *env, jobject ctx) {
    jclass contextClass = (*env)->GetObjectClass(env, ctx);
    jmethodID getSystemServiceMethod = (*env)->GetMethodID(env, contextClass, "getSystemService", "(Ljava/lang/String;)Ljava/lang/Object;");
    jstring usbService = (*env)->NewStringUTF(env, "usb");
    jobject usbManager = (*env)->CallObjectMethod(env, ctx, getSystemServiceMethod, usbService);
    (*env)->DeleteLocalRef(env, usbService);
    (*env)->DeleteLocalRef(env, contextClass);
    return usbManager;
}

static jobject findDeviceByName(JNIEnv *env, jobject usbManager, const char *deviceName) {
    jclass usbManagerClass = (*env)->GetObjectClass(env, usbManager);
    jmethodID getDeviceListMethod = (*env)->GetMethodID(env, usbManagerClass, "getDeviceList", "()Ljava/util/HashMap;");
    jobject deviceMap = (*env)->CallObjectMethod(env, usbManager, getDeviceListMethod);
    if (deviceMap == NULL) {
        return NULL;
    }
    jclass mapClass = (*env)->GetObjectClass(env, deviceMap);
    jmethodID getMethod = (*env)->GetMethodID(env, mapClass, "get", "(Ljava/lang/Object;)Ljava/lang/Object;");
    jstring jName = (*env)->NewStringUTF(env, deviceName);
    jobject device = (*env)->CallObjectMethod(env, deviceMap, getMethod, jName);
    (*env)->DeleteLocalRef(env, jName);
    (*env)->DeleteLocalRef(env, deviceMap);
    return device; // local ref, or NULL if not found
}

// jni_usbHasPermission reports whether this app already holds
// UsbManager.hasPermission() for deviceName -- checked before every claim
// attempt so a device the user already approved doesn't re-prompt.
int jni_usbHasPermission(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *deviceName) {
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject ctx = (jobject)ctx_ptr;

    jobject usbManager = getUsbManager(env, ctx);
    if (usbManager == NULL) {
        return 0;
    }
    jobject device = findDeviceByName(env, usbManager, deviceName);
    if (device == NULL) {
        (*env)->DeleteLocalRef(env, usbManager);
        return 0; // unplugged since list time
    }
    jclass usbManagerClass = (*env)->GetObjectClass(env, usbManager);
    jmethodID hasPermissionMethod = (*env)->GetMethodID(env, usbManagerClass, "hasPermission", "(Landroid/hardware/usb/UsbDevice;)Z");
    jboolean granted = (*env)->CallBooleanMethod(env, usbManager, hasPermissionMethod, device);

    (*env)->DeleteLocalRef(env, device);
    (*env)->DeleteLocalRef(env, usbManager);
    return granted ? 1 : 0;
}

// jni_usbRequestPermission fires the system "Allow app to access USB
// device?" dialog (a no-op if already granted or already pending). The
// PendingIntent's target action is never actually handled by a registered
// receiver -- Go polls jni_usbHasPermission instead of waiting on the
// broadcast, which avoids needing any Kotlin/manifest changes for this
// flow. FLAG_IMMUTABLE (0x04000000, added API 23) is required on API 31+;
// harmless as an extra flag bit on any API level this app actually ships
// to.
void jni_usbRequestPermission(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *deviceName) {
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject ctx = (jobject)ctx_ptr;

    jobject usbManager = getUsbManager(env, ctx);
    if (usbManager == NULL) {
        return;
    }
    jobject device = findDeviceByName(env, usbManager, deviceName);
    if (device == NULL) {
        (*env)->DeleteLocalRef(env, usbManager);
        return;
    }

    jclass intentClass = (*env)->FindClass(env, "android/content/Intent");
    jmethodID intentCtor = (*env)->GetMethodID(env, intentClass, "<init>", "(Ljava/lang/String;)V");
    jstring action = (*env)->NewStringUTF(env, "io.usbridge.client.USB_PERMISSION");
    jobject intent = (*env)->NewObject(env, intentClass, intentCtor, action);

    jclass pendingIntentClass = (*env)->FindClass(env, "android/app/PendingIntent");
    jmethodID getBroadcastMethod = (*env)->GetStaticMethodID(env, pendingIntentClass, "getBroadcast",
        "(Landroid/content/Context;ILandroid/content/Intent;I)Landroid/app/PendingIntent;");
    jobject pendingIntent = (*env)->CallStaticObjectMethod(env, pendingIntentClass, getBroadcastMethod,
        ctx, 0, intent, 0x04000000 /* FLAG_IMMUTABLE */);

    jclass usbManagerClass = (*env)->GetObjectClass(env, usbManager);
    jmethodID requestPermissionMethod = (*env)->GetMethodID(env, usbManagerClass, "requestPermission",
        "(Landroid/hardware/usb/UsbDevice;Landroid/app/PendingIntent;)V");
    (*env)->CallVoidMethod(env, usbManager, requestPermissionMethod, device, pendingIntent);

    if ((*env)->ExceptionCheck(env)) {
        logExceptionDetails(env, "requestPermission");
    } else {
        LOGI("📱 [JNI-USB-CLAIM] permission dialog requested for %s", deviceName);
    }

    (*env)->DeleteLocalRef(env, pendingIntent);
    (*env)->DeleteLocalRef(env, intent);
    (*env)->DeleteLocalRef(env, action);
    (*env)->DeleteLocalRef(env, device);
    (*env)->DeleteLocalRef(env, usbManager);
}

// bytesToHex renders src as lowercase hex into a malloc'd NUL-terminated
// buffer (2*len+1 bytes) -- used to smuggle the raw descriptor bytes
// through the same tab-separated char* return convention as
// jni_listUsbDevices without worrying about embedded NULs/tabs/newlines.
static char *bytesToHex(const uint8_t *src, int len) {
    char *out = (char *)malloc((size_t)len * 2 + 1);
    if (out == NULL) {
        return NULL;
    }
    static const char hexDigits[] = "0123456789abcdef";
    for (int i = 0; i < len; i++) {
        out[i * 2] = hexDigits[(src[i] >> 4) & 0xF];
        out[i * 2 + 1] = hexDigits[src[i] & 0xF];
    }
    out[len * 2] = '\0';
    return out;
}

// jni_usbClaim opens deviceName (permission must already be granted -- see
// jni_usbHasPermission/jni_usbRequestPermission), force-claims its first
// interface (evicting any kernel driver already bound to it -- the
// capability macOS's libusb doesn't have, see hidbridge_darwin.go's doc
// comment), and returns:
//   connHandle \t ifaceHandle \t ifaceNum \t rawDescriptorsHex
// connHandle/ifaceHandle are JNI global refs (UsbDeviceConnection /
// UsbInterface) packed as decimal jlong -- Go carries them opaquely and
// passes them back to jni_usbControlTransfer/jni_usbBulkTransfer/
// jni_usbClose. rawDescriptorsHex is UsbDeviceConnection.getRawDescriptors()
// -- the actual bytes the kernel enumerated this device with (device
// descriptor followed by the active configuration descriptor and
// everything under it), not a synthesized stand-in the way mac's HID path
// has to build one. Returns NULL on any failure (permission, open, or
// claim); check logcat tag USB_CLAIM_JNI for which.
char* jni_usbClaim(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *deviceName) {
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject ctx = (jobject)ctx_ptr;

    jobject usbManager = getUsbManager(env, ctx);
    if (usbManager == NULL) {
        LOGE("❌ [JNI-USB-CLAIM] no UsbManager");
        return NULL;
    }
    jobject device = findDeviceByName(env, usbManager, deviceName);
    if (device == NULL) {
        LOGE("❌ [JNI-USB-CLAIM] device %s not found (unplugged?)", deviceName);
        (*env)->DeleteLocalRef(env, usbManager);
        return NULL;
    }

    jclass usbManagerClass = (*env)->GetObjectClass(env, usbManager);
    jmethodID openDeviceMethod = (*env)->GetMethodID(env, usbManagerClass, "openDevice", "(Landroid/hardware/usb/UsbDevice;)Landroid/hardware/usb/UsbDeviceConnection;");
    jobject conn = (*env)->CallObjectMethod(env, usbManager, openDeviceMethod, device);
    if (conn == NULL || (*env)->ExceptionCheck(env)) {
        logExceptionDetails(env, "openDevice");
        LOGE("❌ [JNI-USB-CLAIM] openDevice returned null for %s (permission not granted?)", deviceName);
        (*env)->DeleteLocalRef(env, device);
        (*env)->DeleteLocalRef(env, usbManager);
        return NULL;
    }

    jclass deviceClass = (*env)->GetObjectClass(env, device);
    jmethodID getInterfaceCountMethod = (*env)->GetMethodID(env, deviceClass, "getInterfaceCount", "()I");
    jint ifaceCount = (*env)->CallIntMethod(env, device, getInterfaceCountMethod);
    if (ifaceCount <= 0) {
        LOGE("❌ [JNI-USB-CLAIM] %s has no interfaces", deviceName);
        (*env)->DeleteLocalRef(env, conn);
        (*env)->DeleteLocalRef(env, device);
        (*env)->DeleteLocalRef(env, usbManager);
        return NULL;
    }
    jmethodID getInterfaceMethod = (*env)->GetMethodID(env, deviceClass, "getInterface", "(I)Landroid/hardware/usb/UsbInterface;");
    jobject iface = (*env)->CallObjectMethod(env, device, getInterfaceMethod, 0); // first interface, same simplification as gousb's TryClaimGousb

    jclass connClass = (*env)->GetObjectClass(env, conn);
    jmethodID claimInterfaceMethod = (*env)->GetMethodID(env, connClass, "claimInterface", "(Landroid/hardware/usb/UsbInterface;Z)Z");
    jboolean claimed = (*env)->CallBooleanMethod(env, conn, claimInterfaceMethod, iface, JNI_TRUE /* force -- evict any kernel driver */);
    if (!claimed || (*env)->ExceptionCheck(env)) {
        logExceptionDetails(env, "claimInterface");
        LOGE("❌ [JNI-USB-CLAIM] claimInterface(force=true) failed for %s", deviceName);
        (*env)->DeleteLocalRef(env, iface);
        (*env)->DeleteLocalRef(env, conn);
        (*env)->DeleteLocalRef(env, device);
        (*env)->DeleteLocalRef(env, usbManager);
        return NULL;
    }

    jclass interfaceClass = (*env)->GetObjectClass(env, iface);
    jmethodID getIdMethod = (*env)->GetMethodID(env, interfaceClass, "getId", "()I");
    jint ifaceNum = (*env)->CallIntMethod(env, iface, getIdMethod);

    jmethodID getRawDescriptorsMethod = (*env)->GetMethodID(env, connClass, "getRawDescriptors", "()[B");
    jbyteArray rawDescArr = (jbyteArray)(*env)->CallObjectMethod(env, conn, getRawDescriptorsMethod);
    char *descHex;
    if (rawDescArr == NULL) {
        LOGW("⚠️ [JNI-USB-CLAIM] getRawDescriptors() returned null for %s", deviceName);
        descHex = strdup("");
    } else {
        jsize descLen = (*env)->GetArrayLength(env, rawDescArr);
        jbyte *descBytes = (*env)->GetByteArrayElements(env, rawDescArr, NULL);
        descHex = bytesToHex((const uint8_t *)descBytes, descLen);
        (*env)->ReleaseByteArrayElements(env, rawDescArr, descBytes, JNI_ABORT);
        (*env)->DeleteLocalRef(env, rawDescArr);
    }

    jobject connGlobal = (*env)->NewGlobalRef(env, conn);
    jobject ifaceGlobal = (*env)->NewGlobalRef(env, iface);

    size_t descHexLen = strlen(descHex);
    char *result = (char *)malloc(64 + descHexLen);
    snprintf(result, 64 + descHexLen, "%lld\t%lld\t%d\t%s",
             (long long)(intptr_t)connGlobal, (long long)(intptr_t)ifaceGlobal, (int)ifaceNum, descHex);

    LOGI("✅ [JNI-USB-CLAIM] claimed %s iface=%d (%d raw descriptor bytes)", deviceName, (int)ifaceNum, (int)(descHexLen / 2));
    free(descHex);

    (*env)->DeleteLocalRef(env, iface);
    (*env)->DeleteLocalRef(env, conn);
    (*env)->DeleteLocalRef(env, device);
    (*env)->DeleteLocalRef(env, usbManager);
    return result;
}

// resolveEndpoint walks ifaceObj's endpoints (a global ref from
// jni_usbClaim) looking for the one whose getAddress() (endpoint number |
// direction bit, exactly the on-wire "ep" byte USB/IP frames carry) matches
// epAddr. Returns a local ref, or NULL if this claimed interface has no
// such endpoint.
static jobject resolveEndpoint(JNIEnv *env, jobject ifaceObj, int epAddr) {
    jclass interfaceClass = (*env)->GetObjectClass(env, ifaceObj);
    jmethodID getEndpointCountMethod = (*env)->GetMethodID(env, interfaceClass, "getEndpointCount", "()I");
    jmethodID getEndpointMethod = (*env)->GetMethodID(env, interfaceClass, "getEndpoint", "(I)Landroid/hardware/usb/UsbEndpoint;");
    jint count = (*env)->CallIntMethod(env, ifaceObj, getEndpointCountMethod);

    for (jint i = 0; i < count; i++) {
        jobject ep = (*env)->CallObjectMethod(env, ifaceObj, getEndpointMethod, i);
        jclass epClass = (*env)->GetObjectClass(env, ep);
        jmethodID getAddressMethod = (*env)->GetMethodID(env, epClass, "getAddress", "()I");
        jint addr = (*env)->CallIntMethod(env, ep, getAddressMethod);
        (*env)->DeleteLocalRef(env, epClass);
        if (addr == epAddr) {
            return ep;
        }
        (*env)->DeleteLocalRef(env, ep);
    }
    return NULL;
}

// jni_usbControlTransfer forwards one control transfer over connHandle
// (from jni_usbClaim). buf is both the OUT payload (bmRequestType bit 7
// clear) and the scratch space for an IN reply; returns bytes
// transferred, or -1 on error/STALL/timeout -- USB/IP's own EPIPE mapping
// happens on the Go side, same as backend_gousb.go's live control fallback.
int jni_usbControlTransfer(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, jlong connHandle,
                            int requestType, int request, int value, int index,
                            void *buf, int len, int timeoutMs) {
    (void)ctx_ptr;
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject conn = (jobject)(intptr_t)connHandle;

    jbyteArray arr = (*env)->NewByteArray(env, len);
    if (len > 0 && (requestType & 0x80) == 0) {
        (*env)->SetByteArrayRegion(env, arr, 0, len, (const jbyte *)buf);
    }

    jclass connClass = (*env)->GetObjectClass(env, conn);
    jmethodID controlTransferMethod = (*env)->GetMethodID(env, connClass, "controlTransfer", "(IIII[BII)I");
    jint n = (*env)->CallIntMethod(env, conn, controlTransferMethod, requestType, request, value, index, arr, len, timeoutMs);

    if ((*env)->ExceptionCheck(env)) {
        logExceptionDetails(env, "controlTransfer");
        (*env)->DeleteLocalRef(env, arr);
        return -1;
    }
    if (n > 0 && (requestType & 0x80) != 0) {
        (*env)->GetByteArrayRegion(env, arr, 0, n, (jbyte *)buf);
    }
    (*env)->DeleteLocalRef(env, arr);
    return (int)n;
}

// jni_usbBulkTransfer forwards one bulk transfer on epAddr (full address
// with direction bit) over connHandle/ifaceHandle. Same buf/return
// convention as jni_usbControlTransfer. A negative return (or 0 for an IN
// read that expected data) is this project's signal for a STALL/error --
// Go's HandleBulk maps that to errnoEPIPE the same way the gousb backend's
// live path does.
int jni_usbBulkTransfer(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, jlong connHandle, jlong ifaceHandle,
                         int epAddr, void *buf, int len, int timeoutMs) {
    (void)ctx_ptr;
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject conn = (jobject)(intptr_t)connHandle;
    jobject iface = (jobject)(intptr_t)ifaceHandle;
    int dirIn = (epAddr & 0x80) != 0;

    jobject ep = resolveEndpoint(env, iface, epAddr);
    if (ep == NULL) {
        LOGE("❌ [JNI-USB-TRANSFER] no endpoint %#02x on claimed interface", epAddr);
        return -1;
    }

    jbyteArray arr = (*env)->NewByteArray(env, len);
    if (len > 0 && !dirIn) {
        (*env)->SetByteArrayRegion(env, arr, 0, len, (const jbyte *)buf);
    }

    jclass connClass = (*env)->GetObjectClass(env, conn);
    jmethodID bulkTransferMethod = (*env)->GetMethodID(env, connClass, "bulkTransfer", "(Landroid/hardware/usb/UsbEndpoint;[BII)I");
    jint n = (*env)->CallIntMethod(env, conn, bulkTransferMethod, ep, arr, len, timeoutMs);

    if ((*env)->ExceptionCheck(env)) {
        logExceptionDetails(env, "bulkTransfer");
        n = -1;
    } else if (n > 0 && dirIn) {
        (*env)->GetByteArrayRegion(env, arr, 0, n, (jbyte *)buf);
    }
    (*env)->DeleteLocalRef(env, arr);
    (*env)->DeleteLocalRef(env, ep);
    return (int)n;
}

// jni_usbClose releases the claimed interface, closes the connection, and
// drops both global refs -- must be called exactly once per successful
// jni_usbClaim (DeviceBackend.Close()).
void jni_usbClose(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, jlong connHandle, jlong ifaceHandle) {
    (void)ctx_ptr;
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject conn = (jobject)(intptr_t)connHandle;
    jobject iface = (jobject)(intptr_t)ifaceHandle;

    jclass connClass = (*env)->GetObjectClass(env, conn);
    jmethodID releaseInterfaceMethod = (*env)->GetMethodID(env, connClass, "releaseInterface", "(Landroid/hardware/usb/UsbInterface;)Z");
    (*env)->CallBooleanMethod(env, conn, releaseInterfaceMethod, iface);
    (*env)->ExceptionClear(env); // best-effort; device may already be unplugged

    jmethodID closeMethod = (*env)->GetMethodID(env, connClass, "close", "()V");
    (*env)->CallVoidMethod(env, conn, closeMethod);
    (*env)->ExceptionClear(env);

    (*env)->DeleteGlobalRef(env, iface);
    (*env)->DeleteGlobalRef(env, conn);
    LOGI("🔌 [JNI-USB-CLAIM] closed");
}
