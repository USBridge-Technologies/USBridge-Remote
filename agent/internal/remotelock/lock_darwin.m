//go:build darwin

#import <ApplicationServices/ApplicationServices.h>
#import <Cocoa/Cocoa.h>
#include <stdint.h>
#include <unistd.h>

static volatile int gArmed;
static CFMachPortRef gTap;
static CFRunLoopSourceRef gSrc;

void usbridgeRemoteLockSetArmed(int on) {
	gArmed = on ? 1 : 0;
}

static int eventIsInjected(CGEventRef event) {
	// Hardware at a session tap is CombinedSessionState with PID 0.
	// Sunshine / libvirtualhid mouse is HIDSystemState posted at the HID
	// tap, which also reports PID 0 — so PID alone is not enough. Keyboard
	// uses a private source.
	int64_t pid = CGEventGetIntegerValueField(event, kCGEventSourceUnixProcessID);
	if (pid != 0) {
		return 1;
	}
	int64_t state = CGEventGetIntegerValueField(event, kCGEventSourceStateID);
	return state == (int64_t)kCGEventSourceStatePrivate
		|| state == (int64_t)kCGEventSourceStateHIDSystemState;
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
	(void)refcon;
	if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
		if (gTap != NULL) {
			CGEventTapEnable(gTap, true);
		}
		return event;
	}
	if (!gArmed) {
		return event;
	}
	if (!eventIsInjected(event)) {
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

static CFMachPortRef createTap(CGEventTapLocation loc, CGEventMask mask) {
	return CGEventTapCreate(
		loc,
		kCGHeadInsertEventTap,
		kCGEventTapOptionDefault,
		mask,
		usbridgeRemoteLockCallback,
		NULL);
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
	if (gTap != NULL) {
		return 1;
	}
	CGEventMask mask = usbridgeRemoteLockMask();
	// Session, not HID: Sunshine posts mouse at kCGHIDEventTap with
	// HIDSystemState. Those events can skip a HID-level tap, and
	// kCGEventSourceUnixProcessID is not annotated there.
	gTap = createTap(kCGSessionEventTap, mask);
	if (gTap == NULL && !AXIsProcessTrusted()) {
		promptAccessibility();
		gTap = createTap(kCGSessionEventTap, mask);
	}
	if (gTap == NULL) {
		return 0;
	}
	gSrc = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, gTap, 0);
	if (gSrc == NULL) {
		CFRelease(gTap);
		gTap = NULL;
		return 0;
	}
	CFRunLoopAddSource(CFRunLoopGetMain(), gSrc, kCFRunLoopCommonModes);
	CGEventTapEnable(gTap, true);
	return 1;
}

void usbridgeRemoteLockRemove(void) {
	if (gTap != NULL) {
		CGEventTapEnable(gTap, false);
	}
	if (gSrc != NULL) {
		CFRunLoopRemoveSource(CFRunLoopGetMain(), gSrc, kCFRunLoopCommonModes);
		CFRelease(gSrc);
		gSrc = NULL;
	}
	if (gTap != NULL) {
		CFRelease(gTap);
		gTap = NULL;
	}
}
