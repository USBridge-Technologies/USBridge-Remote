//go:build darwin && !ios

// Windowed (non-global) raw relative mouse capture for Capture mode.
//
// go/build's filename-based matching treats "_darwin" files as valid for
// GOOS=ios too, same as "_linux" for android -- without the explicit tag
// above this gets pulled into iOS builds and fails on missing AppKit/NSEvent
// symbols. Same fix native_fullscreen_capture_darwin.* and
// qr_camera_scanner_darwin.* already carry.
//
// This is deliberately NOT built on CGEventTap/kCGSessionEventTap the way
// native_fullscreen_capture_darwin.go's exclusive-fullscreen input path is --
// that one needs a *global* session event tap (because its custom fullscreen
// surface bypasses the normal AppKit responder chain entirely), and global
// taps/monitors always need the user to grant Accessibility (and, for mouse
// events specifically, Input Monitoring) in System Settings.
//
// Capture mode instead runs inside the app's own focused window via
// +[NSEvent addLocalMonitorForEventsMatchingMask:handler:], which only ever
// sees events already addressed to this app's own windows. Apple documents
// local monitors as not requiring Accessibility permission (unlike the
// global monitor/tap APIs) -- confirmed against the current AppKit event
// overview docs. CGAssociateMouseAndMouseCursorPosition/CGDisplayHideCursor
// are plain CoreGraphics calls usable by any foreground app, no entitlement
// needed either. Net effect: Capture mode never shows a permission prompt.
//
// Keyboard events are never touched here, so the app's existing
// Ctrl+Alt+Shift+M hotkey (video_widget_hotkeys.go) keeps reaching Fyne
// normally and can release capture, the same way Moonlight's own
// Ctrl+Alt+Shift+Z does.

#include <TargetConditionals.h>
#if !TARGET_OS_IPHONE

#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
#include <math.h>

#include "mouse_capture_darwin.h"

extern void goMouseCaptureMove(int dx, int dy);
extern void goMouseCaptureButton(int button, int pressed);
extern void goMouseCaptureScroll(int delta);
extern void goMouseCaptureHScroll(int delta);

static id g_monitor = nil;
static NSWindow *g_window = nil;
static BOOL g_cursorHidden = NO;
static BOOL g_active = NO;

int usbridge_mouse_capture_start(uintptr_t nsWindowPtr) {
    if (g_active) {
        return 1;
    }
    NSWindow *window = (__bridge NSWindow *)((void *)nsWindowPtr);
    if (window == nil) {
        return 0;
    }

    NSEventMask mask = NSEventMaskMouseMoved | NSEventMaskLeftMouseDragged |
        NSEventMaskRightMouseDragged | NSEventMaskOtherMouseDragged |
        NSEventMaskLeftMouseDown | NSEventMaskLeftMouseUp |
        NSEventMaskRightMouseDown | NSEventMaskRightMouseUp |
        NSEventMaskOtherMouseDown | NSEventMaskOtherMouseUp |
        NSEventMaskScrollWheel;

    g_window = window;
    g_active = YES;

    g_monitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask handler:^NSEvent *(NSEvent *event) {
        if (!g_active || event.window != g_window) {
            return event;
        }

        switch (event.type) {
            case NSEventTypeMouseMoved:
            case NSEventTypeLeftMouseDragged:
            case NSEventTypeRightMouseDragged:
            case NSEventTypeOtherMouseDragged: {
                CGFloat dx = event.deltaX;
                CGFloat dy = event.deltaY;
                if (dx != 0 || dy != 0) {
                    goMouseCaptureMove((int)lround(dx), (int)lround(dy));
                }
                return nil; // swallow -- Fyne must never see this
            }
            // Button numbers match Limelight.h's BUTTON_LEFT/MIDDLE/RIGHT
            // (1/2/3) directly -- NOT native_fullscreen_capture_darwin.go's
            // own 1/2/3 numbering, which swaps 2 and 3 for right/middle.
            case NSEventTypeLeftMouseDown:
                goMouseCaptureButton(1, 1);
                return nil;
            case NSEventTypeLeftMouseUp:
                goMouseCaptureButton(1, 0);
                return nil;
            case NSEventTypeRightMouseDown:
                goMouseCaptureButton(3, 1);
                return nil;
            case NSEventTypeRightMouseUp:
                goMouseCaptureButton(3, 0);
                return nil;
            case NSEventTypeOtherMouseDown:
                goMouseCaptureButton(2, 1);
                return nil;
            case NSEventTypeOtherMouseUp:
                goMouseCaptureButton(2, 0);
                return nil;
            case NSEventTypeScrollWheel: {
                CGFloat delta = event.hasPreciseScrollingDeltas ? event.scrollingDeltaY : event.deltaY;
                if (delta != 0) {
                    goMouseCaptureScroll((int)lround(delta));
                }
                // Sideways: deltaX > 0 is wheel left, Moonlight's positive
                // horizontal scroll is right.
                CGFloat hdelta = event.hasPreciseScrollingDeltas ? event.scrollingDeltaX : event.deltaX;
                if (hdelta != 0) {
                    goMouseCaptureHScroll(-(int)lround(hdelta));
                }
                return nil;
            }
            default:
                return event;
        }
    }];

    // Freeze the OS cursor avatar in place and hide it. Unlike the
    // fullscreen/global-tap path, we deliberately do NOT warp the cursor
    // to a fixed point first -- the user just toggled Capture while the
    // pointer was already wherever they wanted it (over the video), and
    // leaving it there means Stop() below restores it to the same spot.
    CGAssociateMouseAndMouseCursorPosition(false);
    CGDisplayHideCursor(kCGDirectMainDisplay);
    g_cursorHidden = YES;

    return 1;
}

void usbridge_mouse_capture_stop(void) {
    if (g_monitor != nil) {
        [NSEvent removeMonitor:g_monitor];
        g_monitor = nil;
    }
    g_active = NO;
    g_window = nil;

    CGAssociateMouseAndMouseCursorPosition(true);
    if (g_cursorHidden) {
        CGDisplayShowCursor(kCGDirectMainDisplay);
        g_cursorHidden = NO;
    }
}

#endif
