//go:build darwin && !ios

package controller

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework CoreGraphics

#import <AppKit/AppKit.h>

extern void goFullscreenExitKey(void);

// The window server's switch for the system's own shortcuts (Cmd+Tab,
// Mission Control, Show Desktop on Fn+F11, Spotlight...). Private, but
// what SDL's keyboard grab -- and so Moonlight -- uses on macOS.
typedef int CGSConnectionID;
extern CGSConnectionID _CGSDefaultConnection(void);
extern CGError CGSSetGlobalHotKeyOperatingMode(CGSConnectionID cid, int mode);
enum { usbridgeHotKeysEnable = 0, usbridgeHotKeysDisable = 1 };

static id usbridgeExitKeyMonitor = nil;
static id usbridgeActiveObserver = nil;
static id usbridgeResignObserver = nil;

static void usbridgeSystemHotKeys(bool on) {
	CGSSetGlobalHotKeyOperatingMode(_CGSDefaultConnection(), on ? usbridgeHotKeysEnable : usbridgeHotKeysDisable);
}

// Moonlight's Ctrl+Option+Shift+X, or Ctrl+Esc, leaves fullscreen. Seen
// here before the window gets them: GLFW never delivered Ctrl+Esc to Fyne
// in the fullscreen window. F11 is not one of them on a Mac: with the
// system shortcuts off, Fn+F1..F12 go to the host, F11 too.
static bool usbridgeIsExitKey(NSEvent *e) {
	NSEventModifierFlags m = e.modifierFlags & NSEventModifierFlagDeviceIndependentFlagsMask;
	bool ctrl = (m & NSEventModifierFlagControl) != 0;
	bool opt = (m & NSEventModifierFlagOption) != 0;
	bool shift = (m & NSEventModifierFlagShift) != 0;
	bool cmd = (m & NSEventModifierFlagCommand) != 0;
	switch (e.keyCode) {
	case 7:  return ctrl && opt && shift && !cmd; // X
	case 53: return ctrl;                         // Esc
	}
	return false;
}

static void usbridgeStartExitKeyMonitor(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (usbridgeExitKeyMonitor != nil) return;
		usbridgeExitKeyMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskKeyDown
			handler:^NSEvent *(NSEvent *e) {
				if (!usbridgeIsExitKey(e)) return e;
				goFullscreenExitKey();
				return nil;
			}];
		// The system shortcuts stay off only while we are the active app:
		// Cmd+Tab out of us (impossible while off) or a click elsewhere
		// gives them back.
		NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
		usbridgeActiveObserver = [nc addObserverForName:NSApplicationDidBecomeActiveNotification object:nil queue:nil
			usingBlock:^(NSNotification *n) { (void)n; usbridgeSystemHotKeys(false); }];
		usbridgeResignObserver = [nc addObserverForName:NSApplicationDidResignActiveNotification object:nil queue:nil
			usingBlock:^(NSNotification *n) { (void)n; usbridgeSystemHotKeys(true); }];
		if (NSApp.isActive) usbridgeSystemHotKeys(false);
	});
}

static void usbridgeStopExitKeyMonitor(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (usbridgeExitKeyMonitor == nil) return;
		[NSEvent removeMonitor:usbridgeExitKeyMonitor];
		usbridgeExitKeyMonitor = nil;
		NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
		if (usbridgeActiveObserver) [nc removeObserver:usbridgeActiveObserver];
		if (usbridgeResignObserver) [nc removeObserver:usbridgeResignObserver];
		usbridgeActiveObserver = nil;
		usbridgeResignObserver = nil;
		usbridgeSystemHotKeys(true);
	});
}

// (On quit while fullscreen the mode dies with the process's connection.)
*/
import "C"

import (
	"sync/atomic"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

var fullscreenExitKeyTarget atomic.Pointer[FullscreenDialog]

// startFullscreenExitKeys, while fd is fullscreen: watches for the exit
// keys and turns the system shortcuts off, so Fn+F1..F12, Cmd+Tab and the
// like go to the host as on Moonlight.
func startFullscreenExitKeys(fd *FullscreenDialog) {
	fullscreenExitKeyTarget.Store(fd)
	C.usbridgeStartExitKeyMonitor()
}

func stopFullscreenExitKeys() {
	fullscreenExitKeyTarget.Store(nil)
	C.usbridgeStopExitKeyMonitor()
}

//export goFullscreenExitKey
func goFullscreenExitKey() {
	fd := fullscreenExitKeyTarget.Load()
	if fd == nil {
		return
	}
	logrus.Info("🔍 Fullscreen exit key (macOS)")
	go fyne.Do(func() {
		// The modifiers went to the host as held keys: let them go there.
		if fd.videoWidget != nil {
			fd.videoWidget.releaseAllMoonlightKeys()
		}
		noteFullscreenExitKey()
		fd.exitFullscreen()
	})
}
