//go:build windows

package gui

import (
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"
)

var (
	placementUser32          = windows.NewLazySystemDLL("user32.dll")
	procGetWindowRect        = placementUser32.NewProc("GetWindowRect")
	procSetWindowPos         = placementUser32.NewProc("SetWindowPos")
	procGetSystemMetrics     = placementUser32.NewProc("GetSystemMetrics")
)

const (
	smXVIRTUALSCREEN  = 76
	smYVIRTUALSCREEN  = 77
	smCXVIRTUALSCREEN = 78
	smCYVIRTUALSCREEN = 79
	swpNoSize         = 0x0001
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

func nativeMoveWindow(window fyne.Window, x, y int) bool {
	hwnd := fyneWindowHWND(window)
	if hwnd == 0 {
		return false
	}
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
