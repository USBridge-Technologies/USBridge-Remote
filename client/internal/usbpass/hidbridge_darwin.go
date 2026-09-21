//go:build darwin && !ios

// HID-descriptor-level USB/IP export for macOS.
//
// The Windows/Linux client claims a device's raw USB endpoints directly via
// libusb (backend_gousb.go) and forwards bulk/control transfers verbatim.
// That path is not available for a HID-class device on macOS: libusb's
// detach_kernel_driver call on a HID interface returns LIBUSB_ERROR_ACCESS
// (confirmed live against a real Wacom Intuos S -- macOS simply refuses to
// hand a HID interface to a second claimant the way Linux's usbfs does),
// which is exactly why this project's earlier pen/tablet work
// (pen_capture_darwin.go) went the semantic-CGEvent-on-the-agent route
// instead of literal USB/IP passthrough.
//
// This file takes a different path that sidesteps the libusb conflict
// entirely: IOHIDManager reads a HID device's input reports and its raw HID
// Report Descriptor (kIOHIDReportDescriptorKey) without ever claiming
// exclusive ownership of the interface -- the same non-exclusive tap
// pen_capture_darwin.go already relies on. Wrapping that tap in a synthetic
// but byte-faithful USB Device/Configuration/HID descriptor (using the
// device's *real* VID/PID/report descriptor, not placeholders) lets the
// existing USB/IP export server (server.go) present it to a VHCI-importing
// agent exactly as if it were a real USB HID device on that machine's own
// bus -- so the agent OS's own native class/vendor driver (e.g. Wacom's
// Windows driver) enumerates and parses it itself, with no protocol
// knowledge needed on the agent side at all.
//
// Trade-off vs real USB/IP: only INPUT reports are forwarded. SET_REPORT /
// GET_REPORT (feature reports -- Wacom uses these for vendor configuration,
// e.g. tablet-area mapping) are acknowledged but not actually round-tripped
// to the physical device, and macOS keeps the device too (this taps input
// reports, it does not detach the interface) -- both matching the same
// scope the user asked to wrap, not a full bidirectional mirror.
package usbpass

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation

#include <IOKit/hid/IOHIDManager.h>
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

extern void goHIDBridgeReport(uint64_t token, uint8_t* report, int length);

typedef struct {
    IOHIDDeviceRef device;
    uint64_t token;
    uint8_t* buf;
    int bufLen;
} HIDBridgeCtx;

static void hidBridgeReportCallback(void* context, IOReturn result, void* sender,
                                     IOHIDReportType type, uint32_t reportID,
                                     uint8_t* report, CFIndex reportLength) {
    (void)result; (void)sender; (void)type; (void)reportID;
    HIDBridgeCtx* ctx = (HIDBridgeCtx*)context;
    goHIDBridgeReport(ctx->token, report, (int)reportLength);
}

static IOHIDDeviceRef findHIDDeviceByVIDPID(int vid, int pid) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return NULL;
    CFMutableDictionaryRef match = CFDictionaryCreateMutable(kCFAllocatorDefault, 2,
                                    &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFNumberRef vidRef = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &vid);
    CFNumberRef pidRef = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &pid);
    CFDictionarySetValue(match, CFSTR(kIOHIDVendorIDKey), vidRef);
    CFDictionarySetValue(match, CFSTR(kIOHIDProductIDKey), pidRef);
    CFRelease(vidRef);
    CFRelease(pidRef);
    IOHIDManagerSetDeviceMatching(mgr, match);
    CFRelease(match);
    IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone);

    CFSetRef devices = IOHIDManagerCopyDevices(mgr);
    IOHIDDeviceRef found = NULL;
    if (devices) {
        CFIndex n = CFSetGetCount(devices);
        if (n > 0) {
            IOHIDDeviceRef* devs = (IOHIDDeviceRef*)malloc(n * sizeof(IOHIDDeviceRef));
            CFSetGetValues(devices, (const void**)devs);
            found = devs[0];
            CFRetain(found);
            free(devs);
        }
        CFRelease(devices);
    }
    IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
    CFRelease(mgr);
    return found;
}

static int getIntProp(IOHIDDeviceRef dev, CFStringRef key, int def) {
    CFNumberRef n = (CFNumberRef)IOHIDDeviceGetProperty(dev, key);
    int v = def;
    if (n) CFNumberGetValue(n, kCFNumberIntType, &v);
    return v;
}

static int getStringProp(IOHIDDeviceRef dev, CFStringRef key, char* buf, int bufLen) {
    CFStringRef s = (CFStringRef)IOHIDDeviceGetProperty(dev, key);
    if (!s) return 0;
    if (!CFStringGetCString(s, buf, bufLen, kCFStringEncodingUTF8)) return 0;
    return (int)strlen(buf);
}

// Thin per-key wrappers -- CFSTR() is a compile-time C macro and cannot be
// invoked from Go, so each property key needs its own tiny C function
// rather than passing the key across the cgo boundary.
static int getVersionNumber(IOHIDDeviceRef dev) { return getIntProp(dev, CFSTR(kIOHIDVersionNumberKey), 0x0100); }
static int getMaxInputReportSize(IOHIDDeviceRef dev) { return getIntProp(dev, CFSTR(kIOHIDMaxInputReportSizeKey), 64); }
static int getManufacturerString(IOHIDDeviceRef dev, char* buf, int bufLen) { return getStringProp(dev, CFSTR(kIOHIDManufacturerKey), buf, bufLen); }
static int getProductString(IOHIDDeviceRef dev, char* buf, int bufLen) { return getStringProp(dev, CFSTR(kIOHIDProductKey), buf, bufLen); }
static int getSerialNumberString(IOHIDDeviceRef dev, char* buf, int bufLen) { return getStringProp(dev, CFSTR(kIOHIDSerialNumberKey), buf, bufLen); }
static int getDeviceUsagePage(IOHIDDeviceRef dev) { return getIntProp(dev, CFSTR(kIOHIDDeviceUsagePageKey), 0); }
static int getDeviceUsage(IOHIDDeviceRef dev) { return getIntProp(dev, CFSTR(kIOHIDDeviceUsageKey), 0); }

// findHIDDeviceByEntryID locates one HID device by its stable IOKit registry
// entry ID -- the only unambiguous handle for one interface of a composite
// HID device that exposes several interfaces under the same VID:PID (e.g. a
// Logitech Unifying receiver, confirmed live: one physical receiver, four
// separate IOHIDDevice entries at the same 046d:c548, one per logical
// function). findHIDDeviceByVIDPID's first-match behavior can't tell those
// apart; this can.
static IOHIDDeviceRef findHIDDeviceByEntryID(uint64_t entryID) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return NULL;
    IOHIDManagerSetDeviceMatching(mgr, NULL);
    IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone);

    CFSetRef devices = IOHIDManagerCopyDevices(mgr);
    IOHIDDeviceRef found = NULL;
    if (devices) {
        CFIndex n = CFSetGetCount(devices);
        IOHIDDeviceRef* devs = (IOHIDDeviceRef*)malloc(n * sizeof(IOHIDDeviceRef));
        CFSetGetValues(devices, (const void**)devs);
        for (CFIndex i = 0; i < n; i++) {
            io_service_t svc = IOHIDDeviceGetService(devs[i]);
            uint64_t id = 0;
            if (svc != IO_OBJECT_NULL) IORegistryEntryGetRegistryEntryID(svc, &id);
            if (id == entryID) {
                found = devs[i];
                CFRetain(found);
                break;
            }
        }
        free(devs);
        CFRelease(devices);
    }
    IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
    CFRelease(mgr);
    return found;
}

// getReportDescriptor copies dev's raw HID report descriptor into buf (up to
// bufLen bytes) and returns the real length (may exceed bufLen -- caller
// should retry with a bigger buffer in that case).
static int getReportDescriptor(IOHIDDeviceRef dev, uint8_t* buf, int bufLen) {
    CFDataRef desc = (CFDataRef)IOHIDDeviceGetProperty(dev, CFSTR(kIOHIDReportDescriptorKey));
    if (!desc) return 0;
    CFIndex len = CFDataGetLength(desc);
    CFIndex n = len;
    if (n > bufLen) n = bufLen;
    if (n > 0) memcpy(buf, CFDataGetBytePtr(desc), n);
    return (int)len;
}

static int isNullDevice(IOHIDDeviceRef dev) {
    return dev == NULL;
}

static void releaseHIDDevice(IOHIDDeviceRef dev) {
    if (dev) CFRelease(dev);
}

// hidDeviceHasUSBAncestor walks svc's IOService-plane ancestry looking for a
// real IOUSBHostDevice/IOUSBDevice node -- the only reliable signal this
// project found (live, against a real machine) that an IOHIDDevice is
// backed by an actual device on the physical USB bus, as opposed to a
// software-synthesized one. kIOHIDTransportKey looked like the obvious
// discriminator but lies: a virtual Xbox-360-compatible pad conjured by a
// gamepad-remapping driver, and macOS's own "Virtual Dictation Input
// Device", both self-report kIOHIDTransportKey "USB" despite having no
// entry anywhere in the real USB tree (confirmed via ioreg -- neither
// showed up under IOUSBHostDevice, unlike the real gamepad sitting right
// next to them in the same enumeration). Bounded to 10 hops so a broken
// registry chain can't spin forever.
static int hidDeviceHasUSBAncestor(io_service_t svc) {
    io_service_t cur = svc;
    IOObjectRetain(cur);
    for (int depth = 0; depth < 10; depth++) {
        if (IOObjectConformsTo(cur, "IOUSBHostDevice") || IOObjectConformsTo(cur, "IOUSBDevice")) {
            IOObjectRelease(cur);
            return 1;
        }
        io_service_t parent = IO_OBJECT_NULL;
        kern_return_t kr = IORegistryEntryGetParentEntry(cur, kIOServicePlane, &parent);
        IOObjectRelease(cur);
        if (kr != KERN_SUCCESS || parent == IO_OBJECT_NULL) break;
        cur = parent;
    }
    return 0;
}

// enumerateAllHIDDevices fills ids/names/vids/pids with up to maxDevices
// entries for every currently connected HID device -- no vendor/usage-page
// filter, unlike pen_capture_darwin.go's Wacom-only enumeratePenTablets --
// matching how the Windows/Linux raw-USB path (list.go's listSysfs/
// listViaBroker) already lists every USB device with no class restriction.
// Three exclusions keep the list to real, claimable devices:
//   - no real IOUSBHostDevice/IOUSBDevice ancestor (hidDeviceHasUSBAncestor):
//     drops software-synthesized HID devices. Live-verified: without this,
//     a single Mac surfaced a phantom "Xbox 360 Controller" and a
//     "Razer DriverKit VirtualJoystic" alongside the one real gamepad
//     actually plugged in, plus macOS's own "Virtual Dictation Input
//     Device" -- none backed by anything on the real USB bus, all
//     un-attachable garbage that would confuse the passthrough list.
//   - "Built-In" devices (kIOHIDBuiltInKey): Apple's own internal keyboard/
//     trackpad/Touch ID/ambient-light sensor etc. -- these can sit on an
//     internal USB bus too (passing hasUSBAncestor) but must never be
//     offered as passthrough candidates given this bridge's tap is
//     non-exclusive (see file doc comment): silently mirroring every local
//     keystroke on the built-in keyboard to a remote peer would be a
//     serious surprise, not a feature. External keyboards/mice are NOT
//     excluded (their being HID passthrough candidates is intentional --
//     the same is already true for raw USB on Windows/Linux).
//   - devices with no VID/PID (some synthetic/virtual IOHIDDevice nodes
//     report neither): there's no descriptor to build for them.
static int enumerateAllHIDDevices(uint64_t* ids, char** names, int* vids, int* pids,
                                   int* usagePages, int* usages, int maxDevices) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return 0;
    IOHIDManagerSetDeviceMatching(mgr, NULL); // no filter -- every HID device
    IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone);

    CFSetRef devices = IOHIDManagerCopyDevices(mgr);
    int count = 0;
    if (devices) {
        CFIndex n = CFSetGetCount(devices);
        IOHIDDeviceRef* devs = (IOHIDDeviceRef*)malloc(n * sizeof(IOHIDDeviceRef));
        CFSetGetValues(devices, (const void**)devs);
        for (CFIndex i = 0; i < n && count < maxDevices; i++) {
            io_service_t svc = IOHIDDeviceGetService(devs[i]);
            if (svc == IO_OBJECT_NULL || !hidDeviceHasUSBAncestor(svc)) continue;

            CFTypeRef builtIn = IOHIDDeviceGetProperty(devs[i], CFSTR("Built-In"));
            if (builtIn) {
                int bi = 0;
                CFTypeID t = CFGetTypeID(builtIn);
                if (t == CFBooleanGetTypeID()) {
                    bi = CFBooleanGetValue((CFBooleanRef)builtIn) ? 1 : 0;
                } else if (t == CFNumberGetTypeID()) {
                    CFNumberGetValue((CFNumberRef)builtIn, kCFNumberIntType, &bi);
                }
                if (bi) continue;
            }

            CFNumberRef v = (CFNumberRef)IOHIDDeviceGetProperty(devs[i], CFSTR(kIOHIDVendorIDKey));
            CFNumberRef p = (CFNumberRef)IOHIDDeviceGetProperty(devs[i], CFSTR(kIOHIDProductIDKey));
            int vv = 0, pv = 0;
            if (v) CFNumberGetValue(v, kCFNumberIntType, &vv);
            if (p) CFNumberGetValue(p, kCFNumberIntType, &pv);
            if (vv == 0 && pv == 0) continue;

            uint64_t entryID = 0;
            IORegistryEntryGetRegistryEntryID(svc, &entryID);
            if (entryID == 0) continue;

            ids[count] = entryID;
            vids[count] = vv;
            pids[count] = pv;
            usagePages[count] = getDeviceUsagePage(devs[i]);
            usages[count] = getDeviceUsage(devs[i]);

            CFStringRef nameRef = (CFStringRef)IOHIDDeviceGetProperty(devs[i], CFSTR(kIOHIDProductKey));
            if (nameRef) {
                char buf[256] = {0};
                CFStringGetCString(nameRef, buf, sizeof(buf), kCFStringEncodingUTF8);
                names[count] = strdup(buf);
            } else {
                names[count] = strdup("HID Device");
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

// openHIDBridge opens dev for input-report capture on the calling thread's
// run loop -- must be paired with runHIDBridgeRunLoop on that exact same OS
// thread, same reasoning as pen_capture_darwin.go's openPenCapture (a Go
// goroutine is not pinned to one OS thread between separate cgo calls, and
// IOHIDDeviceScheduleWithRunLoop ties the callback source to whichever
// thread happens to call it).
static HIDBridgeCtx* openHIDBridge(IOHIDDeviceRef dev, uint64_t token, int maxReportSize) {
    if (!dev) return NULL;
    IOReturn ret = IOHIDDeviceOpen(dev, kIOHIDOptionsTypeNone);
    if (ret != kIOReturnSuccess) return NULL;

    if (maxReportSize < 64) maxReportSize = 64;
    HIDBridgeCtx* ctx = (HIDBridgeCtx*)calloc(1, sizeof(HIDBridgeCtx));
    ctx->device = dev;
    ctx->token = token;
    ctx->buf = (uint8_t*)malloc((size_t)maxReportSize);
    ctx->bufLen = maxReportSize;

    IOHIDDeviceRegisterInputReportCallback(dev, ctx->buf, ctx->bufLen, hidBridgeReportCallback, ctx);
    IOHIDDeviceScheduleWithRunLoop(dev, CFRunLoopGetCurrent(), kCFRunLoopDefaultMode);
    return ctx;
}

static void runHIDBridgeRunLoop(void) {
    CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.05, false);
}

static void closeHIDBridge(HIDBridgeCtx* ctx) {
    if (!ctx) return;
    if (ctx->device) {
        IOHIDDeviceUnscheduleFromRunLoop(ctx->device, CFRunLoopGetCurrent(), kCFRunLoopDefaultMode);
        IOHIDDeviceRegisterInputReportCallback(ctx->device, ctx->buf, ctx->bufLen, NULL, NULL);
        IOHIDDeviceClose(ctx->device, kIOHIDOptionsTypeNone);
        CFRelease(ctx->device);
    }
    free(ctx->buf);
    free(ctx);
}
*/
import "C"

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/sirupsen/logrus"
)

// hidBridgeInfo is everything read once, statically, off the real device
// (via non-exclusive IOHIDManager property gets) that the synthetic
// Device/Configuration descriptors are built from.
type hidBridgeInfo struct {
	vid, pid       uint16
	bcdDevice      uint16
	manufacturer   string
	product        string
	serial         string
	reportDesc     []byte
	maxInputReport int
}

// HIDDeviceSummary is one entry from ListAllHIDDevices. ID is what
// disambiguates one interface of a composite HID device sharing a VID:PID
// with its siblings (see findHIDDeviceByEntryID's doc comment) -- it is
// threaded through as ExportedDevice.InstanceID's suffix and read back by
// TryClaimGousb.
type HIDDeviceSummary struct {
	ID        uint64
	Name      string
	VID       uint16
	PID       uint16
	UsagePage uint16
	Usage     uint16
}

// ListAllHIDDevices enumerates every connected HID device this bridge could
// claim -- see enumerateAllHIDDevices' doc comment for the built-in/no-VID
// exclusions. This is the darwin USB-passthrough device list's enumeration
// step (list_hid_darwin.go); listing here does not open or claim anything,
// so a device that later fails to describe/claim in TryClaimGousb (e.g. a
// composite device whose HID interface has no report descriptor) just
// surfaces that error at attach time.
func ListAllHIDDevices() []HIDDeviceSummary {
	const maxDevices = 32
	ids := make([]C.uint64_t, maxDevices)
	names := make([]*C.char, maxDevices)
	vids := make([]C.int, maxDevices)
	pids := make([]C.int, maxDevices)
	usagePages := make([]C.int, maxDevices)
	usages := make([]C.int, maxDevices)

	count := int(C.enumerateAllHIDDevices(&ids[0], &names[0], &vids[0], &pids[0], &usagePages[0], &usages[0], C.int(maxDevices)))

	result := make([]HIDDeviceSummary, 0, count)
	for i := 0; i < count; i++ {
		name := C.GoString(names[i])
		C.free(unsafe.Pointer(names[i]))
		result = append(result, HIDDeviceSummary{
			ID:        uint64(ids[i]),
			Name:      name,
			UsagePage: uint16(usagePages[i]),
			Usage:     uint16(usages[i]),
			VID:       uint16(vids[i]),
			PID:       uint16(pids[i]),
		})
	}
	return result
}

// hidTarget identifies which HID device to open. entryID, when nonzero,
// picks one exact interface via findHIDDeviceByEntryID -- needed because
// vid:pid alone is ambiguous for a composite device that exposes several
// HID interfaces under the same VID:PID (see findHIDDeviceByEntryID's doc
// comment). entryID is zero for callers that only ever had vid:pid to go on
// (cmd/hidbridgetest, cmd/hiddescprobe), where first-match is the best
// available and matches this bridge's original behavior.
type hidTarget struct {
	vid, pid uint16
	entryID  uint64
}

func (t hidTarget) find() C.IOHIDDeviceRef {
	if t.entryID != 0 {
		return C.findHIDDeviceByEntryID(C.uint64_t(t.entryID))
	}
	return C.findHIDDeviceByVIDPID(C.int(t.vid), C.int(t.pid))
}

func describeHIDDevice(t hidTarget) (*hidBridgeInfo, error) {
	dev := t.find()
	if C.isNullDevice(dev) != 0 {
		return nil, fmt.Errorf("hidbridge: no HID device %04x:%04x (entry %d)", t.vid, t.pid, t.entryID)
	}
	defer C.releaseHIDDevice(dev)

	info := &hidBridgeInfo{
		vid:            t.vid,
		pid:            t.pid,
		bcdDevice:      uint16(C.getVersionNumber(dev)),
		maxInputReport: int(C.getMaxInputReportSize(dev)),
	}

	var nbuf [256]byte
	if n := C.getManufacturerString(dev, (*C.char)(unsafe.Pointer(&nbuf[0])), C.int(len(nbuf))); n > 0 {
		info.manufacturer = string(nbuf[:n])
	}
	if n := C.getProductString(dev, (*C.char)(unsafe.Pointer(&nbuf[0])), C.int(len(nbuf))); n > 0 {
		info.product = string(nbuf[:n])
	}
	if n := C.getSerialNumberString(dev, (*C.char)(unsafe.Pointer(&nbuf[0])), C.int(len(nbuf))); n > 0 {
		info.serial = string(nbuf[:n])
	}

	descBuf := make([]byte, 2048)
	n := int(C.getReportDescriptor(dev, (*C.uint8_t)(unsafe.Pointer(&descBuf[0])), C.int(len(descBuf))))
	if n > len(descBuf) {
		descBuf = make([]byte, n)
		n = int(C.getReportDescriptor(dev, (*C.uint8_t)(unsafe.Pointer(&descBuf[0])), C.int(len(descBuf))))
	}
	if n <= 0 {
		return nil, fmt.Errorf("hidbridge: %04x:%04x has no HID report descriptor", t.vid, t.pid)
	}
	info.reportDesc = append([]byte(nil), descBuf[:n]...)
	return info, nil
}

// --- capture ---

var (
	hidBridgeCallbacksMu sync.Mutex
	hidBridgeCallbacks   = map[uint64]func([]byte){}
	hidBridgeNextToken   uint64
)

//export goHIDBridgeReport
func goHIDBridgeReport(token C.uint64_t, report *C.uint8_t, length C.int) {
	hidBridgeCallbacksMu.Lock()
	cb := hidBridgeCallbacks[uint64(token)]
	hidBridgeCallbacksMu.Unlock()
	if cb == nil || length <= 0 {
		return
	}
	buf := C.GoBytes(unsafe.Pointer(report), length)
	cb(buf)
}

type hidBridgeCapture struct {
	stop chan struct{}
	done chan struct{}
}

func startHIDBridgeCapture(t hidTarget, maxReportSize int, onReport func([]byte)) (*hidBridgeCapture, error) {
	hidBridgeCallbacksMu.Lock()
	hidBridgeNextToken++
	token := hidBridgeNextToken
	hidBridgeCallbacks[token] = onReport
	hidBridgeCallbacksMu.Unlock()

	cap := &hidBridgeCapture{stop: make(chan struct{}), done: make(chan struct{})}
	openErr := make(chan error, 1)

	go func() {
		// See openHIDBridge's doc comment: open (schedule) and pump must run
		// on the same OS thread, so this whole goroutine is locked to one.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		dev := t.find()
		if C.isNullDevice(dev) != 0 {
			hidBridgeCallbacksMu.Lock()
			delete(hidBridgeCallbacks, token)
			hidBridgeCallbacksMu.Unlock()
			openErr <- fmt.Errorf("hidbridge: device %04x:%04x (entry %d) disappeared before capture start", t.vid, t.pid, t.entryID)
			close(cap.done)
			return
		}
		ctx := C.openHIDBridge(dev, C.uint64_t(token), C.int(maxReportSize))
		if ctx == nil {
			C.releaseHIDDevice(dev)
			hidBridgeCallbacksMu.Lock()
			delete(hidBridgeCallbacks, token)
			hidBridgeCallbacksMu.Unlock()
			openErr <- fmt.Errorf("hidbridge: failed to open HID device %04x:%04x (entry %d)", t.vid, t.pid, t.entryID)
			close(cap.done)
			return
		}
		openErr <- nil
		defer func() {
			C.closeHIDBridge(ctx)
			hidBridgeCallbacksMu.Lock()
			delete(hidBridgeCallbacks, token)
			hidBridgeCallbacksMu.Unlock()
			close(cap.done)
		}()
		for {
			select {
			case <-cap.stop:
				return
			default:
				C.runHIDBridgeRunLoop()
			}
		}
	}()

	if err := <-openErr; err != nil {
		return nil, err
	}
	return cap, nil
}

func (c *hidBridgeCapture) Stop() {
	close(c.stop)
	<-c.done
}

// --- synthetic descriptors ---

// hidDeviceDesc builds a standard USB Device Descriptor using the device's
// own real VID/PID/bcdDevice -- not placeholders -- so an agent's PnP
// hardware-ID match against an installed vendor driver package (e.g.
// Wacom's Windows driver, matching on USB\VID_056A&PID_0374) succeeds
// exactly as it would for the physical device on that machine's own bus.
// bDeviceClass/SubClass/Protocol are 0 (defined at the interface, standard
// for a HID-class device) -- confirmed against this project's own real
// Wacom Intuos S via ioreg.
func hidDeviceDesc(vid, pid, bcdDevice uint16) []byte {
	d := []byte{
		18, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 64,
		0, 0, 0, 0, 0, 0, 1, 2, 3, 1,
	}
	binary.LittleEndian.PutUint16(d[8:10], vid)
	binary.LittleEndian.PutUint16(d[10:12], pid)
	binary.LittleEndian.PutUint16(d[12:14], bcdDevice)
	return d
}

// hidConfigDesc builds a single-interface HID configuration descriptor
// (Configuration + Interface + HID + Endpoint), with the HID descriptor's
// wDescriptorLength set to reportDescLen -- the real report descriptor's
// exact length, so it matches what GET_DESCRIPTOR(REPORT) actually returns
// (see hidBridgeBackend.HandleControl).
func hidConfigDesc(reportDescLen int, endpointAddr uint8, maxPacketSize uint16, interval uint8) []byte {
	const total = 9 + 9 + 9 + 7
	d := []byte{
		9, 0x02, 0, 0, 1, 1, 0, 0x80, 50, // configuration, wTotalLength at [2:4]
		9, 0x04, 0, 0, 1, 0x03, 0x00, 0x00, 0, // interface, class=HID
		9, 0x21, 0x11, 0x01, 0, 1, 0x22, 0, 0, // HID desc, wDescriptorLength at [25:27]
		7, 0x05, 0, 0x03, 0, 0, interval, // endpoint (interrupt), addr at [29], wMaxPacketSize at [31:33]
	}
	binary.LittleEndian.PutUint16(d[2:4], total)
	binary.LittleEndian.PutUint16(d[25:27], uint16(reportDescLen))
	d[29] = endpointAddr
	binary.LittleEndian.PutUint16(d[31:33], maxPacketSize)
	return d
}

const (
	hidBridgeEndpointAddr  = 0x81
	hidBridgeMaxPacketSize = 64
	hidBridgeInterval      = 4
)

// --- DeviceBackend ---

type hidBridgeBackend struct {
	deviceDesc []byte
	configDesc []byte
	reportDesc []byte

	capture *hidBridgeCapture

	mu      sync.Mutex
	pending chan []byte
}

// hidEntryIDFromInstanceID extracts the registry-entry-ID suffix
// list_hid_darwin.go embeds in InstanceID (format
// "HID\VID_XXXX&PID_YYYY\<entryID>"), or 0 if there's no suffix -- e.g.
// InstanceID is empty because dev came from NewExportedFromVIDPID directly
// (cmd/hidbridgetest, cmd/hiddescprobe) rather than through the
// passthrough device list.
func hidEntryIDFromInstanceID(instanceID string) uint64 {
	i := strings.LastIndexByte(instanceID, '\\')
	if i < 0 {
		return 0
	}
	id, err := strconv.ParseUint(instanceID[i+1:], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// TryClaimGousb is macOS's stand-in for backend_gousb.go's libusb claim (see
// backend_gousb.go's `!darwin` build tag) -- same call site in session.go,
// no caller changes needed. Named to match rather than adding a
// platform-specific call site to session.go.
func TryClaimGousb(dev *ExportedDevice) error {
	target := hidTarget{vid: dev.VID, pid: dev.PID, entryID: hidEntryIDFromInstanceID(dev.InstanceID)}
	info, err := describeHIDDevice(target)
	if err != nil {
		return err
	}

	backend := &hidBridgeBackend{
		deviceDesc: hidDeviceDesc(info.vid, info.pid, info.bcdDevice),
		configDesc: hidConfigDesc(len(info.reportDesc), hidBridgeEndpointAddr, hidBridgeMaxPacketSize, hidBridgeInterval),
		reportDesc: info.reportDesc,
		pending:    make(chan []byte, 8),
	}
	capture, err := startHIDBridgeCapture(target, info.maxInputReport, backend.onReport)
	if err != nil {
		return err
	}
	backend.capture = capture

	dev.Backend = backend
	dev.DeviceDesc = backend.deviceDesc
	dev.ConfigDesc = backend.configDesc
	dev.Class, dev.SubClass, dev.Protocol = 0, 0, 0
	dev.ConfigVal = 1
	dev.NumConfigs = 1
	dev.BCDDevice = info.bcdDevice
	dev.Interfaces = [][3]uint8{{0x03, 0x00, 0x00}}
	dev.Speed = 2 // FULL -- matches every Wacom consumer tablet this project has seen (USBSpeed=1/full-speed via ioreg)

	logrus.Infof("usbpass: hidbridge claimed %04x:%04x %q (report descriptor %d bytes)", info.vid, info.pid, info.product, len(info.reportDesc))
	return nil
}

func (b *hidBridgeBackend) onReport(report []byte) {
	select {
	case b.pending <- report:
	default:
		// Queue full -- drop the oldest sample and push the newest, so a
		// slow-polling importer still sees fresh pointer/pressure data
		// instead of a growing backlog of stale ones.
		select {
		case <-b.pending:
		default:
		}
		select {
		case b.pending <- report:
		default:
		}
	}
}

func (b *hidBridgeBackend) HandleControl(_ context.Context, setup [8]byte, wLength int, _ []byte) (int32, []byte) {
	bm := setup[0]
	req := setup[1]
	wValue := binary.LittleEndian.Uint16(setup[2:4])

	if bm == 0x80 && req == 0x06 { // standard GET_DESCRIPTOR (device recipient)
		return b.getDescriptor(uint8(wValue>>8), uint8(wValue), wLength)
	}
	if bm == 0x81 && req == 0x06 { // standard GET_DESCRIPTOR (interface recipient) -- HID/Report
		return b.getDescriptor(uint8(wValue>>8), uint8(wValue), wLength)
	}
	switch {
	case req == 0x05, req == 0x01, req == 0x09, req == 0x0b: // SET_ADDRESS/CLEAR_FEATURE/SET_CONFIGURATION/SET_INTERFACE
		return 0, nil
	case req == 0x00: // GET_STATUS
		n := 2
		if wLength < n {
			n = wLength
		}
		return 0, make([]byte, n)
	}
	// HID class-specific requests (bmRequestType 0x21 host->device, 0xA1 device->host).
	switch bm {
	case 0x21: // SET_IDLE / SET_PROTOCOL / SET_REPORT -- acked, not forwarded (see file doc comment)
		return 0, nil
	case 0xA1: // GET_IDLE / GET_PROTOCOL / GET_REPORT
		n := wLength
		if n > 1 {
			n = 1
		}
		return 0, make([]byte, n)
	}
	return errnoEPIPE, nil
}

func (b *hidBridgeBackend) getDescriptor(descType, descIndex uint8, wLength int) (int32, []byte) {
	var src []byte
	switch descType {
	case 0x01:
		src = b.deviceDesc
	case 0x02:
		src = b.configDesc
	case 0x21:
		// HID descriptor alone (bytes [18:27) of configDesc) -- rarely
		// queried directly (usually only embedded in the config descriptor
		// above), served for completeness.
		if len(b.configDesc) >= 27 {
			src = b.configDesc[18:27]
		}
	case 0x22:
		src = b.reportDesc
	case 0x03:
		if descIndex == 0 {
			src = []byte{4, 0x03, 0x09, 0x04}
		} else {
			src = []byte{2, 0x03}
		}
	}
	if src == nil {
		return errnoEPIPE, nil
	}
	if wLength < len(src) {
		src = src[:wLength]
	}
	return 0, append([]byte(nil), src...)
}

func (b *hidBridgeBackend) HandleBulk(ctx context.Context, ep uint8, dirIn bool, length int, _ []byte) (int32, []byte) {
	if !dirIn || ep&0x7f != hidBridgeEndpointAddr&0x7f {
		return errnoEPIPE, nil
	}
	select {
	case report := <-b.pending:
		if len(report) > length {
			report = report[:length]
		}
		return 0, report
	case <-ctx.Done():
		return errnoEPIPE, nil
	}
}

func (b *hidBridgeBackend) Close() error {
	if b.capture != nil {
		b.capture.Stop()
	}
	return nil
}
