//go:build darwin && !ios

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation

#include <IOKit/hid/IOHIDManager.h>
#include <IOKit/hid/IOHIDQueue.h>
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

// Moonlight button flags
#define LI_DPAD_UP    0x0001
#define LI_DPAD_DOWN  0x0002
#define LI_DPAD_LEFT  0x0004
#define LI_DPAD_RIGHT 0x0008
#define LI_START      0x0010
#define LI_BACK       0x0020
#define LI_LS         0x0040
#define LI_RS         0x0080
#define LI_LB         0x0100
#define LI_RB         0x0200
#define LI_GUIDE      0x0400
#define LI_A          0x1000
#define LI_B          0x2000
#define LI_X          0x4000
#define LI_Y          0x8000

// Standard button mapping (USB HID Button Page, button 1-11 → Moonlight flags)
static const uint16_t kButtonMap[11] = {
    LI_A, LI_B, LI_X, LI_Y,
    LI_LB, LI_RB,
    LI_BACK, LI_START,
    LI_LS, LI_RS,
    LI_GUIDE,
};

typedef struct {
    IOHIDDeviceRef device;
    IOHIDQueueRef  queue;
    uint16_t       buttons;
    int16_t        leftX, leftY, rightX, rightY;
    uint8_t        leftTrigger, rightTrigger;
} HIDCapture;

static int16_t scaleAxis(CFIndex val, CFIndex lo, CFIndex hi) {
    if (hi <= lo) return 0;
    CFIndex mid = (hi + lo) / 2;
    CFIndex half = (hi - lo) / 2;
    if (half == 0) return 0;
    int32_t r = (int32_t)(((val - mid) * 32767) / half);
    if (r >  32767) r =  32767;
    if (r < -32767) r = -32767;
    return (int16_t)r;
}

static uint8_t scaleTrigger(CFIndex val, CFIndex lo, CFIndex hi) {
    if (hi <= lo) return 0;
    int64_t r = ((val - lo) * 255) / (hi - lo);
    if (r < 0) r = 0;
    if (r > 255) r = 255;
    return (uint8_t)r;
}

static uint16_t hatToDpad(CFIndex hat) {
    switch (hat) {
        case 0: return LI_DPAD_UP;
        case 1: return LI_DPAD_UP  | LI_DPAD_RIGHT;
        case 2: return LI_DPAD_RIGHT;
        case 3: return LI_DPAD_DOWN | LI_DPAD_RIGHT;
        case 4: return LI_DPAD_DOWN;
        case 5: return LI_DPAD_DOWN | LI_DPAD_LEFT;
        case 6: return LI_DPAD_LEFT;
        case 7: return LI_DPAD_UP  | LI_DPAD_LEFT;
        default: return 0;
    }
}

static void processValue(HIDCapture* cap, IOHIDValueRef value) {
    IOHIDElementRef elem  = IOHIDValueGetElement(value);
    uint32_t page  = IOHIDElementGetUsagePage(elem);
    uint32_t usage = IOHIDElementGetUsage(elem);
    CFIndex  ival  = IOHIDValueGetIntegerValue(value);
    CFIndex  lo    = IOHIDElementGetLogicalMin(elem);
    CFIndex  hi    = IOHIDElementGetLogicalMax(elem);

    if (page == kHIDPage_Button) {
        uint32_t idx = usage - 1; // 0-indexed
        if (idx < 11) {
            if (ival) cap->buttons |= kButtonMap[idx];
            else      cap->buttons &= ~kButtonMap[idx];
        }
        return;
    }

    if (page == kHIDPage_GenericDesktop) {
        switch (usage) {
            case kHIDUsage_GD_X:
                cap->leftX = scaleAxis(ival, lo, hi); break;
            case kHIDUsage_GD_Y:
                cap->leftY = scaleAxis(ival, lo, hi); break;
            case kHIDUsage_GD_Z:
                cap->leftTrigger = scaleTrigger(ival, lo, hi); break;
            case kHIDUsage_GD_Rx:
                cap->rightX = scaleAxis(ival, lo, hi); break;
            case kHIDUsage_GD_Ry:
                cap->rightY = scaleAxis(ival, lo, hi); break;
            case kHIDUsage_GD_Rz:
                cap->rightTrigger = scaleTrigger(ival, lo, hi); break;
            case kHIDUsage_GD_Hatswitch: {
                uint16_t dpad = hatToDpad(ival);
                cap->buttons = (cap->buttons & 0xFFF0) | dpad;
                break;
            }
        }
    }
}

// openCapture finds the gamepad by its IOKit registry entry ID, retains it, and
// opens an HID queue. Using a registry entry ID (instead of a raw IOHIDDeviceRef
// pointer) avoids dangling-pointer crashes when the enumerating IOHIDManager has
// been closed and released before capture starts.
static HIDCapture* openCapture(uint64_t registryEntryID) {
    // Create a temporary manager to locate the device by its stable registry ID.
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return NULL;

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
        CFDictionarySetValue(m, CFSTR(kIOHIDDeviceUsageKey), uRef);
        CFRelease(upRef); CFRelease(uRef);
        CFArrayAppendValue(matchers, m);
        CFRelease(m);
    }
    IOHIDManagerSetDeviceMatchingMultiple(mgr, matchers);
    CFRelease(matchers);
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
                CFRetain(dev); // keep alive after manager is released
                break;
            }
        }
        free(devs);
        CFRelease(devices);
    }
    IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
    CFRelease(mgr); // manager gone, dev survives via our CFRetain

    if (!dev) return NULL;

    IOReturn ret = IOHIDDeviceOpen(dev, kIOHIDOptionsTypeNone);
    if (ret != kIOReturnSuccess) {
        CFRelease(dev);
        return NULL;
    }

    IOHIDQueueRef queue = IOHIDQueueCreate(kCFAllocatorDefault, dev, 64, kIOHIDOptionsTypeNone);
    if (!queue) {
        IOHIDDeviceClose(dev, kIOHIDOptionsTypeNone);
        CFRelease(dev);
        return NULL;
    }

    CFArrayRef elems = IOHIDDeviceCopyMatchingElements(dev, NULL, kIOHIDOptionsTypeNone);
    if (elems) {
        CFIndex n = CFArrayGetCount(elems);
        for (CFIndex i = 0; i < n; i++) {
            IOHIDElementRef elem = (IOHIDElementRef)CFArrayGetValueAtIndex(elems, i);
            IOHIDQueueAddElement(queue, elem);
        }
        CFRelease(elems);
    }

    IOHIDQueueStart(queue);

    HIDCapture* cap = (HIDCapture*)calloc(1, sizeof(HIDCapture));
    cap->device = dev;
    cap->queue  = queue;
    return cap;
}

// pollCapture drains the HID queue and writes the latest accumulated state into cap.
static void pollCapture(HIDCapture* cap) {
    if (!cap || !cap->queue) return;
    IOHIDValueRef value;
    while ((value = IOHIDQueueCopyNextValueWithTimeout(cap->queue, 0)) != NULL) {
        processValue(cap, value);
        CFRelease(value);
    }
}

static void getState(HIDCapture* cap,
                     uint16_t* buttons,
                     int16_t* lx, int16_t* ly,
                     int16_t* rx, int16_t* ry,
                     uint8_t* lt, uint8_t* rt) {
    if (!cap) {
        *buttons = 0; *lx = *ly = *rx = *ry = 0; *lt = *rt = 0;
        return;
    }
    *buttons = cap->buttons;
    *lx = cap->leftX;  *ly = cap->leftY;
    *rx = cap->rightX; *ry = cap->rightY;
    *lt = cap->leftTrigger;
    *rt = cap->rightTrigger;
}

// closeCapture releases resources.
static void closeCapture(HIDCapture* cap) {
    if (!cap) return;
    if (cap->queue) {
        IOHIDQueueStop(cap->queue);
        CFRelease(cap->queue);
    }
    if (cap->device) {
        IOHIDDeviceClose(cap->device, kIOHIDOptionsTypeNone);
        CFRelease(cap->device); // balance the CFRetain in openCapture
    }
    free(cap);
}
*/
import "C"
import (
	"fmt"
	"time"
)

// GamepadCaptureState holds the current decoded state of a gamepad.
type GamepadCaptureState struct {
	Buttons                   uint16
	LeftX, LeftY              int16
	RightX, RightY            int16
	LeftTrigger, RightTrigger uint8
}

// GamepadCapture manages an active IOKit HID capture for one gamepad.
type GamepadCapture struct {
	cctx *C.HIDCapture
	stop chan struct{}
	done chan struct{}
}

// StartGamepadCapture opens the gamepad identified by deviceID and calls onState
// at ~60 Hz with the latest input state. Returns an error if the device cannot be opened.
func StartGamepadCapture(deviceID string, onState func(GamepadCaptureState)) (*GamepadCapture, error) {
	var id uint64
	if _, err := fmt.Sscanf(deviceID, "%d", &id); err != nil || id == 0 {
		return nil, fmt.Errorf("invalid gamepad device ID %q", deviceID)
	}

	cctx := C.openCapture(C.uint64_t(id))
	if cctx == nil {
		return nil, fmt.Errorf("failed to open IOKit HID device %s", deviceID)
	}

	cap := &GamepadCapture{
		cctx: cctx,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}

	go func() {
		defer func() {
			C.closeCapture(cap.cctx)
			close(cap.done)
		}()

		ticker := time.NewTicker(16 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-cap.stop:
				return
			case <-ticker.C:
				C.pollCapture(cctx)

				var buttons C.uint16_t
				var lx, ly, rx, ry C.int16_t
				var lt, rt C.uint8_t
				C.getState(cctx, &buttons, &lx, &ly, &rx, &ry, &lt, &rt)

				onState(GamepadCaptureState{
					Buttons:      uint16(buttons),
					LeftX:        int16(lx),
					LeftY:        int16(ly),
					RightX:       int16(rx),
					RightY:       int16(ry),
					LeftTrigger:  uint8(lt),
					RightTrigger: uint8(rt),
				})
			}
		}
	}()

	return cap, nil
}

// Stop halts the capture goroutine and closes the HID device.
func (c *GamepadCapture) Stop() {
	close(c.stop)
	<-c.done
}
