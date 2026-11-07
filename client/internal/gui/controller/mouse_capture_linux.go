//go:build linux && !android && cgo

package controller

/*
#cgo LDFLAGS: -lX11 -ldl -lwayland-client

#include "mouse_capture_linux.h"
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

// linuxMouseCaptureEngine is the Linux mouseCaptureEngine (see mouse_
// capture.go). See mouse_capture_linux.c's top comment for the X11
// approach, why it needs no permission, and the Xlib threading footgun it
// works around.
type linuxMouseCaptureEngine struct {
	miProvider func() service.MoonlightInputSender
	running    atomic.Bool
	wayland    bool // started through the Wayland path (set on the main thread in Start)
}

// startWayland locks the pointer through the compositor's pointer-
// constraints / relative-pointer protocols (mouse_capture_wl_linux.c).
func (e *linuxMouseCaptureEngine) startWayland(surface uintptr) error {
	display := waylandDisplay()
	if display == 0 {
		return fmt.Errorf("no Wayland display")
	}
	activeLinuxMouseCaptureMu.Lock()
	activeLinuxMouseCapture = e
	activeLinuxMouseCaptureMu.Unlock()
	if C.usbridge_mouse_capture_wl_start(C.uintptr_t(display), C.uintptr_t(surface)) == 0 {
		activeLinuxMouseCaptureMu.Lock()
		if activeLinuxMouseCapture == e {
			activeLinuxMouseCapture = nil
		}
		activeLinuxMouseCaptureMu.Unlock()
		return fmt.Errorf("the Wayland compositor does not offer pointer lock (zwp_pointer_constraints_v1 / zwp_relative_pointer_v1)")
	}
	return nil
}

func newPlatformMouseCaptureEngine(miProvider func() service.MoonlightInputSender) mouseCaptureEngine {
	return &linuxMouseCaptureEngine{miProvider: miProvider}
}

// Only one engine is ever active at a time (Capture is a single exclusive
// mouse mode), so a single package-level pointer is enough to route the
// cgo callbacks below to the right Go instance.
var (
	activeLinuxMouseCaptureMu sync.Mutex
	activeLinuxMouseCapture   *linuxMouseCaptureEngine
)

func (e *linuxMouseCaptureEngine) Start(window fyne.Window) error {
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
		var xwin, wlSurface uintptr
		switch c := ctx.(type) {
		case *driver.X11WindowContext:
			xwin = c.WindowHandle
		case driver.X11WindowContext:
			xwin = c.WindowHandle
		case *driver.WaylandWindowContext:
			wlSurface = c.WaylandSurface
		case driver.WaylandWindowContext:
			wlSurface = c.WaylandSurface
		default:
			started <- fmt.Errorf("unexpected native context type %T", ctx)
			return
		}
		if wlSurface != 0 {
			e.wayland = true
			started <- e.startWayland(wlSurface)
			return
		}
		e.wayland = false
		if xwin == 0 {
			started <- fmt.Errorf("X11 window handle is nil")
			return
		}

		activeLinuxMouseCaptureMu.Lock()
		activeLinuxMouseCapture = e
		activeLinuxMouseCaptureMu.Unlock()

		if ok := C.usbridge_mouse_capture_start(C.uintptr_t(xwin)); ok == 0 {
			activeLinuxMouseCaptureMu.Lock()
			if activeLinuxMouseCapture == e {
				activeLinuxMouseCapture = nil
			}
			activeLinuxMouseCaptureMu.Unlock()
			started <- fmt.Errorf("failed to start X11 pointer capture")
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

func (e *linuxMouseCaptureEngine) Stop() error {
	if !e.running.CompareAndSwap(true, false) {
		return nil
	}
	if e.wayland {
		C.usbridge_mouse_capture_wl_stop()
	} else {
		C.usbridge_mouse_capture_stop()
	}
	activeLinuxMouseCaptureMu.Lock()
	if activeLinuxMouseCapture == e {
		activeLinuxMouseCapture = nil
	}
	activeLinuxMouseCaptureMu.Unlock()
	return nil
}

func activeLinuxMouseCaptureEngine() *linuxMouseCaptureEngine {
	activeLinuxMouseCaptureMu.Lock()
	defer activeLinuxMouseCaptureMu.Unlock()
	return activeLinuxMouseCapture
}

// goMouseCaptureButton's button already matches Limelight.h's BUTTON_LEFT/
// MIDDLE/RIGHT (1/2/3) -- see mouse_capture_linux.c's mapping comment.

//export goMouseCaptureMove
func goMouseCaptureMove(dx, dy C.int) {
	e := activeLinuxMouseCaptureEngine()
	if e == nil || e.miProvider == nil {
		return
	}
	mi := e.miProvider()
	if mi == nil {
		return
	}
	mi.SendMoonlightMouseMove(int16(clamp(int(dx), -32767, 32767)), int16(clamp(int(dy), -32767, 32767)))
}

//export goMouseCaptureButton
func goMouseCaptureButton(button, pressed C.int) {
	e := activeLinuxMouseCaptureEngine()
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
	e := activeLinuxMouseCaptureEngine()
	if e == nil || e.miProvider == nil {
		return
	}
	mi := e.miProvider()
	if mi == nil {
		return
	}
	mi.SendMoonlightScroll(int8(clamp(int(delta), -127, 127)))
}
