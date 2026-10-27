//go:build windows && cgo

package controller

/*
#cgo LDFLAGS: -luser32

#include "mouse_capture_windows.h"
*/
import "C"

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/service"
)

// windowsMouseCaptureEngine is the Windows mouseCaptureEngine (see mouse_
// capture.go). See mouse_capture_windows.c's top comment for the Raw
// Input + WH_GETMESSAGE approach and why it needs no special permission.
type windowsMouseCaptureEngine struct {
	miProvider func() service.MoonlightInputSender
	running    atomic.Bool
}

func newPlatformMouseCaptureEngine(miProvider func() service.MoonlightInputSender) mouseCaptureEngine {
	return &windowsMouseCaptureEngine{miProvider: miProvider}
}

// Only one engine is ever active at a time (Capture is a single exclusive
// mouse mode), so a single package-level pointer is enough to route the
// cgo callbacks below to the right Go instance.
var (
	activeWindowsMouseCaptureMu sync.Mutex
	activeWindowsMouseCapture   *windowsMouseCaptureEngine
)

func (e *windowsMouseCaptureEngine) Start(window fyne.Window) error {
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
		var hwnd uintptr
		switch c := ctx.(type) {
		case *driver.WindowsWindowContext:
			hwnd = c.HWND
		case driver.WindowsWindowContext:
			hwnd = c.HWND
		default:
			started <- fmt.Errorf("unexpected native context type %T", ctx)
			return
		}
		if hwnd == 0 {
			started <- fmt.Errorf("HWND handle is nil")
			return
		}

		activeWindowsMouseCaptureMu.Lock()
		activeWindowsMouseCapture = e
		activeWindowsMouseCaptureMu.Unlock()

		if ok := C.usbridge_mouse_capture_start(C.uintptr_t(hwnd)); ok == 0 {
			activeWindowsMouseCaptureMu.Lock()
			if activeWindowsMouseCapture == e {
				activeWindowsMouseCapture = nil
			}
			activeWindowsMouseCaptureMu.Unlock()
			started <- fmt.Errorf("failed to register raw mouse input / install message hook")
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

func (e *windowsMouseCaptureEngine) Stop() error {
	if !e.running.CompareAndSwap(true, false) {
		return nil
	}
	C.usbridge_mouse_capture_stop()
	activeWindowsMouseCaptureMu.Lock()
	if activeWindowsMouseCapture == e {
		activeWindowsMouseCapture = nil
	}
	activeWindowsMouseCaptureMu.Unlock()
	return nil
}

func activeWinMouseCapture() *windowsMouseCaptureEngine {
	activeWindowsMouseCaptureMu.Lock()
	defer activeWindowsMouseCaptureMu.Unlock()
	return activeWindowsMouseCapture
}

// Capture traffic counters, logged every 2s while moves flow: raw samples seen,
// moves handed to Moonlight, and samples dropped because no stream input was
// available. Tells "the engine saw nothing" from "the stream had no input"
// from "sent, but the remote ignored it".
var (
	capStatSeen, capStatSent, capStatDropped atomic.Int64
	capStatButtons                           atomic.Int64
	capStatLogAt                             atomic.Int64
)

func logCaptureStats() {
	now := time.Now().UnixNano()
	last := capStatLogAt.Load()
	if last == 0 {
		capStatLogAt.CompareAndSwap(0, now)
		return
	}
	if now-last < int64(2*time.Second) || !capStatLogAt.CompareAndSwap(last, now) {
		return
	}
	logrus.Infof("🖱️ [Capture] 2s | raw moves=%d sent=%d dropped(no input)=%d buttons=%d",
		capStatSeen.Swap(0), capStatSent.Swap(0), capStatDropped.Swap(0), capStatButtons.Swap(0))
}

//export goMouseCaptureMove
func goMouseCaptureMove(dx, dy C.int) {
	capStatSeen.Add(1)
	logCaptureStats()
	e := activeWinMouseCapture()
	if e == nil || e.miProvider == nil {
		capStatDropped.Add(1)
		return
	}
	mi := e.miProvider()
	if mi == nil {
		capStatDropped.Add(1)
		return
	}
	capStatSent.Add(1)
	// RAWMOUSE's lLastX/lLastY (and our own absolute-device delta fallback in
	// mouse_capture_windows.c) already use the same positive-Y-is-down
	// convention LiSendMouseMoveEvent expects -- no sign flip needed here,
	// unlike the macOS engine.
	mi.SendMoonlightMouseMove(int16(clamp(int(dx), -32767, 32767)), int16(clamp(int(dy), -32767, 32767)))
}

// goMouseCaptureButton's button already matches Limelight.h's BUTTON_LEFT/
// MIDDLE/RIGHT (1/2/3) -- see mouse_capture_windows.c's mapping comment.
//
//export goMouseCaptureButton
func goMouseCaptureButton(button, pressed C.int) {
	capStatButtons.Add(1)
	e := activeWinMouseCapture()
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
	e := activeWinMouseCapture()
	if e == nil || e.miProvider == nil {
		return
	}
	mi := e.miProvider()
	if mi == nil {
		return
	}
	mi.SendMoonlightScroll(int8(clamp(int(delta), -127, 127)))
}
