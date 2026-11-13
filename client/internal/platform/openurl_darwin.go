//go:build darwin

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation

#import <Foundation/Foundation.h>
#import <objc/runtime.h>

extern void goOpenedURL(char*);

// A clicked usbridge:// link never reaches os.Args on macOS or iOS: the OS
// hands it to the application delegate -- application:openURLs: on macOS,
// application:openURL:options: on iOS -- and neither GLFW's delegate (macOS)
// nor Fyne's GoAppAppDelegate (iOS) has the method, so the link was dropped.
// Add it before the app finishes launching (AppKit/UIKit check for it then),
// for both a cold start and an already running app.
static void usbridgeOpenedURL(NSURL *u) {
	const char *s = [[u absoluteString] UTF8String];
	if (s) goOpenedURL((char *)s);
}

static void usbridgeOpenURLs(id self, SEL _cmd, id app, NSArray<NSURL *> *urls) {
	for (NSURL *u in urls) usbridgeOpenedURL(u);
}

static BOOL usbridgeOpenURL(id self, SEL _cmd, id app, NSURL *url, NSDictionary *options) {
	usbridgeOpenedURL(url);
	return YES;
}

__attribute__((constructor)) static void usbridgeInstallOpenURLs(void) {
	Class mac = objc_getClass("GLFWApplicationDelegate");
	if (mac) {
		class_addMethod(mac, @selector(application:openURLs:), (IMP)usbridgeOpenURLs, "v@:@@");
	}
	Class ios = objc_getClass("GoAppAppDelegate");
	if (ios) {
		class_addMethod(ios, @selector(application:openURL:options:), (IMP)usbridgeOpenURL, "B@:@@@");
	}
}
*/
import "C"

import (
	"strings"
	"sync"
)

var (
	openedURLMu sync.Mutex
	openedURL   string
)

//export goOpenedURL
func goOpenedURL(s *C.char) {
	u := C.GoString(s)
	if !strings.HasPrefix(u, URLScheme+"://") {
		return
	}
	openedURLMu.Lock()
	openedURL = u
	openedURLMu.Unlock()
}

// TakeOpenedURL returns (once) the last usbridge:// link macOS/iOS opened
// the app with, "" if none came since the last call.
func TakeOpenedURL() string {
	openedURLMu.Lock()
	defer openedURLMu.Unlock()
	u := openedURL
	openedURL = ""
	return u
}
