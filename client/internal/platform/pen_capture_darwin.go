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
// ourselves (see pen_capture_darwin.go's doc comment for the byte layout,
// reverse engineered against OpenTabletDriver's IntuosV2Report.cs, which
// ships the same parser for this whole tablet family).
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

// ListPenTablets returns every connected Wacom HID device. Only the
// "IntuosV2"-family consumer report layout (CTL-4100/CTL-6100 and the same
// generation Bamboo/Intuos tablets) is understood -- see pen_capture_darwin.go's
// decodeReport for the byte layout. Newer Pro/AES tablets use a different,
// undecoded protocol and will show up here but produce no events.
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

// PenCaptureState is one decoded pen sample, in raw device units. Coordinate/
// pressure normalization to Moonlight's 0.0..1.0 wire format happens in the
// caller (see disk_widget_pen.go), which is also where MaxX/MaxY/MaxPressure
// -- read once from a Wacom-family HID report descriptor's logical max, but
// hardcoded here since every CTL-4100-generation tablet this parser
// understands reports the same three ranges -- get applied.
type PenCaptureState struct {
	X, Y         uint32
	Pressure     uint16
	TiltX, TiltY int8
	Rotation     int16
	InRange      bool
	TipSwitch    bool
	Button1      bool
	Button2      bool
	Eraser       bool
}

// Wacom "IntuosV2" family digitizer ranges (Wacom CTL-4100/CTL-6100 and
// same-generation Bamboo/Intuos), matching OpenTabletDriver's
// Configurations/Wacom/CTL-4100.json Digitizer/Pen specification.
const (
	PenMaxX        = 15200
	PenMaxY        = 9500
	PenMaxPressure = 4095
)

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

// decodePenReport parses one raw Wacom "IntuosV2" family HID input report
// (report ID 0x10, empirically confirmed byte-for-byte against a real
// CTL-4100 "Intuos S" against OpenTabletDriver's
// Configurations/Parsers/Wacom/IntuosV2/IntuosV2Report.cs, the reference
// open-source parser for this tablet generation -- Wacom's own HID report
// descriptor for this family uses a fully vendor-defined usage page, so
// there is no standards-based way to decode this short of matching a known
// driver's byte layout):
//
//	byte[0]      report ID (0x10)
//	byte[1]      bit0 tip switch, bit1/bit2 barrel buttons, bit4 eraser, bit6
//	             in-range. bit5 is also set while the tip is down (live
//	             capture against real hardware showed 0x40 while hovering
//	             close but not touching, and 0x61 -- bit0|bit5|bit6 -- while
//	             touching), so it's excluded from InRange: treating it as
//	             "proximity" made a real touch-then-lift sequence emit a
//	             spurious "left the tablet" cancellation the instant the tip
//	             came up, because bit5 dropped out exactly at that moment
//	             while the pen was still genuinely hovering (bit6 stayed
//	             set). OpenTabletDriver's IntuosV2Report.cs names this
//	             bit "NearProximity" and IntuosV2.json doesn't map bit6 to
//	             anything -- it's possible OTD only cares about bit5 because
//	             its own hover-vs-touch handling lives elsewhere, or that
//	             bit numbering differs slightly on this firmware; bit6 is
//	             what actually matches the "in range for the whole hover,
//	             not just while touching" behavior this decoder needs.
//	byte[2..5)   X, 24-bit little-endian (byte4 is the high byte; always 0 in
//	             range, since MaxX=15200 fits in 16 bits)
//	byte[5..8)   Y, same 24-bit layout
//	byte[8..10)  pressure, uint16 little-endian, 0..4095
//	byte[10]     tilt X, signed byte (degrees)
//	byte[11]     tilt Y, signed byte (degrees)
//	byte[12..14) rotation, int16 little-endian (Art Pen/airbrush only; 0 on a
//	             standard pen)
//	byte[16]     hover distance (unused here)
func decodePenReport(buf []byte) (PenCaptureState, bool) {
	if len(buf) < 14 || buf[0] != 0x10 {
		return PenCaptureState{}, false
	}
	penByte := buf[1]
	x := uint32(buf[2]) | uint32(buf[3])<<8 | uint32(buf[4])<<16
	y := uint32(buf[5]) | uint32(buf[6])<<8 | uint32(buf[7])<<16
	pressure := uint16(buf[8]) | uint16(buf[9])<<8
	return PenCaptureState{
		X:         x,
		Y:         y,
		Pressure:  pressure,
		TiltX:     int8(buf[10]),
		TiltY:     int8(buf[11]),
		Rotation:  int16(uint16(buf[12]) | uint16(buf[13])<<8),
		InRange:   penByte&0x40 != 0,
		TipSwitch: penByte&0x01 != 0,
		Button1:   penByte&0x02 != 0,
		Button2:   penByte&0x04 != 0,
		Eraser:    penByte&0x10 != 0,
	}, true
}
