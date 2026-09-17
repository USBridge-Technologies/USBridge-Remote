//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation

#include <IOKit/hid/IOHIDManager.h>
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

#define WACOM_VENDOR_ID 0x056A

static int getIntProp(IOHIDDeviceRef dev, CFStringRef key) {
    CFNumberRef n = (CFNumberRef)IOHIDDeviceGetProperty(dev, key);
    int v = -1;
    if (n) CFNumberGetValue(n, kCFNumberIntType, &v);
    return v;
}

static int dumpFirstWacom(unsigned char* descBuf, int descBufLen) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) { printf("no mgr\n"); return -1; }
    int vid = WACOM_VENDOR_ID;
    CFMutableDictionaryRef match = CFDictionaryCreateMutable(kCFAllocatorDefault, 1,
                                    &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFNumberRef vidRef = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &vid);
    CFDictionarySetValue(match, CFSTR(kIOHIDVendorIDKey), vidRef);
    CFRelease(vidRef);
    IOHIDManagerSetDeviceMatching(mgr, match);
    CFRelease(match);
    IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone);

    CFSetRef devices = IOHIDManagerCopyDevices(mgr);
    if (!devices || CFSetGetCount(devices) == 0) {
        printf("no wacom devices found\n");
        return -1;
    }
    CFIndex n = CFSetGetCount(devices);
    IOHIDDeviceRef* devs = (IOHIDDeviceRef*)malloc(n * sizeof(IOHIDDeviceRef));
    CFSetGetValues(devices, (const void**)devs);

    for (CFIndex i = 0; i < n; i++) {
        IOHIDDeviceRef dev = devs[i];
        CFStringRef nameRef = (CFStringRef)IOHIDDeviceGetProperty(dev, CFSTR(kIOHIDProductKey));
        char nameBuf[256] = {0};
        if (nameRef) CFStringGetCString(nameRef, nameBuf, sizeof(nameBuf), kCFStringEncodingUTF8);
        printf("--- device %ld: %s ---\n", (long)i, nameBuf);
        printf("VendorID=%#x ProductID=%#x VersionNumber=%d\n",
            getIntProp(dev, CFSTR(kIOHIDVendorIDKey)),
            getIntProp(dev, CFSTR(kIOHIDProductIDKey)),
            getIntProp(dev, CFSTR(kIOHIDVersionNumberKey)));
        printf("PrimaryUsagePage=%#x PrimaryUsage=%#x\n",
            getIntProp(dev, CFSTR(kIOHIDPrimaryUsagePageKey)),
            getIntProp(dev, CFSTR(kIOHIDPrimaryUsageKey)));
        printf("MaxInputReportSize=%d MaxOutputReportSize=%d MaxFeatureReportSize=%d\n",
            getIntProp(dev, CFSTR(kIOHIDMaxInputReportSizeKey)),
            getIntProp(dev, CFSTR(kIOHIDMaxOutputReportSizeKey)),
            getIntProp(dev, CFSTR(kIOHIDMaxFeatureReportSizeKey)));
        printf("LocationID=%#x TransportKey follows below\n", getIntProp(dev, CFSTR(kIOHIDLocationIDKey)));

        CFStringRef transport = (CFStringRef)IOHIDDeviceGetProperty(dev, CFSTR(kIOHIDTransportKey));
        if (transport) {
            char tbuf[64] = {0};
            CFStringGetCString(transport, tbuf, sizeof(tbuf), kCFStringEncodingUTF8);
            printf("Transport=%s\n", tbuf);
        }

        CFDataRef desc = (CFDataRef)IOHIDDeviceGetProperty(dev, CFSTR(kIOHIDReportDescriptorKey));
        if (desc) {
            CFIndex len = CFDataGetLength(desc);
            printf("ReportDescriptor: %ld bytes\n", (long)len);
            const UInt8* bytes = CFDataGetBytePtr(desc);
            for (CFIndex j = 0; j < len; j++) {
                printf("%02x ", bytes[j]);
                if ((j+1) % 16 == 0) printf("\n");
            }
            printf("\n");
            if (i == 0 && descBuf != NULL) {
                CFIndex copyLen = len;
                if (copyLen > descBufLen) copyLen = descBufLen;
                memcpy(descBuf, bytes, copyLen);
            }
        } else {
            printf("no report descriptor property\n");
        }

        // Also try the service-level registry property (some drivers store it
        // there instead of/in addition to the IOHIDDevice property).
        io_service_t svc = IOHIDDeviceGetService(dev);
        if (svc != IO_OBJECT_NULL) {
            CFTypeRef regDesc = IORegistryEntryCreateCFProperty(svc, CFSTR("ReportDescriptor"), kCFAllocatorDefault, 0);
            if (regDesc) {
                printf("(registry ReportDescriptor also present, type id=%lu)\n", (unsigned long)CFGetTypeID(regDesc));
                CFRelease(regDesc);
            }
        }
    }
    free(devs);
    CFRelease(devices);
    IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
    CFRelease(mgr);
    return 0;
}
*/
import "C"

func main() {
	buf := make([]byte, 4096)
	C.dumpFirstWacom((*C.uchar)(&buf[0]), C.int(len(buf)))
}
