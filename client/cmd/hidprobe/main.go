//go:build darwin

// Command hidprobe is a throwaway diagnostic for reverse-engineering a HID
// device's raw report layout on macOS. Wacom's consumer tablets (Bamboo/
// Intuos, non-Pro) describe their reports under a fully vendor-defined HID
// usage page, so the standard Digitizer usages (X/Y/Tip/Pressure) don't
// apply -- IOHIDManager still decodes the report into per-field integer
// values (using the device's own logical min/max), but we have to work out
// empirically which field is which by moving/pressing the real hardware and
// watching which value changes. Not part of the shipped product.
package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation

#include <IOKit/hid/IOHIDManager.h>
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <stdint.h>

extern void goValueCallback(uint32_t page, uint32_t usage, uint32_t reportID,
                             int64_t value, int64_t min, int64_t max, uint64_t timestamp);
extern void goRawReportCallback(uint8_t* report, int length);

static void hidValueCallback(void* context, IOReturn result, void* sender, IOHIDValueRef value) {
    (void)context; (void)result; (void)sender;
    IOHIDElementRef elem = IOHIDValueGetElement(value);
    uint32_t page = IOHIDElementGetUsagePage(elem);
    uint32_t usage = IOHIDElementGetUsage(elem);
    uint32_t reportID = IOHIDElementGetReportID(elem);
    CFIndex ival = IOHIDValueGetIntegerValue(value);
    CFIndex lo = IOHIDElementGetLogicalMin(elem);
    CFIndex hi = IOHIDElementGetLogicalMax(elem);
    uint64_t ts = IOHIDValueGetTimeStamp(value);
    goValueCallback(page, usage, reportID, (int64_t)ival, (int64_t)lo, (int64_t)hi, ts);
}

static uint8_t g_reportBuf[256];

static void hidRawReportCallback(void* context, IOReturn result, void* sender,
                                  IOHIDReportType type, uint32_t reportID,
                                  uint8_t* report, CFIndex reportLength) {
    (void)context; (void)result; (void)sender; (void)type; (void)reportID;
    goRawReportCallback(report, (int)reportLength);
}

// openByVidPid finds the first matching device by VID/PID, opens it, and
// registers hidValueCallback on the manager's run loop. Returns 1 on success.
static IOHIDManagerRef g_mgr = NULL;

static int openByVidPid(int vid, int pid) {
    g_mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!g_mgr) return 0;

    CFMutableDictionaryRef match = CFDictionaryCreateMutable(kCFAllocatorDefault, 2,
                                    &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFNumberRef vidRef = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &vid);
    CFNumberRef pidRef = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &pid);
    CFDictionarySetValue(match, CFSTR(kIOHIDVendorIDKey), vidRef);
    CFDictionarySetValue(match, CFSTR(kIOHIDProductIDKey), pidRef);
    CFRelease(vidRef); CFRelease(pidRef);

    IOHIDManagerSetDeviceMatching(g_mgr, match);
    CFRelease(match);

    IOReturn ret = IOHIDManagerOpen(g_mgr, kIOHIDOptionsTypeNone);
    if (ret != kIOReturnSuccess) {
        CFRelease(g_mgr);
        g_mgr = NULL;
        return 0;
    }

    IOHIDManagerRegisterInputValueCallback(g_mgr, hidValueCallback, NULL);
    IOHIDManagerScheduleWithRunLoop(g_mgr, CFRunLoopGetCurrent(), kCFRunLoopDefaultMode);

    CFSetRef devices = IOHIDManagerCopyDevices(g_mgr);
    if (devices) {
        CFIndex n = CFSetGetCount(devices);
        IOHIDDeviceRef* devs = (IOHIDDeviceRef*)malloc(n * sizeof(IOHIDDeviceRef));
        CFSetGetValues(devices, (const void**)devs);
        for (CFIndex i = 0; i < n; i++) {
            IOHIDDeviceRegisterInputReportCallback(devs[i], g_reportBuf, sizeof(g_reportBuf),
                                                    hidRawReportCallback, NULL);
        }
        free(devs);
        CFRelease(devices);
    }
    return 1;
}

static void runLoopForever(void) {
    CFRunLoopRun();
}
*/
import "C"

import (
	"flag"
	"fmt"
	"strconv"
	"unsafe"
)

var showValues bool

//export goValueCallback
func goValueCallback(page, usage, reportID C.uint32_t, value, min, max C.int64_t, timestamp C.uint64_t) {
	if !showValues {
		return
	}
	fmt.Printf("page=0x%04x usage=0x%02x(%d) reportID=%d value=%d [%d..%d]\n",
		uint32(page), uint32(usage), uint32(usage), uint32(reportID), int64(value), int64(min), int64(max))
}

//export goRawReportCallback
func goRawReportCallback(report *C.uint8_t, length C.int) {
	buf := unsafe.Slice((*byte)(unsafe.Pointer(report)), int(length))
	fmt.Printf("RAW[%3d]: % 02x\n", len(buf), buf)
	if len(buf) >= 17 {
		x := uint32(buf[2]) | uint32(buf[3])<<8 | uint32(buf[4])<<16
		y := uint32(buf[5]) | uint32(buf[6])<<8 | uint32(buf[7])<<16
		pressure := uint16(buf[8]) | uint16(buf[9])<<8
		tiltX := int8(buf[10])
		tiltY := int8(buf[11])
		rotation := int16(uint16(buf[12]) | uint16(buf[13])<<8)
		hover := buf[16]
		penByte := buf[1]
		fmt.Printf("  decoded: penByte=%08b x=%d y=%d pressure=%d tiltX=%d tiltY=%d rotation=%d hover=%d\n",
			penByte, x, y, pressure, tiltX, tiltY, rotation, hover)
	}
}

func main() {
	vidStr := flag.String("vid", "056a", "vendor id (hex, no 0x)")
	pidStr := flag.String("pid", "0374", "product id (hex, no 0x)")
	values := flag.Bool("values", false, "also print decoded per-element HID values")
	flag.Parse()
	showValues = *values

	vid64, err := strconv.ParseUint(*vidStr, 16, 32)
	if err != nil {
		fmt.Println("bad -vid:", err)
		return
	}
	pid64, err := strconv.ParseUint(*pidStr, 16, 32)
	if err != nil {
		fmt.Println("bad -pid:", err)
		return
	}

	fmt.Printf("opening HID device vid=0x%04x pid=0x%04x ...\n", vid64, pid64)
	ok := C.openByVidPid(C.int(vid64), C.int(pid64))
	if ok == 0 {
		fmt.Println("failed to open device (not found, or IOHIDManagerOpen failed)")
		return
	}
	fmt.Println("opened. Move the pen, hover, press the tip, press barrel buttons. Ctrl+C to quit.")
	C.runLoopForever()
}
