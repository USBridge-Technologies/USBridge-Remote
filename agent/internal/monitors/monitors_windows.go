//go:build windows

package monitors

import (
	"fmt"
	"runtime"
	"sort"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                           = windows.NewLazySystemDLL("user32.dll")
	procEnumDisplayMonitors          = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW              = user32.NewProc("GetMonitorInfoW")
	procEnumDisplayDevicesW          = user32.NewProc("EnumDisplayDevicesW")
	procEnumWindows                  = user32.NewProc("EnumWindows")
	procIsWindowVisible              = user32.NewProc("IsWindowVisible")
	procGetWindowRect                = user32.NewProc("GetWindowRect")
	procMonitorFromWindow            = user32.NewProc("MonitorFromWindow")
	procSetWindowPos                 = user32.NewProc("SetWindowPos")
	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
)

// DPI_AWARENESS_CONTEXT values (windef.h).
const (
	dpiContextUnaware          = ^uintptr(0)     // -1
	dpiContextPerMonitorAware2 = ^uintptr(4 - 1) // -4
)

const (
	monitorInfoFPrimary     = 0x1
	monitorDefaultToNearest = 0x2
	swpNoZOrder             = 0x0004
	swpNoActivate           = 0x0010
	swpShowWindow           = 0x0040
)

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfoEx struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
	SzDevice  [32]uint16
}

type displayDevice struct {
	Cb           uint32
	DeviceName   [32]uint16
	DeviceString [128]uint16
	StateFlags   uint32
	DeviceID     [128]uint16
	DeviceKey    [128]uint16
}

// withDPIContext runs f on a locked OS thread switched to the given DPI
// awareness, so the coordinates Windows hands back are in that space: this
// process's own awareness is whatever its GUI toolkit set, and a DPI-unaware
// caller gets virtualized (scaled) monitor rects instead of real pixels.
func withDPIContext(ctx uintptr, f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if procSetThreadDpiAwarenessContext.Find() == nil {
		if prev, _, _ := procSetThreadDpiAwarenessContext.Call(ctx); prev != 0 {
			defer procSetThreadDpiAwarenessContext.Call(prev)
		}
	}
	f()
}

// WithPhysicalDPI runs f on a locked OS thread switched to per-monitor-v2
// DPI awareness: Win32 geometry/cursor calls inside f (GetMonitorInfo,
// SetCursorPos) then work in physical pixels whatever this process's own
// awareness is. Mixing logical rects (1280x720 for a 4K panel at 300%) with a
// physical cursor space confined absolute mouse moves to a corner of the screen.
func WithPhysicalDPI(f func()) { withDPIContext(dpiContextPerMonitorAware2, f) }

func monitorInfo(h uintptr) (monitorInfoEx, bool) {
	mi := monitorInfoEx{CbSize: uint32(unsafe.Sizeof(monitorInfoEx{}))}
	ok, _, _ := procGetMonitorInfoW.Call(h, uintptr(unsafe.Pointer(&mi)))
	return mi, ok != 0
}

func enumMonitors() []monitorInfoEx {
	var out []monitorInfoEx
	cb := windows.NewCallback(func(h, _ uintptr, _ uintptr, _ uintptr) uintptr {
		if mi, ok := monitorInfo(h); ok {
			out = append(out, mi)
		}
		return 1
	})
	procEnumDisplayMonitors.Call(0, 0, cb, 0)
	return out
}

// monitorDescription is the monitor's own description from the display
// driver ("Generic PnP Monitor" on many machines, the model on others).
func monitorDescription(gdi string) string {
	name, err := windows.UTF16PtrFromString(gdi)
	if err != nil {
		return ""
	}
	dd := displayDevice{Cb: uint32(unsafe.Sizeof(displayDevice{}))}
	if ok, _, _ := procEnumDisplayDevicesW.Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&dd)), 0); ok == 0 {
		return ""
	}
	return windows.UTF16ToString(dd.DeviceString[:])
}

// List returns the active monitors, primary first, then top-to-bottom and
// left-to-right. Coordinates are physical pixels.
func List() ([]Monitor, error) {
	if snap, ok := sessionSnapshot(); ok {
		return snap.Monitors, nil
	}
	return listLocal()
}

func listLocal() ([]Monitor, error) {
	var infos []monitorInfoEx
	withDPIContext(dpiContextPerMonitorAware2, func() { infos = enumMonitors() })
	if len(infos) == 0 {
		return nil, fmt.Errorf("no monitors found")
	}
	out := make([]Monitor, 0, len(infos))
	for _, mi := range infos {
		id := windows.UTF16ToString(mi.SzDevice[:])
		out = append(out, Monitor{
			ID:      id,
			Name:    monitorDescription(id),
			X:       int(mi.RcMonitor.Left),
			Y:       int(mi.RcMonitor.Top),
			Width:   int(mi.RcMonitor.Right - mi.RcMonitor.Left),
			Height:  int(mi.RcMonitor.Bottom - mi.RcMonitor.Top),
			Primary: mi.DwFlags&monitorInfoFPrimary != 0,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Primary != out[j].Primary {
			return out[i].Primary
		}
		if out[i].Y != out[j].Y {
			return out[i].Y < out[j].Y
		}
		return out[i].X < out[j].X
	})
	return out, nil
}

// GDINames is every active monitor's GDI name (`\.\DISPLAYn`) in
// EnumDisplayMonitors order -- unsorted, the order kbinani/screenshot indexes
// displays in, so its index i is GDINames()[i].
func GDINames() []string {
	if snap, ok := sessionSnapshot(); ok {
		return snap.GDI
	}
	return gdiNamesLocal()
}

func gdiNamesLocal() []string {
	var infos []monitorInfoEx
	withDPIContext(dpiContextPerMonitorAware2, func() { infos = enumMonitors() })
	out := make([]string, 0, len(infos))
	for _, mi := range infos {
		out = append(out, windows.UTF16ToString(mi.SzDevice[:]))
	}
	return out
}

// LogicalOrigin is the monitor's top-left corner as a DPI-unaware program
// sees it -- the space ffplay's -left/-top (SDL) positions its window in.
// With display scaling those are not the physical pixels List reports.
func LogicalOrigin(id string) (x, y int, ok bool) {
	if snap, have := sessionSnapshot(); have {
		o, found := snap.Logical[id]
		return o[0], o[1], found
	}
	return logicalOriginLocal(id)
}

func logicalOriginLocal(id string) (x, y int, ok bool) {
	withDPIContext(dpiContextUnaware, func() {
		for _, mi := range enumMonitors() {
			if windows.UTF16ToString(mi.SzDevice[:]) == id {
				x, y, ok = int(mi.RcMonitor.Left), int(mi.RcMonitor.Top), true
				return
			}
		}
	})
	return x, y, ok
}

// processWindows returns pid's visible top-level windows, largest first.
func processWindows(pid int) []windows.HWND {
	type win struct {
		h    windows.HWND
		area int64
	}
	var found []win
	cb := windows.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
		var owner uint32
		if _, err := windows.GetWindowThreadProcessId(h, &owner); err != nil || int(owner) != pid {
			return 1
		}
		if v, _, _ := procIsWindowVisible.Call(uintptr(h)); v == 0 {
			return 1
		}
		var r rect
		procGetWindowRect.Call(uintptr(h), uintptr(unsafe.Pointer(&r)))
		found = append(found, win{h, int64(r.Right-r.Left) * int64(r.Bottom-r.Top)})
		return 1
	})
	procEnumWindows.Call(cb, 0)
	sort.Slice(found, func(i, j int) bool { return found[i].area > found[j].area })
	out := make([]windows.HWND, len(found))
	for i, w := range found {
		out[i] = w.h
	}
	return out
}

// ProcessMonitor returns the monitor showing pid's main (largest visible)
// window; false while it has no visible window yet, or when it runs where
// this process can't see its windows (another session).
func ProcessMonitor(pid int) (string, bool) {
	var id string
	var ok bool
	withDPIContext(dpiContextPerMonitorAware2, func() {
		wins := processWindows(pid)
		if len(wins) == 0 {
			return
		}
		h, _, _ := procMonitorFromWindow.Call(uintptr(wins[0]), monitorDefaultToNearest)
		if mi, got := monitorInfo(h); got {
			id, ok = windows.UTF16ToString(mi.SzDevice[:]), true
		}
	})
	return id, ok
}

// MoveProcessWindows moves pid's main window to cover m entirely -- for a
// player that opened fullscreen on the wrong monitor.
func MoveProcessWindows(pid int, m Monitor) error {
	var err error
	withDPIContext(dpiContextPerMonitorAware2, func() {
		wins := processWindows(pid)
		if len(wins) == 0 {
			err = fmt.Errorf("process %d has no visible window", pid)
			return
		}
		r, _, callErr := procSetWindowPos.Call(uintptr(wins[0]), 0, uintptr(m.X), uintptr(m.Y), uintptr(m.Width), uintptr(m.Height), swpNoZOrder|swpNoActivate|swpShowWindow)
		if r == 0 {
			err = fmt.Errorf("SetWindowPos: %v", callErr)
		}
	})
	return err
}
