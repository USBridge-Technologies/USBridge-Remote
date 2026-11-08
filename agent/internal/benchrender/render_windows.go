//go:build windows

package benchrender

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procPeekMessageW                  = user32.NewProc("PeekMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procGetDC                         = user32.NewProc("GetDC")
	procReleaseDC                     = user32.NewProc("ReleaseDC")
	procBeginPaint                    = user32.NewProc("BeginPaint")
	procEndPaint                      = user32.NewProc("EndPaint")
	procFillRect                      = user32.NewProc("FillRect")
	procGetClientRect                 = user32.NewProc("GetClientRect")
	procGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	procSetCursor                     = user32.NewProc("SetCursor")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procCreateSolidBrush              = gdi32.NewProc("CreateSolidBrush")
	procGdiFlush                      = gdi32.NewProc("GdiFlush")
)

const (
	wsPopup      = 0x80000000
	wsVisible    = 0x10000000
	wsExTopmost  = 0x00000008
	csOwnDC      = 0x0020
	pmRemove     = 0x0001
	smCxScreen   = 0
	smCyScreen   = 1
	wmDestroy    = 0x0002
	wmClose      = 0x0010
	wmPaint      = 0x000F
	wmEraseBkgnd = 0x0014
	wmSetCursor  = 0x0020
	wmKeyDown    = 0x0100
	wmQuit       = 0x0012
	vkEscape     = 0x1B
	htClient     = 1

	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2: window coordinates are
	// physical pixels, the same ones internal/monitors reports.
	dpiPerMonitorAwareV2 = ^uintptr(3) // (DPI_AWARENESS_CONTEXT)-4
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type winRect struct{ Left, Top, Right, Bottom int32 }

type winMsg struct {
	Hwnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

type paintStruct struct {
	Hdc         windows.Handle
	Erase       int32
	Paint       winRect
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

// colorref turns 0xRRGGBB into GDI's 0x00BBGGRR.
func colorref(rgb uint32) uintptr {
	return uintptr((rgb>>16)&0xff | (rgb & 0xff00) | (rgb&0xff)<<16)
}

func toRect(b box) winRect {
	return winRect{int32(b.x), int32(b.y), int32(b.x + b.w), int32(b.y + b.h)}
}

// renderer is the window's state; the window procedure reads it (on the
// same, locked thread).
type renderer struct {
	hwnd     windows.HWND
	bg, a, b uintptr // brushes
	white    uintptr
	black    uintptr
	cur      [2]box
	// The barcode cells on screen, repainted only where they change.
	bar      [barcodeCells]bool
	barDrawn bool
	w, h     int
}

// paintBarcode draws the barcode for the clock reading now.
func (r *renderer) paintBarcode(hdc uintptr, now time.Time) {
	bits := barcodeBits(now)
	for i, white := range bits {
		if r.barDrawn && white == r.bar[i] {
			continue
		}
		brush := r.black
		if white {
			brush = r.white
		}
		r.fill(hdc, barcodeBox(i, r.w, r.h), brush)
	}
	r.bar, r.barDrawn = bits, true
}

func (r *renderer) fill(hdc uintptr, b box, brush uintptr) {
	rc := toRect(b)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
}

func (r *renderer) paintAll(hdc uintptr) {
	var rc winRect
	procGetClientRect.Call(uintptr(r.hwnd), uintptr(unsafe.Pointer(&rc)))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), r.bg)
	r.fill(hdc, r.cur[0], r.a)
	r.fill(hdc, r.cur[1], r.b)
	r.w, r.h = int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)
	r.barDrawn = false
	r.paintBarcode(hdc, time.Now())
}

func run(o options) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if procSetProcessDpiAwarenessContext.Find() == nil {
		procSetProcessDpiAwarenessContext.Call(dpiPerMonitorAwareV2)
	}
	area := o.rect
	if area == nil {
		w, _, _ := procGetSystemMetrics.Call(smCxScreen)
		h, _, _ := procGetSystemMetrics.Call(smCyScreen)
		area = &Rect{0, 0, int(w), int(h)}
	}

	r := &renderer{}
	r.bg, _, _ = procCreateSolidBrush.Call(colorref(colorBackground))
	r.a, _, _ = procCreateSolidBrush.Call(colorref(colorBoxA))
	r.b, _, _ = procCreateSolidBrush.Call(colorref(colorBoxB))
	r.white, _, _ = procCreateSolidBrush.Call(colorref(0xffffff))
	r.black, _, _ = procCreateSolidBrush.Call(colorref(0x000000))
	r.cur = boxes(0, area.W, area.H)

	quit := false
	wndProc := windows.NewCallback(func(hwnd windows.HWND, msg uint32, wp, lp uintptr) uintptr {
		switch msg {
		case wmEraseBkgnd:
			return 1
		case wmPaint:
			var ps paintStruct
			hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
			r.paintAll(hdc)
			procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
			return 0
		case wmSetCursor:
			if lp&0xffff == htClient {
				procSetCursor.Call(0)
				return 1
			}
		case wmKeyDown:
			if wp == vkEscape {
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			}
		case wmClose:
			procDestroyWindow.Call(uintptr(hwnd))
			return 0
		case wmDestroy:
			quit = true
			procPostQuitMessage.Call(0)
			return 0
		}
		ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wp, lp)
		return ret
	})

	className, _ := windows.UTF16PtrFromString("USBridgeBenchRender")
	title, _ := windows.UTF16PtrFromString(WindowTitle)
	var inst windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &inst)
	wc := wndClassEx{
		Style:     csOwnDC,
		WndProc:   wndProc,
		Instance:  inst,
		ClassName: className,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return fmt.Errorf("RegisterClassEx: %v", err)
	}
	hwnd, _, err := procCreateWindowExW.Call(
		wsExTopmost,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup|wsVisible,
		uintptr(area.X), uintptr(area.Y), uintptr(area.W), uintptr(area.H),
		0, 0, uintptr(inst), 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %v", err)
	}
	r.hwnd = windows.HWND(hwnd)
	// CS_OWNDC: the window keeps this DC for its lifetime.
	hdc, _, _ := procGetDC.Call(hwnd)
	if hdc == 0 {
		return errors.New("GetDC failed")
	}
	defer procReleaseDC.Call(hwnd, hdc)

	period := time.Second / time.Duration(o.fps)
	start := time.Now()
	next := start
	var msg winMsg
	for {
		for {
			ok, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, pmRemove)
			if ok == 0 {
				break
			}
			if msg.Message == wmQuit {
				return nil
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}
		if quit {
			return nil
		}

		var rc winRect
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		nb := boxes(time.Since(start).Seconds(), int(rc.Right-rc.Left), int(rc.Bottom-rc.Top))
		// Only the two boxes' old and new rectangles change: erase the old
		// ones, draw the new ones.
		r.fill(hdc, r.cur[0], r.bg)
		r.fill(hdc, r.cur[1], r.bg)
		r.cur = nb
		r.fill(hdc, r.cur[0], r.a)
		r.fill(hdc, r.cur[1], r.b)
		r.w, r.h = int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)
		r.paintBarcode(hdc, time.Now())
		procGdiFlush.Call()

		next = next.Add(period)
		if d := time.Until(next); d > 0 {
			time.Sleep(d)
		} else if d < -4*period {
			next = time.Now() // fell far behind (window moved, machine busy): don't race to catch up
		}
	}
}
