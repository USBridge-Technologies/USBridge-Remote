//go:build darwin

#import <ApplicationServices/ApplicationServices.h>
#import <Cocoa/Cocoa.h>
#include <dlfcn.h>
#include <stdint.h>
#include <unistd.h>

static volatile int gArmed;
static CFMachPortRef gTapSession;
static CFMachPortRef gTapHID;
static CFRunLoopSourceRef gSrcSession;
static CFRunLoopSourceRef gSrcHID;

// Real trackpad/mouse clicks still carry an IOHIDEvent at the HID tap.
// Sunshine / USBridge-streamer mouse is CGEventPost with HIDSystemState
// and PID 0. Do not treat HIDSystemState alone as injected: local clicks
// use that state too and freezing the window is worse than a leak.
// CGEventCopyIOHIDEvent is not in the public SDK — resolve at runtime.
typedef CFTypeRef (*copyIOHIDEventFn)(CGEventRef);
static copyIOHIDEventFn gCopyIOHIDEvent;
static dispatch_once_t gCopyIOHIDOnce;

static copyIOHIDEventFn copyIOHIDEvent(void) {
	dispatch_once(&gCopyIOHIDOnce, ^{
		gCopyIOHIDEvent = (copyIOHIDEventFn)dlsym(RTLD_DEFAULT, "CGEventCopyIOHIDEvent");
	});
	return gCopyIOHIDEvent;
}

void usbridgeRemoteLockSetArmed(int on) {
	gArmed = on ? 1 : 0;
}

static int eventIsInjected(CGEventRef event, int hidLevel) {
	int64_t pid = CGEventGetIntegerValueField(event, kCGEventSourceUnixProcessID);
	if (pid != 0) {
		return 1;
	}
	int64_t state = CGEventGetIntegerValueField(event, kCGEventSourceStateID);
	if (state == (int64_t)kCGEventSourceStatePrivate) {
		return 1;
	}
	if (!hidLevel) {
		return 0;
	}
	copyIOHIDEventFn copyHID = copyIOHIDEvent();
	if (copyHID == NULL) {
		return 0;
	}
	CFTypeRef hid = copyHID(event);
	if (hid != NULL) {
		CFRelease(hid);
		return 0;
	}
	return 1;
}

static int isMouseButtonOrWheel(CGEventType type) {
	switch (type) {
	case kCGEventLeftMouseDown:
	case kCGEventLeftMouseUp:
	case kCGEventRightMouseDown:
	case kCGEventRightMouseUp:
	case kCGEventOtherMouseDown:
	case kCGEventOtherMouseUp:
	case kCGEventScrollWheel:
		return 1;
	default:
		return 0;
	}
}

static int isKeyEvent(CGEventType type) {
	return type == kCGEventKeyDown || type == kCGEventKeyUp || type == kCGEventFlagsChanged;
}

static NSPoint cocoaPoint(CGEventRef event) {
	CGPoint p = CGEventGetUnflippedLocation(event);
	return NSMakePoint(p.x, p.y);
}

static int cgWindowOwnedByUs(CGWindowID wid) {
	if (wid == kCGNullWindowID) {
		return 0;
	}
	CFArrayRef arr = CGWindowListCopyWindowInfo(kCGWindowListOptionIncludingWindow, wid);
	if (arr == NULL) {
		return 0;
	}
	int ours = 0;
	if (CFArrayGetCount(arr) > 0) {
		CFDictionaryRef d = (CFDictionaryRef)CFArrayGetValueAtIndex(arr, 0);
		CFNumberRef n = (CFNumberRef)CFDictionaryGetValue(d, kCGWindowOwnerPID);
		int owner = 0;
		if (n != NULL) {
			CFNumberGetValue(n, kCFNumberIntType, &owner);
		}
		ours = (owner == (int)getpid()) ? 1 : 0;
	}
	CFRelease(arr);
	return ours;
}

static int pointInOurWindowFrames(NSPoint pt) {
	for (NSWindow *w in [NSApp windows]) {
		if (![w isVisible]) {
			continue;
		}
		if (NSPointInRect(pt, [w frame])) {
			return 1;
		}
	}
	return 0;
}

static int windowAtPointIsOurs(NSPoint pt) {
	@autoreleasepool {
		NSInteger num = [NSWindow windowNumberAtPoint:pt belowWindowWithWindowNumber:0];
		if (num != 0) {
			if ([NSApp windowWithWindowNumber:num] != nil) {
				return 1;
			}
			if (cgWindowOwnedByUs((CGWindowID)num)) {
				return 1;
			}
		}
		return pointInOurWindowFrames(pt);
	}
}

static int overMinButton(NSPoint pt) {
	@autoreleasepool {
		NSInteger num = [NSWindow windowNumberAtPoint:pt belowWindowWithWindowNumber:0];
		NSWindow *w = [NSApp windowWithWindowNumber:num];
		if (w == nil) {
			return 0;
		}
		NSButton *btn = [w standardWindowButton:NSWindowMiniaturizeButton];
		if (btn == nil || [btn isHidden]) {
			return 0;
		}
		NSRect b = [btn convertRect:[btn bounds] toView:nil];
		NSRect scr = [w convertRectToScreen:b];
		return NSPointInRect(pt, scr) ? 1 : 0;
	}
}

static int appIsFrontmost(void) {
	@autoreleasepool {
		return [NSApp isActive] ? 1 : 0;
	}
}

static CGEventRef usbridgeRemoteLockCallback(
	CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *refcon)
{
	(void)proxy;
	int hidLevel = refcon != NULL;
	if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
		if (gTapSession != NULL) {
			CGEventTapEnable(gTapSession, true);
		}
		if (gTapHID != NULL) {
			CGEventTapEnable(gTapHID, true);
		}
		return event;
	}
	if (!gArmed) {
		return event;
	}
	if (!eventIsInjected(event, hidLevel)) {
		return event;
	}
	if (isMouseButtonOrWheel(type)) {
		NSPoint pt = cocoaPoint(event);
		if (windowAtPointIsOurs(pt) && !overMinButton(pt)) {
			return NULL;
		}
		return event;
	}
	if (isKeyEvent(type) && appIsFrontmost()) {
		return NULL;
	}
	return event;
}

static CGEventMask usbridgeRemoteLockMask(void) {
	return CGEventMaskBit(kCGEventLeftMouseDown)
		| CGEventMaskBit(kCGEventRightMouseDown)
		| CGEventMaskBit(kCGEventOtherMouseDown)
		| CGEventMaskBit(kCGEventLeftMouseUp)
		| CGEventMaskBit(kCGEventRightMouseUp)
		| CGEventMaskBit(kCGEventOtherMouseUp)
		| CGEventMaskBit(kCGEventScrollWheel)
		| CGEventMaskBit(kCGEventKeyDown)
		| CGEventMaskBit(kCGEventKeyUp)
		| CGEventMaskBit(kCGEventFlagsChanged);
}

static CFMachPortRef createTap(CGEventTapLocation loc, CGEventMask mask, void *refcon) {
	return CGEventTapCreate(
		loc,
		kCGHeadInsertEventTap,
		kCGEventTapOptionDefault,
		mask,
		usbridgeRemoteLockCallback,
		refcon);
}

static int addTapToLoop(CFMachPortRef tap, CFRunLoopSourceRef *outSrc) {
	*outSrc = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0);
	if (*outSrc == NULL) {
		return 0;
	}
	CFRunLoopAddSource(CFRunLoopGetMain(), *outSrc, kCFRunLoopCommonModes);
	CGEventTapEnable(tap, true);
	return 1;
}

static void dropTap(CFMachPortRef *tap, CFRunLoopSourceRef *src) {
	if (*tap != NULL) {
		CGEventTapEnable(*tap, false);
	}
	if (*src != NULL) {
		CFRunLoopRemoveSource(CFRunLoopGetMain(), *src, kCFRunLoopCommonModes);
		CFRelease(*src);
		*src = NULL;
	}
	if (*tap != NULL) {
		CFRelease(*tap);
		*tap = NULL;
	}
}

static void promptAccessibility(void) {
	const void *keys[] = {kAXTrustedCheckOptionPrompt};
	const void *values[] = {kCFBooleanTrue};
	CFDictionaryRef options = CFDictionaryCreate(
		kCFAllocatorDefault,
		keys,
		values,
		1,
		&kCFCopyStringDictionaryKeyCallBacks,
		&kCFTypeDictionaryValueCallBacks);
	if (options == NULL) {
		return;
	}
	(void)AXIsProcessTrustedWithOptions(options);
	CFRelease(options);
}

int usbridgeRemoteLockInstall(void) {
	if (gTapSession != NULL) {
		return 1;
	}
	CGEventMask mask = usbridgeRemoteLockMask();
	gTapSession = createTap(kCGSessionEventTap, mask, NULL);
	if (gTapSession == NULL && !AXIsProcessTrusted()) {
		promptAccessibility();
		gTapSession = createTap(kCGSessionEventTap, mask, NULL);
	}
	if (gTapSession == NULL || !addTapToLoop(gTapSession, &gSrcSession)) {
		dropTap(&gTapSession, &gSrcSession);
		return 0;
	}
	// HID tap is extra: IOHIDEvent is only meaningful here. Sunshine posts
	// can skip it, which is why the session tap stays primary.
	gTapHID = createTap(kCGHIDEventTap, mask, (void *)1);
	if (gTapHID != NULL && !addTapToLoop(gTapHID, &gSrcHID)) {
		dropTap(&gTapHID, &gSrcHID);
	}
	return 1;
}

void usbridgeRemoteLockRemove(void) {
	dropTap(&gTapHID, &gSrcHID);
	dropTap(&gTapSession, &gSrcSession);
}
