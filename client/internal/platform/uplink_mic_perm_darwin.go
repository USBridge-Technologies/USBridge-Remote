//go:build darwin && cgo

package platform

/*
#cgo LDFLAGS: -framework AVFoundation -framework Foundation
#include <CoreFoundation/CoreFoundation.h>
#include <objc/runtime.h>
#include <objc/message.h>

extern void usbridgeMicPermissionResult(int granted);

// AVCaptureDevice's microphone permission, the same on macOS (TCC) and iOS;
// "soun" is AVMediaTypeAudio. AVAuthorizationStatus: 0 not determined,
// 1 restricted, 2 denied, 3 authorized.
static int mic_auth_status(void) {
    id cls = (id)objc_getClass("AVCaptureDevice");
    if (!cls) return 3;
    return (int)((long (*)(id, SEL, id))objc_msgSend)(
        cls, sel_registerName("authorizationStatusForMediaType:"), (id)CFSTR("soun"));
}

// mic_auth_request shows the prompt; the answer comes on a queue of its own.
static void mic_auth_request(void) {
    id cls = (id)objc_getClass("AVCaptureDevice");
    if (!cls) { usbridgeMicPermissionResult(1); return; }
    void (^done)(BOOL) = ^(BOOL granted) { usbridgeMicPermissionResult(granted ? 1 : 0); };
    ((void (*)(id, SEL, id, id))objc_msgSend)(
        cls, sel_registerName("requestAccessForMediaType:completionHandler:"), (id)CFSTR("soun"), (id)done);
}
*/
import "C"

import (
	"net/url"
	"runtime"

	"fyne.io/fyne/v2"
)

// MicPermission is this app's microphone permission.
func MicPermission() MicAuth {
	switch C.mic_auth_status() {
	case 0:
		return MicAuthUndetermined
	case 3:
		return MicAuthGranted
	}
	return MicAuthDenied
}

// RequestMicPermission asks the user (NSMicrophoneUsageDescription) and
// calls done with the answer, from another goroutine.
func RequestMicPermission(done func(granted bool)) {
	micPermRequest(done, func() { C.mic_auth_request() })
}

//export usbridgeMicPermissionResult
func usbridgeMicPermissionResult(granted C.int) {
	go micPermAnswered(granted != 0)
}

// OpenMicSettings opens Privacy → Microphone on macOS, the app's own
// settings page on iOS.
func OpenMicSettings() {
	target := "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone"
	if runtime.GOOS == "ios" {
		target = "app-settings:"
	}
	if u, err := url.Parse(target); err == nil {
		if app := fyne.CurrentApp(); app != nil {
			_ = app.OpenURL(u)
		}
	}
}
