//go:build windows

package taskbar

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"
)

const (
	clsctxInprocServer        = 0x1
	coinitApartmentThreaded   = 0x2
	dibRGBColors              = 0
	gaRoot                    = 2
	smCXSMICON                = 49
	tbpNoProgress             = 0
	overlayVtblHrInit         = 3
	overlayVtblSetProgressSt  = 10
	overlayVtblSetOverlayIcon = 18
)

var (
	clsidTaskbarList = windows.GUID{
		Data1: 0x56FDF344,
		Data2: 0xFD6D,
		Data3: 0x11d0,
		Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90},
	}
	iidITaskbarList3 = windows.GUID{
		Data1: 0xea1afb91,
		Data2: 0x9e28,
		Data3: 0x4b86,
		Data4: [8]byte{0x90, 0xe9, 0x9e, 0x9f, 0x8a, 0x5e, 0xef, 0xaf},
	}

	iidIClassFactory = windows.GUID{
		Data1: 0x00000001,
		Data2: 0x0000,
		Data3: 0x0000,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}

	ole32         = windows.NewLazySystemDLL("ole32.dll")
	explorerframe = windows.NewLazySystemDLL("explorerframe.dll")
	shell32       = windows.NewLazySystemDLL("shell32.dll")
	user32        = windows.NewLazySystemDLL("user32.dll")
	gdi32         = windows.NewLazySystemDLL("gdi32.dll")

	procCoInitializeEx     = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance   = ole32.NewProc("CoCreateInstance")
	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procGetDC              = user32.NewProc("GetDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procGetAncestor        = user32.NewProc("GetAncestor")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
)

type overlayRGB struct{ r, g, b byte }

var (
	overlayRunning = overlayRGB{0xc4, 0xe7, 0x7a} // footer lime
	overlayDone    = overlayRGB{0x30, 0xd4, 0xbd} // footer done teal
	overlayError   = overlayRGB{0xff, 0x5a, 0x52} // footer error
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

var overlayMu sync.Mutex
var (
	overlayTaskbar  uintptr
	overlayIcons    [3]uintptr
	overlayLast     ScriptState
	overlayHWND     uintptr
	overlayInitOnce bool
	overlayInitErr  error
)

func setScriptState(win fyne.Window, state ScriptState) {
	// Callers (script footer sync) already run on the Fyne thread.
	// Nested fyne.Do would only delay the COM call.
	applyScriptOverlay(win, state)
}

func applyScriptOverlay(win fyne.Window, state ScriptState) {
	hwnd := taskbarHWND(win)
	overlayMu.Lock()
	defer overlayMu.Unlock()

	if overlayLast == state && overlayHWND == hwnd && overlayTaskbar != 0 {
		return
	}
	if err := ensureTaskbar(); err != nil {
		return
	}
	if hwnd == 0 {
		overlayLast = ScriptIdle
		overlayHWND = 0
		return
	}

	var icon uintptr
	var desc *uint16
	switch state {
	case ScriptRunning:
		icon = overlayIcon(0, overlayRunning)
		desc, _ = windows.UTF16PtrFromString("Script running")
	case ScriptDone:
		icon = overlayIcon(1, overlayDone)
		desc, _ = windows.UTF16PtrFromString("Script finished")
	case ScriptError:
		icon = overlayIcon(2, overlayError)
		desc, _ = windows.UTF16PtrFromString("Script error")
	}

	callVtbl(overlayTaskbar, overlayVtblSetOverlayIcon, hwnd, icon, uintptr(unsafe.Pointer(desc)))
	callVtbl(overlayTaskbar, overlayVtblSetProgressSt, hwnd, tbpNoProgress)
	runtime.KeepAlive(desc)
	overlayLast = state
	overlayHWND = hwnd
}

func ensureTaskbar() error {
	if overlayTaskbar != 0 {
		return nil
	}
	if overlayInitOnce {
		return overlayInitErr
	}
	overlayInitOnce = true
	procCoInitializeEx.Call(0, coinitApartmentThreaded)

	punk, _, err := taskbarFromCoCreate(clsctxInprocServer)
	if punk == 0 {
		punk, _, err = taskbarFromInProcDLL(explorerframe, "explorerframe")
	}
	if punk == 0 {
		punk, _, err = taskbarFromInProcDLL(shell32, "shell32")
	}
	if punk == 0 {
		overlayInitErr = err
		return overlayInitErr
	}
	callVtbl(punk, overlayVtblHrInit)
	overlayTaskbar = punk
	return nil
}

func taskbarFromInProcDLL(dll *windows.LazyDLL, name string) (uintptr, string, error) {
	proc := dll.NewProc("DllGetClassObject")
	if err := proc.Find(); err != nil {
		return 0, "", fmt.Errorf("%s DllGetClassObject: %w", name, err)
	}
	var factory uintptr
	hr, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(&clsidTaskbarList)),
		uintptr(unsafe.Pointer(&iidIClassFactory)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if hr != 0 || factory == 0 {
		return 0, "", fmt.Errorf("%s DllGetClassObject hr=0x%x err=%v", name, uint32(hr), callErr)
	}
	defer callVtbl(factory, 2) // IClassFactory.Release

	var punk uintptr
	hr = callVtbl(factory, 3, 0, uintptr(unsafe.Pointer(&iidITaskbarList3)), uintptr(unsafe.Pointer(&punk)))
	if hr != 0 || punk == 0 {
		return 0, "", fmt.Errorf("%s IClassFactory.CreateInstance hr=0x%x", name, uint32(hr))
	}
	return punk, name, nil
}

func taskbarFromCoCreate(clsctx uintptr) (uintptr, string, error) {
	var punk uintptr
	hr, _, callErr := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidTaskbarList)),
		0,
		clsctx,
		uintptr(unsafe.Pointer(&iidITaskbarList3)),
		uintptr(unsafe.Pointer(&punk)),
	)
	if hr != 0 || punk == 0 {
		return 0, "", fmt.Errorf("CoCreateInstance ctx=0x%x hr=0x%x err=%v", clsctx, uint32(hr), callErr)
	}
	return punk, "CoCreateInstance", nil
}

func overlayIcon(slot int, rgb overlayRGB) uintptr {
	if overlayIcons[slot] != 0 {
		return overlayIcons[slot]
	}
	h := createDotIcon(rgb)
	overlayIcons[slot] = h
	return h
}

func overlayPixelSize() int {
	n, _, _ := procGetSystemMetrics.Call(smCXSMICON)
	if n < 16 {
		return 16
	}
	if n > 48 {
		return 48
	}
	return int(n)
}

func createDotIcon(rgb overlayRGB) uintptr {
	n := overlayPixelSize()
	var bits unsafe.Pointer
	header := bitmapInfoHeader{
		Size:     40,
		Width:    int32(n),
		Height:   -int32(n),
		Planes:   1,
		BitCount: 32,
	}
	hdc, _, _ := procGetDC.Call(0)
	hbm, _, _ := procCreateDIBSection.Call(
		hdc,
		uintptr(unsafe.Pointer(&header)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if hdc != 0 {
		procReleaseDC.Call(0, hdc)
	}
	if hbm == 0 || bits == nil {
		return 0
	}
	pix := unsafe.Slice((*byte)(bits), n*n*4)
	cx := float64(n)/2 - 0.5
	cy := cx
	radius := float64(n) * 0.28
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			d := math.Hypot(float64(x)-cx, float64(y)-cy)
			var cover float64
			switch {
			case d <= radius-0.55:
				cover = 1
			case d < radius+0.55:
				cover = (radius + 0.55 - d) / 1.1
			default:
				continue
			}
			i := (y*n + x) * 4
			pix[i+0] = byte(float64(rgb.b) * cover)
			pix[i+1] = byte(float64(rgb.g) * cover)
			pix[i+2] = byte(float64(rgb.r) * cover)
			pix[i+3] = byte(cover * 255)
		}
	}

	maskBytes := (n + 7) / 8 * n
	maskBits := make([]byte, maskBytes)
	hbmMask, _, _ := procCreateBitmap.Call(uintptr(n), uintptr(n), 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	info := iconInfo{
		FIcon:    1,
		HbmMask:  hbmMask,
		HbmColor: hbm,
	}
	hicon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	procDeleteObject.Call(hbm)
	if hbmMask != 0 {
		procDeleteObject.Call(hbmMask)
	}
	return hicon
}

func taskbarHWND(window fyne.Window) uintptr {
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
	if hwnd == 0 {
		return 0
	}
	root, _, _ := procGetAncestor.Call(hwnd, gaRoot)
	if root != 0 {
		return root
	}
	return hwnd
}

func overlayVtbl(obj uintptr) *[32]uintptr {
	lpVtbl := *(*uintptr)(unsafe.Pointer(obj))
	return (*[32]uintptr)(unsafe.Pointer(lpVtbl))
}

func callVtbl(obj uintptr, idx int, args ...uintptr) uintptr {
	if obj == 0 {
		return 0
	}
	all := make([]uintptr, 0, 1+len(args))
	all = append(all, obj)
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(overlayVtbl(obj)[idx], all...)
	return r
}
