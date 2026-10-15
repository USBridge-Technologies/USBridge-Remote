//go:build darwin && !ios && cgo && metaltest

// Package-internal helpers for the Metal-overlay regression tests in
// metal_video_leak_darwin_test.go. Go's cgo toolchain rejects `import "C"`
// directly inside a _test.go file ("use of cgo in test ... not supported",
// enforced by cmd/go's module index), so the actual NSWindow-creation cgo
// lives here instead, gated behind the `metaltest` build tag so it never
// ships in a normal build:
//
//	go test -tags metaltest ./internal/service/ -run TestMetalVideoCreateReplace -v
package service

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>
#include <stdint.h>
#include <pthread.h>

// metal_test_is_main_thread guards the leak regression test against
// hanging: metal_video_create/_destroy dispatch_sync onto the main queue
// when called off the main thread, which never drains in a bare `go test`
// binary (no NSApplication run loop pumping) -- the test must instead run
// directly ON the process's main OS thread, matching how `go test` executes
// a non-parallel TestXxx on goroutine 1 (see cmd/metalspike/main.go for the
// same real-NSWindow pattern used from an actual main(), which is
// guaranteed main-thread by construction).
static int metal_test_is_main_thread(void) {
    return pthread_main_np() != 0;
}

// metal_test_make_window creates a real, never-ordered-front (so it doesn't
// steal focus during an automated test run) NSWindow with a valid
// contentView -- the one precondition metal_video_create actually needs
// (see its own "no contentView" early-out). Mirrors cmd/metalspike/main.go's
// spike_make_window, minus makeKeyAndOrderFront/activateIgnoringOtherApps.
static uintptr_t metal_test_make_window(void) {
    __block NSWindow *win = nil;
    @autoreleasepool {
        [NSApplication sharedApplication];
        NSRect frame = NSMakeRect(0, 0, 320, 240);
        win = [[NSWindow alloc] initWithContentRect:frame
                                           styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable)
                                             backing:NSBackingStoreBuffered
                                               defer:NO];
    }
    return (uintptr_t)(__bridge_retained void *)win;
}

// metal_test_release_window balances metal_test_make_window's __bridge_retained.
static void metal_test_release_window(uintptr_t winPtr) {
    NSWindow *win = (__bridge_transfer NSWindow *)(void *)winPtr;
    (void)win; // ARC releases it once this scope ends.
}
*/
import "C"

// metalTestIsMainThread reports whether the calling goroutine is currently
// running on the process's actual main OS thread.
func metalTestIsMainThread() bool {
	return C.metal_test_is_main_thread() != 0
}

// metalTestMakeWindow creates a throwaway real NSWindow for exercising
// MetalVideoCreate/-Destroy in tests. Returns 0 on failure.
func metalTestMakeWindow() uintptr {
	return uintptr(C.metal_test_make_window())
}

// metalTestReleaseWindow releases a window returned by metalTestMakeWindow.
func metalTestReleaseWindow(win uintptr) {
	C.metal_test_release_window(C.uintptr_t(win))
}
