//go:build windows

package gui

import (
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"
)

var (
	placementUser32      = windows.NewLazySystemDLL("user32.dll")
	procGetWindowRect     = placementUser32.NewProc("GetWindowRect")
	procSetWindowPos      = placementUser32.NewProc("SetWindowPos")
	procGetSystemMetrics  = placementUser32.NewProc("GetSystemMetrics")
	procGetWindowLongPtr  = placementUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtr  = placementUser32.NewProc("SetWindowLongPtrW")
)

const (
	smXVIRTUALSCREEN  = 76
	smYVIRTUALSCREEN  = 77
	smCXVIRTUALSCREEN = 78
	smCYVIRTUALSCREEN = 79
	swpNoZOrder       = 0x0004
)

type placementWinRect struct {
	Left, Top, Right, Bottom int32
}

func fyneWindowHWND(window fyne.Window) uintptr {
	if window == nil {
		return 0
	}
	nw, ok := window.(driver.NativeWindow)
	if !ok {
		return 0
	}
	var hwnd uintptr
	nw.RunNative(func(ctx any) {
		switch c := ctx.(type) {
		case driver.WindowsWindowContext:
			hwnd = c.HWND
		case *driver.WindowsWindowContext:
			hwnd = c.HWND
		}
	})
	return hwnd
}

func nativeWindowFrame(window fyne.Window) (windowFrame, bool) {
	hwnd := fyneWindowHWND(window)
	if hwnd == 0 {
		return windowFrame{}, false
	}
	var rc placementWinRect
	r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	if r == 0 {
		return windowFrame{}, false
	}
	return windowFrame{
		X: int(rc.Left),
		Y: int(rc.Top),
		W: int(rc.Right - rc.Left),
		H: int(rc.Bottom - rc.Top),
	}, true
}

func nativeSetWindowFrame(window fyne.Window, f windowFrame) bool {
	hwnd := fyneWindowHWND(window)
	if hwnd == 0 || f.W <= 0 || f.H <= 0 {
		return false
	}
	r, _, _ := procSetWindowPos.Call(
		hwnd,
		0,
		uintptr(int32(f.X)),
		uintptr(int32(f.Y)),
		uintptr(int32(f.W)),
		uintptr(int32(f.H)),
		swpNoZOrder,
	)
	return r != 0
}

func nativeMoveWindow(window fyne.Window, x, y int) bool {
	hwnd := fyneWindowHWND(window)
	if hwnd == 0 {
		return false
	}
	const swpNoSize = 0x0001
	r, _, _ := procSetWindowPos.Call(
		hwnd,
		0,
		uintptr(int32(x)),
		uintptr(int32(y)),
		0,
		0,
		swpNoSize|swpNoZOrder,
	)
	return r != 0
}

func nativeUnlockWindowSize(window fyne.Window) {
	hwnd := fyneWindowHWND(window)
	if hwnd == 0 {
		return
	}
	const (
		gwlStyle        = ^uintptr(15) // GWL_STYLE = -16
		wsThickframe    = 0x00040000
		wsMaximizebox   = 0x00010000
		swpNoMove       = 0x0002
		swpNoSize       = 0x0001
		swpFrameChanged = 0x0020
	)
	style, _, _ := procGetWindowLongPtr.Call(hwnd, gwlStyle)
	style |= wsThickframe | wsMaximizebox
	procSetWindowLongPtr.Call(hwnd, gwlStyle, style)
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpFrameChanged)
}

func nativeWindowFrameIsVisible(f windowFrame) bool {
	vx := getSystemMetric(smXVIRTUALSCREEN)
	vy := getSystemMetric(smYVIRTUALSCREEN)
	vw := getSystemMetric(smCXVIRTUALSCREEN)
	vh := getSystemMetric(smCYVIRTUALSCREEN)
	return windowFrameVisible(f, vx, vy, vw, vh)
}

func getSystemMetric(index int) int {
	r, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int(int32(r))
}
