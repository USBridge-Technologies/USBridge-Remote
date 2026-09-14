#include <jni.h>
#include <android/log.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdarg.h>

#define LOG_TAG "USB_JNI"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)
#define LOGW(...) __android_log_print(ANDROID_LOG_WARN, LOG_TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

// Same pattern as saf_jni_android.c: env/ctx come in fresh on every call
// (from Go's driver.RunNative), no global JavaVM caching needed here since
// list enumeration is a single synchronous round trip, not an async
// permission callback.

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
        LOGE("❌ [JNI-USB-EXCEPTION] %s: %s", prefix, cMessage);
        (*env)->ReleaseStringUTFChars(env, message, cMessage);
        (*env)->DeleteLocalRef(env, message);
    } else {
        LOGE("❌ [JNI-USB-EXCEPTION] %s: (no message)", prefix);
    }
    (*env)->DeleteLocalRef(env, exceptionClass);
    (*env)->DeleteLocalRef(env, exception);
}

// jni_usbHostSupported reports whether this device declares
// android.hardware.usb.host (PackageManager.FEATURE_USB_HOST). Devices
// without it (most phones lacking OTG wiring, some tablets) never expose a
// UsbManager device list worth showing -- the Go caller uses this to return
// an empty list up front instead of a confusing always-empty "USB devices"
// section.
int jni_usbHostSupported(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject ctx = (jobject)ctx_ptr;

    jclass contextClass = (*env)->GetObjectClass(env, ctx);
    jmethodID getPackageManagerMethod = (*env)->GetMethodID(env, contextClass, "getPackageManager", "()Landroid/content/pm/PackageManager;");
    jobject pm = (*env)->CallObjectMethod(env, ctx, getPackageManagerMethod);
    if (pm == NULL || (*env)->ExceptionCheck(env)) {
        logExceptionDetails(env, "getPackageManager");
        return 0;
    }

    jclass pmClass = (*env)->GetObjectClass(env, pm);
    jmethodID hasSystemFeatureMethod = (*env)->GetMethodID(env, pmClass, "hasSystemFeature", "(Ljava/lang/String;)Z");
    jstring feature = (*env)->NewStringUTF(env, "android.hardware.usb.host");
    jboolean supported = (*env)->CallBooleanMethod(env, pm, hasSystemFeatureMethod, feature);

    (*env)->DeleteLocalRef(env, feature);
    (*env)->DeleteLocalRef(env, pm);
    (*env)->DeleteLocalRef(env, pmClass);
    (*env)->DeleteLocalRef(env, contextClass);

    if ((*env)->ExceptionCheck(env)) {
        logExceptionDetails(env, "hasSystemFeature");
        return 0;
    }
    return supported ? 1 : 0;
}

// growBuf appends fmt/... to *buf (malloc'd, NUL-terminated), growing it as
// needed. *cap is the allocated size, *len the used size (excluding NUL).
static void growBuf(char **buf, size_t *cap, size_t *len, const char *fmt, ...) {
    char tmp[1024];
    va_list ap;
    va_start(ap, fmt);
    int n = vsnprintf(tmp, sizeof(tmp), fmt, ap);
    va_end(ap);
    if (n < 0) {
        return;
    }
    size_t need = *len + (size_t)n + 1;
    if (need > *cap) {
        size_t newCap = *cap == 0 ? 4096 : *cap;
        while (newCap < need) {
            newCap *= 2;
        }
        char *grown = (char *)realloc(*buf, newCap);
        if (grown == NULL) {
            return;
        }
        *buf = grown;
        *cap = newCap;
    }
    memcpy(*buf + *len, tmp, (size_t)n);
    *len += (size_t)n;
    (*buf)[*len] = '\0';
}

// sanitizeField replaces tab/newline with a space in-place so a device's
// manufacturer/product string (attacker-controlled firmware string data,
// same trust level as any USB descriptor) can't smuggle extra columns or
// rows into the line format jni_listUsbDevices emits.
static void sanitizeField(char *s) {
    for (; *s != '\0'; s++) {
        if (*s == '\t' || *s == '\n' || *s == '\r') {
            *s = ' ';
        }
    }
}

// jni_listUsbDevices enumerates UsbManager.getDeviceList() and returns a
// malloc'd, tab-separated line per device (caller frees with free()):
//   deviceName \t vid(hex) \t pid(hex) \t deviceClass(decimal) \t manufacturer \t product
// deviceName is the kernel device node path (e.g. "/dev/bus/usb/001/002")
// Android hands back from UsbDevice.getDeviceName() -- used as InstanceID
// on the Go side the same way Windows' SetupAPI instance id and the mac HID
// registry id are, since Android app sandboxes can't read
// /sys/bus/usb/devices themselves to derive a real Linux busid.
// Returns NULL on error or when there are no devices (indistinguishable on
// purpose -- Go treats both as "no devices").
char* jni_listUsbDevices(uintptr_t jni_env_ptr, uintptr_t ctx_ptr) {
    JNIEnv *env = (JNIEnv *)jni_env_ptr;
    jobject ctx = (jobject)ctx_ptr;

    jclass contextClass = (*env)->GetObjectClass(env, ctx);
    jmethodID getSystemServiceMethod = (*env)->GetMethodID(env, contextClass, "getSystemService", "(Ljava/lang/String;)Ljava/lang/Object;");
    jstring usbService = (*env)->NewStringUTF(env, "usb");
    jobject usbManager = (*env)->CallObjectMethod(env, ctx, getSystemServiceMethod, usbService);
    (*env)->DeleteLocalRef(env, usbService);
    if (usbManager == NULL || (*env)->ExceptionCheck(env)) {
        logExceptionDetails(env, "getSystemService(usb)");
        return NULL;
    }

    jclass usbManagerClass = (*env)->GetObjectClass(env, usbManager);
    jmethodID getDeviceListMethod = (*env)->GetMethodID(env, usbManagerClass, "getDeviceList", "()Ljava/util/HashMap;");
    jobject deviceMap = (*env)->CallObjectMethod(env, usbManager, getDeviceListMethod);
    if (deviceMap == NULL || (*env)->ExceptionCheck(env)) {
        logExceptionDetails(env, "getDeviceList");
        return NULL;
    }

    jclass mapClass = (*env)->FindClass(env, "java/util/HashMap");
    jmethodID valuesMethod = (*env)->GetMethodID(env, mapClass, "values", "()Ljava/util/Collection;");
    jobject values = (*env)->CallObjectMethod(env, deviceMap, valuesMethod);

    jclass collectionClass = (*env)->FindClass(env, "java/util/Collection");
    jmethodID toArrayMethod = (*env)->GetMethodID(env, collectionClass, "toArray", "()[Ljava/lang/Object;");
    jobjectArray devices = (jobjectArray)(*env)->CallObjectMethod(env, values, toArrayMethod);
    jsize count = (*env)->GetArrayLength(env, devices);

    jclass usbDeviceClass = (*env)->FindClass(env, "android/hardware/usb/UsbDevice");
    jmethodID getDeviceNameMethod = (*env)->GetMethodID(env, usbDeviceClass, "getDeviceName", "()Ljava/lang/String;");
    jmethodID getVendorIdMethod = (*env)->GetMethodID(env, usbDeviceClass, "getVendorId", "()I");
    jmethodID getProductIdMethod = (*env)->GetMethodID(env, usbDeviceClass, "getProductId", "()I");
    jmethodID getDeviceClassMethod = (*env)->GetMethodID(env, usbDeviceClass, "getDeviceClass", "()I");
    jmethodID getManufacturerNameMethod = (*env)->GetMethodID(env, usbDeviceClass, "getManufacturerName", "()Ljava/lang/String;");
    jmethodID getProductNameMethod = (*env)->GetMethodID(env, usbDeviceClass, "getProductName", "()Ljava/lang/String;");

    char *buf = NULL;
    size_t cap = 0, len = 0;

    for (jsize i = 0; i < count; i++) {
        jobject device = (*env)->GetObjectArrayElement(env, devices, i);
        if (device == NULL) {
            continue;
        }

        jstring jName = (jstring)(*env)->CallObjectMethod(env, device, getDeviceNameMethod);
        jint vid = (*env)->CallIntMethod(env, device, getVendorIdMethod);
        jint pid = (*env)->CallIntMethod(env, device, getProductIdMethod);
        jint devClass = (*env)->CallIntMethod(env, device, getDeviceClassMethod);
        jstring jMfr = (jstring)(*env)->CallObjectMethod(env, device, getManufacturerNameMethod);
        jstring jProd = (jstring)(*env)->CallObjectMethod(env, device, getProductNameMethod);
        (*env)->ExceptionClear(env); // getManufacturerName/getProductName can throw on odd descriptors; not fatal

        const char *cName = jName ? (*env)->GetStringUTFChars(env, jName, NULL) : "";
        const char *cMfr = jMfr ? (*env)->GetStringUTFChars(env, jMfr, NULL) : "";
        const char *cProd = jProd ? (*env)->GetStringUTFChars(env, jProd, NULL) : "";

        char mfrBuf[256];
        char prodBuf[256];
        snprintf(mfrBuf, sizeof(mfrBuf), "%s", cMfr);
        snprintf(prodBuf, sizeof(prodBuf), "%s", cProd);
        sanitizeField(mfrBuf);
        sanitizeField(prodBuf);

        growBuf(&buf, &cap, &len, "%s\t%04x\t%04x\t%d\t%s\t%s\n",
                cName, (unsigned int)(vid & 0xFFFF), (unsigned int)(pid & 0xFFFF), (int)devClass, mfrBuf, prodBuf);

        if (jName) { (*env)->ReleaseStringUTFChars(env, jName, cName); (*env)->DeleteLocalRef(env, jName); }
        if (jMfr) { (*env)->ReleaseStringUTFChars(env, jMfr, cMfr); (*env)->DeleteLocalRef(env, jMfr); }
        if (jProd) { (*env)->ReleaseStringUTFChars(env, jProd, cProd); (*env)->DeleteLocalRef(env, jProd); }
        (*env)->DeleteLocalRef(env, device);
    }

    LOGI("✅ [JNI-USB] enumerated %d USB host device(s)", (int)count);
    return buf; // NULL if count == 0 or nothing appended
}
