//go:build darwin && !ios

package controller

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework CoreGraphics

#include "mouse_capture_darwin.h"
*/
import "C"

import (
	"fmt"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"

	"usbridge-client/internal/service"
)

// darwinMouseCaptureEngine is the macOS mouseCaptureEngine (see mouse_
// capture.go). See mouse_capture_darwin.m's top comment for why this uses a
// local NSEvent monitor instead of native_fullscreen_capture_darwin.go's
// global CGEventTap, and in particular why that choice needs no
// Accessibility/Input Monitoring permission.
type darwinMouseCaptureEngine struct {
	miProvider func() service.MoonlightInputSender
	running    atomic.Bool
}

func newPlatformMouseCaptureEngine(miProvider func() service.MoonlightInputSender) mouseCaptureEngine {
	return &darwinMouseCaptureEngine{miProvider: miProvider}
}

// Only one engine is ever active at a time (Capture is a single exclusive
// mouse mode), so a single package-level pointer is enough to route the
// cgo callbacks below to the right Go instance -- same pattern as
// native_fullscreen_capture_darwin.go's activeDarwinCapture.
var (
	activeDarwinMouseCaptureMu sync.Mutex
	activeDarwinMouseCapture   *darwinMouseCaptureEngine
)

func (e *darwinMouseCaptureEngine) Start(window fyne.Window) error {
	if e.miProvider == nil {
		return fmt.Errorf("moonlight input provider is not configured for mouse capture")
	}
	if window == nil {
		return fmt.Errorf("no window to capture")
	}
	if !e.running.CompareAndSwap(false, true) {
		return nil
	}
	nw, ok := window.(driver.NativeWindow)
	if !ok {
		e.running.Store(false)
		return fmt.Errorf("window does not implement driver.NativeWindow")
	}

	started := make(chan error, 1)
	nw.RunNative(func(ctx any) {
		var nsWindow uintptr
		switch c := ctx.(type) {
		case *driver.MacWindowContext:
			nsWindow = c.NSWindow
		case driver.MacWindowContext:
			nsWindow = c.NSWindow
		default:
			started <- fmt.Errorf("unexpected native context type %T", ctx)
			return
		}
		if nsWindow == 0 {
			started <- fmt.Errorf("NSWindow handle is nil")
			return
		}

		activeDarwinMouseCaptureMu.Lock()
		activeDarwinMouseCapture = e
		activeDarwinMouseCaptureMu.Unlock()

		if ok := C.usbridge_mouse_capture_start(C.uintptr_t(nsWindow)); ok == 0 {
			activeDarwinMouseCaptureMu.Lock()
			if activeDarwinMouseCapture == e {
				activeDarwinMouseCapture = nil
			}
			activeDarwinMouseCaptureMu.Unlock()
			started <- fmt.Errorf("failed to install local mouse event monitor")
			return
		}
		started <- nil
	})

	if err := <-started; err != nil {
		e.running.Store(false)
		return err
	}
	return nil
}

func (e *darwinMouseCaptureEngine) Stop() error {
	if !e.running.CompareAndSwap(true, false) {
		return nil
	}
	C.usbridge_mouse_capture_stop()
	activeDarwinMouseCaptureMu.Lock()
	if activeDarwinMouseCapture == e {
		activeDarwinMouseCapture = nil
	}
	activeDarwinMouseCaptureMu.Unlock()
	return nil
}

func activeMouseCapture() *darwinMouseCaptureEngine {
	activeDarwinMouseCaptureMu.Lock()
	defer activeDarwinMouseCaptureMu.Unlock()
	return activeDarwinMouseCapture
}

//export goMouseCaptureMove
func goMouseCaptureMove(dx, dy C.int) {
	e := activeMouseCapture()
	if e == nil || e.miProvider == nil {
		return
	}
	mi := e.miProvider()
	if mi == nil {
		return
	}
	// Confirmed live: NSEvent's -deltaY is already positive-is-down, matching
	// LiSendMouseMoveEvent's own convention directly -- no sign flip needed.
	// (native_fullscreen_capture_darwin.go negates dy, but that's reading
	// CGEventGetIntegerValueField(kCGMouseEventDeltaY) from a CGEventTap,
	// which is evidently NOT the same sign convention as NSEvent's -deltaY
	// despite both nominally wrapping the same hardware field.)
	mi.SendMoonlightMouseMove(int16(clamp(int(dx), -32767, 32767)), int16(clamp(int(dy), -32767, 32767)))
}

// goMouseCaptureButton's button already matches Limelight.h's BUTTON_LEFT/
// MIDDLE/RIGHT (1/2/3) -- see mouse_capture_darwin.m's mapping comment.
//
//export goMouseCaptureButton
func goMouseCaptureButton(button, pressed C.int) {
	e := activeMouseCapture()
	if e == nil || e.miProvider == nil {
		return
	}
	mi := e.miProvider()
	if mi == nil {
		return
	}
	action := service.LiMouseButtonRelease
	if pressed != 0 {
		action = service.LiMouseButtonPress
	}
	mi.SendMoonlightMouseButton(action, int(button))
}

//export goMouseCaptureScroll
func goMouseCaptureScroll(delta C.int) {
	e := activeMouseCapture()
	if e == nil || e.miProvider == nil {
		return
	}
	mi := e.miProvider()
	if mi == nil {
		return
	}
	mi.SendMoonlightScroll(int8(clamp(int(delta), -127, 127)))
}

//export goMouseCaptureHScroll
func goMouseCaptureHScroll(delta C.int) {
	e := activeMouseCapture()
	if e == nil || e.miProvider == nil {
		return
	}
	mi := e.miProvider()
	if mi == nil {
		return
	}
	mi.SendMoonlightHScroll(int8(clamp(int(delta), -127, 127)))
}
