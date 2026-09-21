//go:build darwin && !ios

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation

#include <IOKit/hid/IOHIDManager.h>
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

// Wacom's consumer tablets (Bamboo/Intuos "CTL-*", not the Pro/AES line)
// describe their input report under a fully vendor-defined HID usage page,
// so there is no standard Digitizer usage to match on the way
// gamepad_darwin.go matches kHIDPage_GenericDesktop/kHIDUsage_GD_GamePad --
// we match on Wacom's USB vendor ID instead and parse the raw report
// ourselves (see pen_protocol.go's decodePenReport doc comment for the byte
// layout, reverse engineered against OpenTabletDriver's IntuosV2Report.cs,
// which ships the same parser for this whole tablet family). WACOM_VENDOR_ID
// must match pen_protocol.go's WacomVendorID -- duplicated here since cgo's
// C preamble can't reference a Go constant.
#define WACOM_VENDOR_ID 0x056A

// enumeratePenTablets fills ids/names/vids/pids with up to maxDevices entries
// for every currently connected Wacom HID device. Returns count.
static int enumeratePenTablets(uint64_t* ids, char** names, int* vids, int* pids, int maxDevices) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return 0;

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

            CFNumberRef v = (CFNumberRef)IOHIDDeviceGetProperty(devs[i], CFSTR(kIOHIDVendorIDKey));
            CFNumberRef p = (CFNumberRef)IOHIDDeviceGetProperty(devs[i], CFSTR(kIOHIDProductIDKey));
            int vv = 0, pv = 0;
            if (v) CFNumberGetValue(v, kCFNumberIntType, &vv);
            if (p) CFNumberGetValue(p, kCFNumberIntType, &pv);
            vids[count] = vv;
            pids[count] = pv;

            CFStringRef nameRef = (CFStringRef)IOHIDDeviceGetProperty(devs[i], CFSTR(kIOHIDProductKey));
            if (nameRef) {
                char buf[256] = {0};
                CFStringGetCString(nameRef, buf, sizeof(buf), kCFStringEncodingUTF8);
                names[count] = strdup(buf);
            } else {
                names[count] = strdup("Wacom Tablet");
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

extern void goPenReportCallback(uint64_t token, uint8_t* report, int length);

typedef struct {
    IOHIDDeviceRef device;
    uint64_t token;
    uint8_t buf[256];
} PenCaptureCtx;

static void penReportCallback(void* context, IOReturn result, void* sender,
                               IOHIDReportType type, uint32_t reportID,
                               uint8_t* report, CFIndex reportLength) {
    (void)result; (void)sender; (void)type; (void)reportID;
    PenCaptureCtx* ctx = (PenCaptureCtx*)context;
    goPenReportCallback(ctx->token, report, (int)reportLength);
}

// openPenCapture finds the device by its stable IOKit registry entry ID (see
// gamepad_capture_darwin.go's openCapture for why registry ID rather than a
// raw IOHIDDeviceRef), opens it, and registers penReportCallback on the
// current run loop. token is an opaque caller-chosen id echoed back on every
// report so Go can route it to the right callback without unsafe.Pointer
// round-tripping through cgo.
static PenCaptureCtx* openPenCapture(uint64_t registryEntryID, uint64_t token) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return NULL;

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
    IOHIDDeviceRef dev = NULL;
    if (devices) {
        CFIndex n = CFSetGetCount(devices);
        IOHIDDeviceRef* devs = (IOHIDDeviceRef*)malloc(n * sizeof(IOHIDDeviceRef));
        CFSetGetValues(devices, (const void**)devs);
        for (CFIndex i = 0; i < n; i++) {
            io_service_t svc = IOHIDDeviceGetService(devs[i]);
            uint64_t entryID = 0;
            if (svc != IO_OBJECT_NULL) {
                IORegistryEntryGetRegistryEntryID(svc, &entryID);
            }
            if (entryID == registryEntryID) {
                dev = devs[i];
                CFRetain(dev);
                break;
            }
        }
        free(devs);
        CFRelease(devices);
    }
    IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
    CFRelease(mgr);

    if (!dev) return NULL;

    IOReturn ret = IOHIDDeviceOpen(dev, kIOHIDOptionsTypeNone);
    if (ret != kIOReturnSuccess) {
        CFRelease(dev);
        return NULL;
    }

    PenCaptureCtx* ctx = (PenCaptureCtx*)calloc(1, sizeof(PenCaptureCtx));
    ctx->device = dev;
    ctx->token = token;

    IOHIDDeviceRegisterInputReportCallback(dev, ctx->buf, sizeof(ctx->buf), penReportCallback, ctx);
    IOHIDDeviceScheduleWithRunLoop(dev, CFRunLoopGetCurrent(), kCFRunLoopDefaultMode);
    return ctx;
}

static void runPenRunLoop(void) {
    // Pump the run loop in short slices so Go can signal shutdown via the
    // stop channel between iterations (mirrors gamepad_capture_darwin.go's
    // ticker-driven poll loop, but IOHIDDeviceRegisterInputReportCallback is
    // push-based, so this just needs to keep the run loop alive).
    CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.05, false);
}

static void closePenCapture(PenCaptureCtx* ctx) {
    if (!ctx) return;
    if (ctx->device) {
        IOHIDDeviceUnscheduleFromRunLoop(ctx->device, CFRunLoopGetCurrent(), kCFRunLoopDefaultMode);
        IOHIDDeviceRegisterInputReportCallback(ctx->device, ctx->buf, sizeof(ctx->buf), NULL, NULL);
        IOHIDDeviceClose(ctx->device, kIOHIDOptionsTypeNone);
        CFRelease(ctx->device);
    }
    free(ctx);
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// PenTabletInfo describes a Wacom-protocol pen tablet currently connected to
// the system.
type PenTabletInfo struct {
	ID   string // stable IOKit registry entry ID, pass to StartPenCapture
	Name string
	VID  uint16
	PID  uint16
}

// ListPenTablets returns every connected Wacom HID device. Only models
// OpenTabletDriver itself tags with the "IntuosV2.IntuosV2ReportParser"
// class -- see wacomIntuosV2Ranges' doc comment for the full list and how
// it was found -- decode into real events; anything else shows up here but
// produces none. That list turned out to span more than the consumer
// CTL-4100/CTL-6100 pair this project originally assumed: it also includes
// the Intuos Pro (PTH-460/660/860) and Cintiq Pro/DTC/DTK display line,
// while some models that sound like the same generation (CTH-680, CTL-470)
// actually use a different, unrelated 10-byte report format this decoder
// does not understand at all.
func ListPenTablets() []PenTabletInfo {
	const maxDevices = 8
	ids := make([]C.uint64_t, maxDevices)
	names := make([]*C.char, maxDevices)
	vids := make([]C.int, maxDevices)
	pids := make([]C.int, maxDevices)

	count := int(C.enumeratePenTablets(&ids[0], &names[0], &vids[0], &pids[0], C.int(maxDevices)))

	result := make([]PenTabletInfo, 0, count)
	for i := 0; i < count; i++ {
		name := C.GoString(names[i])
		C.free(unsafe.Pointer(names[i]))
		result = append(result, PenTabletInfo{
			ID:   fmt.Sprintf("%d", uint64(ids[i])),
			Name: name,
			VID:  uint16(vids[i]),
			PID:  uint16(pids[i]),
		})
	}
	return result
}

// PenCaptureState, PenRange, wacomIntuosV2Ranges, PenMaxX/Y/Pressure,
// PenRangeFor, and decodePenReport now live in pen_protocol.go -- pure Go
// parsing/normalization logic shared with the browser/wasm client's WebHID
// capture source, which decodes the exact same raw report bytes this
// IOHIDManager tap does.

// PenCapture manages an active IOKit HID capture for one pen tablet.
type PenCapture struct {
	token uint64
	stop  chan struct{}
	done  chan struct{}
}

var (
	penCallbacksMu sync.Mutex
	penCallbacks   = map[uint64]func(PenCaptureState){}
	penNextToken   uint64
)

// StartPenCapture opens the Wacom device identified by deviceID (from
// ListPenTablets) and calls onState synchronously from the capture's own
// goroutine every time the device pushes a new HID input report (Wacom's
// consumer tablets push a report per sample rather than requiring polling,
// unlike gamepad_capture_darwin.go's IOHIDQueue+ticker approach).
func StartPenCapture(deviceID string, onState func(PenCaptureState)) (*PenCapture, error) {
	var id uint64
	if _, err := fmt.Sscanf(deviceID, "%d", &id); err != nil || id == 0 {
		return nil, fmt.Errorf("invalid pen tablet device ID %q", deviceID)
	}

	penCallbacksMu.Lock()
	penNextToken++
	token := penNextToken
	penCallbacks[token] = onState
	penCallbacksMu.Unlock()

	cap := &PenCapture{
		token: token,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	openErr := make(chan error, 1)

	go func() {
		// IOHIDDeviceScheduleWithRunLoop ties the device's async callback
		// source to whatever OS thread calls it (via CFRunLoopGetCurrent);
		// the CFRunLoopRunInMode pump below has to run on that exact same
		// thread or the callback never fires. Go can otherwise migrate a
		// goroutine onto a different M between two separate cgo calls, so
		// open (which schedules) and pump (which runs the loop) must both
		// happen inside one LockOSThread'd goroutine -- confirmed live:
		// splitting them across a synchronous open call and a separate
		// pump goroutine silently produced zero callbacks despite the
		// device opening successfully and the same C code working fine in
		// a single-goroutine command-line probe.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		cctx := C.openPenCapture(C.uint64_t(id), C.uint64_t(token))
		if cctx == nil {
			penCallbacksMu.Lock()
			delete(penCallbacks, token)
			penCallbacksMu.Unlock()
			openErr <- fmt.Errorf("failed to open IOKit HID device %s", deviceID)
			close(cap.done)
			return
		}
		openErr <- nil

		defer func() {
			C.closePenCapture(cctx)
			penCallbacksMu.Lock()
			delete(penCallbacks, token)
			penCallbacksMu.Unlock()
			close(cap.done)
		}()
		for {
			select {
			case <-cap.stop:
				return
			default:
				C.runPenRunLoop()
			}
		}
	}()

	if err := <-openErr; err != nil {
		return nil, err
	}
	return cap, nil
}

// Stop halts the capture goroutine and closes the HID device.
func (c *PenCapture) Stop() {
	close(c.stop)
	<-c.done
}

//export goPenReportCallback
func goPenReportCallback(token C.uint64_t, report *C.uint8_t, length C.int) {
	penCallbacksMu.Lock()
	cb := penCallbacks[uint64(token)]
	penCallbacksMu.Unlock()
	if cb == nil {
		return
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(report)), int(length))
	state, ok := decodePenReport(buf)
	if !ok {
		return
	}
	cb(state)
}
