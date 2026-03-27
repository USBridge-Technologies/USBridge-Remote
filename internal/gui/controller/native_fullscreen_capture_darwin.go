//go:build darwin

package controller

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework ApplicationServices -framework AppKit

#include <stdbool.h>
#include <stdint.h>
#include <ApplicationServices/ApplicationServices.h>

extern void goNativeCaptureMove(int dx, int dy);
extern void goNativeCaptureButton(int button, int pressed);
extern void goNativeCaptureScroll(int delta);
extern void goNativeCaptureExit(void);

static CFMachPortRef usbridgeEventTap = NULL;
static CFRunLoopSourceRef usbridgeRunLoopSource = NULL;
static CFRunLoopRef usbridgeRunLoop = NULL;
static bool usbridgeCursorHidden = false;
static bool usbridgeCaptureActive = false;

static CGEventRef usbridgeEventTapCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *refcon) {
    (void)proxy;
    (void)refcon;

    if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
        if (usbridgeEventTap != NULL) {
            CGEventTapEnable(usbridgeEventTap, true);
        }
        return event;
    }

    if (!usbridgeCaptureActive) {
        return event;
    }

    switch (type) {
        case kCGEventMouseMoved:
        case kCGEventLeftMouseDragged:
        case kCGEventRightMouseDragged:
        case kCGEventOtherMouseDragged: {
            int dx = (int)CGEventGetIntegerValueField(event, kCGMouseEventDeltaX);
            int dy = (int)CGEventGetIntegerValueField(event, kCGMouseEventDeltaY);
            if (dx != 0 || dy != 0) {
                goNativeCaptureMove(dx, dy);
            }
            return NULL;
        }
        case kCGEventLeftMouseDown:
            goNativeCaptureButton(1, 1);
            return NULL;
        case kCGEventLeftMouseUp:
            goNativeCaptureButton(1, 0);
            return NULL;
        case kCGEventRightMouseDown:
            goNativeCaptureButton(2, 1);
            return NULL;
        case kCGEventRightMouseUp:
            goNativeCaptureButton(2, 0);
            return NULL;
        case kCGEventOtherMouseDown:
            goNativeCaptureButton(3, 1);
            return NULL;
        case kCGEventOtherMouseUp:
            goNativeCaptureButton(3, 0);
            return NULL;
        case kCGEventScrollWheel: {
            int delta = (int)CGEventGetIntegerValueField(event, kCGScrollWheelEventDeltaAxis1);
            if (delta != 0) {
                goNativeCaptureScroll(delta);
            }
            return NULL;
        }
        case kCGEventKeyDown: {
            CGKeyCode keycode = (CGKeyCode)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
            if (keycode == (CGKeyCode)53 || keycode == (CGKeyCode)103) {
                goNativeCaptureExit();
                return NULL;
            }
            return event;
        }
        default:
            return event;
    }
}

static bool usbridgeStartNativeCapture(void) {
    if (usbridgeEventTap != NULL) {
        return true;
    }

    CGEventMask mask =
        CGEventMaskBit(kCGEventMouseMoved) |
        CGEventMaskBit(kCGEventLeftMouseDragged) |
        CGEventMaskBit(kCGEventRightMouseDragged) |
        CGEventMaskBit(kCGEventOtherMouseDragged) |
        CGEventMaskBit(kCGEventLeftMouseDown) |
        CGEventMaskBit(kCGEventLeftMouseUp) |
        CGEventMaskBit(kCGEventRightMouseDown) |
        CGEventMaskBit(kCGEventRightMouseUp) |
        CGEventMaskBit(kCGEventOtherMouseDown) |
        CGEventMaskBit(kCGEventOtherMouseUp) |
        CGEventMaskBit(kCGEventScrollWheel) |
        CGEventMaskBit(kCGEventKeyDown);

    usbridgeEventTap = CGEventTapCreate(
        kCGSessionEventTap,
        kCGHeadInsertEventTap,
        kCGEventTapOptionDefault,
        mask,
        usbridgeEventTapCallback,
        NULL
    );
    if (usbridgeEventTap == NULL) {
        return false;
    }

    usbridgeRunLoop = CFRunLoopGetCurrent();
    CFRetain(usbridgeRunLoop);

    usbridgeRunLoopSource = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, usbridgeEventTap, 0);
    if (usbridgeRunLoopSource == NULL) {
        CFRelease(usbridgeEventTap);
        usbridgeEventTap = NULL;
        CFRelease(usbridgeRunLoop);
        usbridgeRunLoop = NULL;
        return false;
    }

    CFRunLoopAddSource(usbridgeRunLoop, usbridgeRunLoopSource, kCFRunLoopCommonModes);
    CGEventTapEnable(usbridgeEventTap, true);

    usbridgeCaptureActive = true;
    CGAssociateMouseAndMouseCursorPosition(false);
    CGRect bounds = CGDisplayBounds(kCGDirectMainDisplay);
    CGPoint center = CGPointMake(CGRectGetMidX(bounds), CGRectGetMidY(bounds));
    CGWarpMouseCursorPosition(center);
    CGDisplayHideCursor(kCGDirectMainDisplay);
    usbridgeCursorHidden = true;

    return true;
}

static void usbridgeRunNativeCaptureLoop(void) {
    CFRunLoopRun();

    usbridgeCaptureActive = false;

    if (usbridgeRunLoopSource != NULL && usbridgeRunLoop != NULL) {
        CFRunLoopRemoveSource(usbridgeRunLoop, usbridgeRunLoopSource, kCFRunLoopCommonModes);
        CFRelease(usbridgeRunLoopSource);
        usbridgeRunLoopSource = NULL;
    }
    if (usbridgeEventTap != NULL) {
        CFRelease(usbridgeEventTap);
        usbridgeEventTap = NULL;
    }
    if (usbridgeRunLoop != NULL) {
        CFRelease(usbridgeRunLoop);
        usbridgeRunLoop = NULL;
    }

    CGAssociateMouseAndMouseCursorPosition(true);
    if (usbridgeCursorHidden) {
        CGDisplayShowCursor(kCGDirectMainDisplay);
        usbridgeCursorHidden = false;
    }
}

static void usbridgeStopNativeCapture(void) {
    usbridgeCaptureActive = false;
    if (usbridgeRunLoop != NULL) {
        CFRunLoopStop(usbridgeRunLoop);
    } else {
        CGAssociateMouseAndMouseCursorPosition(true);
        if (usbridgeCursorHidden) {
            CGDisplayShowCursor(kCGDirectMainDisplay);
            usbridgeCursorHidden = false;
        }
    }
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"usbridge-client/internal/api"

	"github.com/sirupsen/logrus"
)

type darwinNativeFullscreenCapture struct {
	usbClient *api.USBClient
	onExit    func()

	events     chan nativeCaptureEvent
	stopEvents chan struct{}
	loopDone   chan struct{}

	moveDX atomic.Int64
	moveDY atomic.Int64

	running atomic.Bool
}

type nativeCaptureEvent struct {
	kind   string
	button int
	value  int
}

var (
	activeDarwinCaptureMu sync.RWMutex
	activeDarwinCapture   *darwinNativeFullscreenCapture
)

func newPlatformNativeFullscreenCapture(usbClient *api.USBClient, onExit func()) nativeFullscreenCapture {
	return &darwinNativeFullscreenCapture{
		usbClient:  usbClient,
		onExit:     onExit,
		events:     make(chan nativeCaptureEvent, 128),
		stopEvents: make(chan struct{}),
		loopDone:   make(chan struct{}),
	}
}

func (c *darwinNativeFullscreenCapture) Start() error {
	if c.usbClient == nil {
		return fmt.Errorf("usb client is not configured for native fullscreen capture")
	}
	if !c.running.CompareAndSwap(false, true) {
		return nil
	}

	activeDarwinCaptureMu.Lock()
	activeDarwinCapture = c
	activeDarwinCaptureMu.Unlock()

	go c.processEvents()

	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if ok := bool(C.usbridgeStartNativeCapture()); !ok {
			started <- fmt.Errorf("failed to install macOS native event tap; check Input Monitoring / Accessibility permissions")
			return
		}

		started <- nil
		C.usbridgeRunNativeCaptureLoop()
		close(c.loopDone)
	}()

	if err := <-started; err != nil {
		c.running.Store(false)
		activeDarwinCaptureMu.Lock()
		if activeDarwinCapture == c {
			activeDarwinCapture = nil
		}
		activeDarwinCaptureMu.Unlock()
		close(c.stopEvents)
		return err
	}

	logrus.Info("✅ macOS native fullscreen input capture started")
	return nil
}

func (c *darwinNativeFullscreenCapture) Stop() error {
	if !c.running.CompareAndSwap(true, false) {
		return nil
	}

	C.usbridgeStopNativeCapture()
	<-c.loopDone
	close(c.stopEvents)

	activeDarwinCaptureMu.Lock()
	if activeDarwinCapture == c {
		activeDarwinCapture = nil
	}
	activeDarwinCaptureMu.Unlock()

	logrus.Info("✅ macOS native fullscreen input capture stopped")
	return nil
}

func (c *darwinNativeFullscreenCapture) processEvents() {
	ticker := time.NewTicker(4 * time.Millisecond)
	defer ticker.Stop()

	activeButton := 0

	flushMove := func() {
		dx := int(c.moveDX.Swap(0))
		dy := int(c.moveDY.Swap(0))
		if dx == 0 && dy == 0 {
			return
		}

		for dx != 0 || dy != 0 {
			stepX := clamp(dx, -127, 127)
			stepY := clamp(dy, -127, 127)
			dx -= stepX
			dy -= stepY

			var err error
			if activeButton != 0 {
				err = c.usbClient.SendMouseAction(activeButton, stepX, stepY, 0)
			} else {
				err = c.usbClient.SendMouseMove(stepX, stepY)
			}
			if err != nil {
				logrus.Errorf("❌ macOS native capture mouse move failed: %v", err)
				return
			}
		}
	}

	for {
		select {
		case <-c.stopEvents:
			flushMove()
			if activeButton != 0 {
				if err := c.usbClient.SendMouseAction(0, 0, 0, 0); err != nil {
					logrus.Warnf("⚠️ macOS native capture button release failed on stop: %v", err)
				}
			}
			return
		case <-ticker.C:
			flushMove()
		case event := <-c.events:
			switch event.kind {
			case "button":
				flushMove()
				if event.value != 0 {
					activeButton = event.button
					if err := c.usbClient.SendMouseAction(activeButton, 0, 0, 0); err != nil {
						logrus.Errorf("❌ macOS native capture button press failed: %v", err)
					}
				} else {
					if activeButton == event.button || activeButton == 0 {
						activeButton = 0
					}
					if err := c.usbClient.SendMouseAction(0, 0, 0, 0); err != nil {
						logrus.Errorf("❌ macOS native capture button release failed: %v", err)
					}
				}
			case "scroll":
				flushMove()
				if err := c.usbClient.SendMouseScroll(event.value); err != nil {
					logrus.Errorf("❌ macOS native capture scroll failed: %v", err)
				}
			case "exit":
				if c.onExit != nil {
					go c.onExit()
				}
			}
		}
	}
}

func (c *darwinNativeFullscreenCapture) enqueue(event nativeCaptureEvent) {
	if !c.running.Load() {
		return
	}
	select {
	case c.events <- event:
	default:
		logrus.Warn("⚠️ macOS native capture event queue overflow")
	}
}

func activeCapture() *darwinNativeFullscreenCapture {
	activeDarwinCaptureMu.RLock()
	defer activeDarwinCaptureMu.RUnlock()
	return activeDarwinCapture
}

//export goNativeCaptureMove
func goNativeCaptureMove(dx, dy C.int) {
	capture := activeCapture()
	if capture == nil {
		return
	}
	capture.moveDX.Add(int64(int(dx)))
	capture.moveDY.Add(int64(-int(dy)))
}

//export goNativeCaptureButton
func goNativeCaptureButton(button, pressed C.int) {
	capture := activeCapture()
	if capture == nil {
		return
	}
	capture.enqueue(nativeCaptureEvent{
		kind:   "button",
		button: int(button),
		value:  int(pressed),
	})
}

//export goNativeCaptureScroll
func goNativeCaptureScroll(delta C.int) {
	capture := activeCapture()
	if capture == nil {
		return
	}
	capture.enqueue(nativeCaptureEvent{
		kind:  "scroll",
		value: clamp(-int(delta), -127, 127),
	})
}

//export goNativeCaptureExit
func goNativeCaptureExit() {
	capture := activeCapture()
	if capture == nil {
		return
	}
	capture.enqueue(nativeCaptureEvent{kind: "exit"})
}
