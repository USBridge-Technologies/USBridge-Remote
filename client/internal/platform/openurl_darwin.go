//go:build darwin && !ios

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation

#import <AppKit/AppKit.h>
#import <objc/runtime.h>

extern void goOpenedURL(char*);

// A clicked usbridge:// link never reaches os.Args on macOS: LaunchServices
// sends a GetURL Apple Event, which AppKit hands to the application
// delegate's application:openURLs: -- and GLFW's delegate has none, so the
// link was dropped. Give it one before NSApp finishes launching (AppKit
// checks for it then), for both a cold start and an already running app.
static void usbridgeOpenURLs(id self, SEL _cmd, NSApplication *app, NSArray<NSURL *> *urls) {
	for (NSURL *u in urls) {
		const char *s = [[u absoluteString] UTF8String];
		if (s) goOpenedURL((char *)s);
	}
}

__attribute__((constructor)) static void usbridgeInstallOpenURLs(void) {
	Class cls = objc_getClass("GLFWApplicationDelegate");
	if (cls) {
		class_addMethod(cls, @selector(application:openURLs:), (IMP)usbridgeOpenURLs, "v@:@@");
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

// TakeOpenedURL returns (once) the last usbridge:// link macOS opened the
// app with, "" if none came since the last call.
func TakeOpenedURL() string {
	openedURLMu.Lock()
	defer openedURLMu.Unlock()
	u := openedURL
	openedURL = ""
	return u
}
