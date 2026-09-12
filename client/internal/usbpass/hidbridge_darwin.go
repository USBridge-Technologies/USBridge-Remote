//go:build darwin

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

func describeHIDDevice(vid, pid uint16) (*hidBridgeInfo, error) {
	dev := C.findHIDDeviceByVIDPID(C.int(vid), C.int(pid))
	if C.isNullDevice(dev) != 0 {
		return nil, fmt.Errorf("hidbridge: no HID device %04x:%04x", vid, pid)
	}
	defer C.releaseHIDDevice(dev)

	info := &hidBridgeInfo{
		vid:            vid,
		pid:            pid,
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
		return nil, fmt.Errorf("hidbridge: %04x:%04x has no HID report descriptor", vid, pid)
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

func startHIDBridgeCapture(vid, pid uint16, maxReportSize int, onReport func([]byte)) (*hidBridgeCapture, error) {
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

		dev := C.findHIDDeviceByVIDPID(C.int(vid), C.int(pid))
		if C.isNullDevice(dev) != 0 {
			hidBridgeCallbacksMu.Lock()
			delete(hidBridgeCallbacks, token)
			hidBridgeCallbacksMu.Unlock()
			openErr <- fmt.Errorf("hidbridge: device %04x:%04x disappeared before capture start", vid, pid)
			close(cap.done)
			return
		}
		ctx := C.openHIDBridge(dev, C.uint64_t(token), C.int(maxReportSize))
		if ctx == nil {
			C.releaseHIDDevice(dev)
			hidBridgeCallbacksMu.Lock()
			delete(hidBridgeCallbacks, token)
			hidBridgeCallbacksMu.Unlock()
			openErr <- fmt.Errorf("hidbridge: failed to open HID device %04x:%04x", vid, pid)
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

// TryClaimGousb is macOS's stand-in for backend_gousb.go's libusb claim (see
// backend_gousb.go's `!darwin` build tag) -- same call site in session.go,
// no caller changes needed. Named to match rather than adding a
// platform-specific call site to session.go.
func TryClaimGousb(dev *ExportedDevice) error {
	info, err := describeHIDDevice(dev.VID, dev.PID)
	if err != nil {
		return err
	}

	backend := &hidBridgeBackend{
		deviceDesc: hidDeviceDesc(info.vid, info.pid, info.bcdDevice),
		configDesc: hidConfigDesc(len(info.reportDesc), hidBridgeEndpointAddr, hidBridgeMaxPacketSize, hidBridgeInterval),
		reportDesc: info.reportDesc,
		pending:    make(chan []byte, 8),
	}
	capture, err := startHIDBridgeCapture(info.vid, info.pid, info.maxInputReport, backend.onReport)
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

func (b *hidBridgeBackend) HandleControl(_ context.Context, setup [8]byte, wLength int) (int32, []byte) {
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
