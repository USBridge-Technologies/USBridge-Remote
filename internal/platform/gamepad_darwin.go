//go:build darwin

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation

#include <IOKit/hid/IOHIDManager.h>
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

// enumerateGamepads fills buf with up to maxDevices device IDs (uint64) and
// copies the corresponding name into names[i] (caller must free). Returns count.
static int enumerateGamepads(uint64_t* ids, char** names, int maxDevices) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return 0;

    // Match gamepads and joysticks (Usage Page 1, Usage 4 or 5)
    CFMutableArrayRef matchers = CFArrayCreateMutable(kCFAllocatorDefault, 2, &kCFTypeArrayCallBacks);

    int usages[] = {kHIDUsage_GD_GamePad, kHIDUsage_GD_Joystick};
    for (int i = 0; i < 2; i++) {
        CFMutableDictionaryRef m = CFDictionaryCreateMutable(kCFAllocatorDefault, 2,
                                    &kCFTypeDictionaryKeyCallBacks,
                                    &kCFTypeDictionaryValueCallBacks);
        int up = kHIDPage_GenericDesktop;
        CFNumberRef upRef = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &up);
        CFNumberRef uRef  = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &usages[i]);
        CFDictionarySetValue(m, CFSTR(kIOHIDDeviceUsagePageKey), upRef);
        CFDictionarySetValue(m, CFSTR(kIOHIDDeviceUsageKey),     uRef);
        CFRelease(upRef); CFRelease(uRef);
        CFArrayAppendValue(matchers, m);
        CFRelease(m);
    }
    IOHIDManagerSetDeviceMatchingMultiple(mgr, matchers);
    CFRelease(matchers);

    IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone);

    CFSetRef devices = IOHIDManagerCopyDevices(mgr);
    int count = 0;
    if (devices) {
        CFIndex n = CFSetGetCount(devices);
        IOHIDDeviceRef* devs = (IOHIDDeviceRef*)malloc(n * sizeof(IOHIDDeviceRef));
        CFSetGetValues(devices, (const void**)devs);
        for (CFIndex i = 0; i < n && count < maxDevices; i++) {
            io_service_t svc = IOHIDDeviceGetService(devs[i]);
            uint64_t entryID = 0;
            if (svc != IO_OBJECT_NULL) {
                IORegistryEntryGetRegistryEntryID(svc, &entryID);
            }
            if (entryID == 0) continue;
            ids[count] = entryID;
            CFStringRef nameRef = (CFStringRef)IOHIDDeviceGetProperty(devs[i], CFSTR(kIOHIDProductKey));
            if (nameRef) {
                char buf[256] = {0};
                CFStringGetCString(nameRef, buf, sizeof(buf), kCFStringEncodingUTF8);
                names[count] = strdup(buf);
            } else {
                names[count] = strdup("Unknown Gamepad");
            }
            count++;
        }
        free(devs);
        CFRelease(devices);
    }
    IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
    CFRelease(mgr);
    return count;
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// GamepadDevice describes a system gamepad.
type GamepadDevice struct {
	ID        string
	Name      string
	VendorID  string // e.g. "0x045e" (empty if not available via IOKit)
	ProductID string // e.g. "0x028e"
}

// EnumerateGamepads returns all gamepads currently connected to the system.
func EnumerateGamepads() []GamepadDevice {
	const maxDevices = 16
	ids := make([]C.uint64_t, maxDevices)
	names := make([]*C.char, maxDevices)

	count := int(C.enumerateGamepads(&ids[0], &names[0], C.int(maxDevices)))

	result := make([]GamepadDevice, 0, count)
	for i := 0; i < count; i++ {
		name := C.GoString(names[i])
		C.free(unsafe.Pointer(names[i]))
		result = append(result, GamepadDevice{
			ID:   fmt.Sprintf("%d", uint64(ids[i])),
			Name: name,
		})
	}
	return result
}
