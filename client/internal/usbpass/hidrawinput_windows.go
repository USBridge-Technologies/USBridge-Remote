//go:build windows

package usbpass

// Raw Input source. hid.dll cannot read the collections Windows opens
// exclusively (pens, digitizers, mice, keyboards), but the Raw Input API
// (WM_INPUT) delivers the raw HID reports of those devices to any process that
// registers for their usage pages, without taking the device from the OS.
//
// A registered window must pump messages, so this owns one message-only window
// on a dedicated, OS-locked goroutine.

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")

	procRegisterClassExW        = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW         = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow           = modUser32.NewProc("DestroyWindow")
	procDefWindowProcW          = modUser32.NewProc("DefWindowProcW")
	procGetMessageW             = modUser32.NewProc("GetMessageW")
	procPostMessageW            = modUser32.NewProc("PostMessageW")
	procDispatchMessageW        = modUser32.NewProc("DispatchMessageW")
	procRegisterRawInputDevices = modUser32.NewProc("RegisterRawInputDevices")
	procGetRawInputData         = modUser32.NewProc("GetRawInputData")
	procGetRawInputDeviceInfoW  = modUser32.NewProc("GetRawInputDeviceInfoW")
	procUnregisterClassW        = modUser32.NewProc("UnregisterClassW")
	modKernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procGetModuleHandleW        = modKernel32.NewProc("GetModuleHandleW")
)

const (
	wmInput = 0x00FF
	wmClose = 0x0010
	wmQuit  = 0x0012
	wmApp   = 0x8000
	hwndMsg = ^uintptr(2) // HWND_MESSAGE == (HWND)-3

	ridInput        = 0x10000003
	ridiDeviceName  = 0x20000007
	ridevInputSink  = 0x00000100
	ridevPageOnly   = 0x00000020
	rimTypeMouse    = 0
	rimTypeKeyboard = 1
	rimTypeHID      = 2

	rawHeaderSize = 24 // RAWINPUTHEADER on x64
)

type rawInputDevice struct {
	UsagePage uint16
	Usage     uint16
	Flags     uint32
	Target    uintptr
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// rawInputEvent is one WM_INPUT record.
type rawInputEvent struct {
	Device  uintptr  // handle of the source device (stable while it stays plugged in)
	Path    string   // device interface path (\\?\HID#VID_056A&PID_0374&COL03#...)
	Type    uint32   // rimType*
	Reports [][]byte // RIM_TYPEHID: one entry per report, report ID first
	When    time.Time
}

// rawInputSource owns the window/thread. Stop() ends it.
type rawInputSource struct {
	hwnd    uintptr
	done    chan struct{}
	stopped sync.Once
	class   *uint16

	onEvent func(rawInputEvent)

	pathMu sync.Mutex
	paths  map[uintptr]string
}

var rawInputSourceForWnd sync.Map // hwnd -> *rawInputSource

var rawWndProc = syscall.NewCallback(func(hwnd, msg, wparam, lparam uintptr) uintptr {
	if msg == wmInput {
		if v, ok := rawInputSourceForWnd.Load(hwnd); ok {
			v.(*rawInputSource).handleWMInput(lparam)
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
})

// startRawInput registers for the given usage pages (whole pages) plus the
// listed exact usages and calls onEvent for every WM_INPUT on the source's own
// thread. Keep onEvent quick.
func startRawInput(pages []uint16, exact [][2]uint16, onEvent func(rawInputEvent)) (*rawInputSource, error) {
	s := &rawInputSource{done: make(chan struct{}), onEvent: onEvent, paths: map[uintptr]string{}}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(s.done)

		inst, _, _ := procGetModuleHandleW.Call(0)
		className, _ := windows.UTF16PtrFromString(fmt.Sprintf("USBridgeRawInput%d", time.Now().UnixNano()))
		s.class = className
		wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: rawWndProc, Instance: inst, ClassName: className}
		if r, _, e := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			ready <- fmt.Errorf("RegisterClassEx: %v", e)
			return
		}
		defer procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), inst)
		hwnd, _, e := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, hwndMsg, 0, inst, 0)
		if hwnd == 0 {
			ready <- fmt.Errorf("CreateWindowEx: %v", e)
			return
		}
		s.hwnd = hwnd
		rawInputSourceForWnd.Store(hwnd, s)
		defer rawInputSourceForWnd.Delete(hwnd)
		defer procDestroyWindow.Call(hwnd)

		var devs []rawInputDevice
		for _, p := range pages {
			devs = append(devs, rawInputDevice{UsagePage: p, Flags: ridevInputSink | ridevPageOnly, Target: hwnd})
		}
		for _, u := range exact {
			devs = append(devs, rawInputDevice{UsagePage: u[0], Usage: u[1], Flags: ridevInputSink, Target: hwnd})
		}
		if r, _, e := procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&devs[0])), uintptr(len(devs)), unsafe.Sizeof(devs[0])); r == 0 {
			ready <- fmt.Errorf("RegisterRawInputDevices: %v", e)
			return
		}
		ready <- nil

		var m winMsg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 { // WM_QUIT or error
				return
			}
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
	if err := <-ready; err != nil {
		<-s.done
		return nil, err
	}
	return s, nil
}

func (s *rawInputSource) Stop() {
	s.stopped.Do(func() {
		if s.hwnd != 0 {
			procPostMessageW.Call(s.hwnd, wmClose, 0, 0)
			// WM_CLOSE reaches DefWindowProc, which destroys the window; the
			// message loop ends on WM_QUIT.
			procPostMessageW.Call(s.hwnd, wmQuit, 0, 0)
		}
		select {
		case <-s.done:
		case <-time.After(2 * time.Second):
		}
	})
}

func (s *rawInputSource) devicePath(h uintptr) string {
	s.pathMu.Lock()
	p, ok := s.paths[h]
	s.pathMu.Unlock()
	if ok {
		return p
	}
	var n uint32
	procGetRawInputDeviceInfoW.Call(h, ridiDeviceName, 0, uintptr(unsafe.Pointer(&n)))
	if n > 0 {
		buf := make([]uint16, n+1)
		r, _, _ := procGetRawInputDeviceInfoW.Call(h, ridiDeviceName, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
		if int32(r) > 0 {
			p = windows.UTF16ToString(buf)
		}
	}
	s.pathMu.Lock()
	s.paths[h] = p
	s.pathMu.Unlock()
	return p
}

func (s *rawInputSource) handleWMInput(hRawInput uintptr) {
	var size uint32
	procGetRawInputData.Call(hRawInput, ridInput, 0, uintptr(unsafe.Pointer(&size)), rawHeaderSize)
	if size == 0 {
		return
	}
	buf := make([]byte, size)
	r, _, _ := procGetRawInputData.Call(hRawInput, ridInput, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), rawHeaderSize)
	if int32(r) <= 0 || int(size) < rawHeaderSize {
		return
	}
	typ := *(*uint32)(unsafe.Pointer(&buf[0]))
	dev := *(*uintptr)(unsafe.Pointer(&buf[8]))
	ev := rawInputEvent{Device: dev, Type: typ, When: time.Now(), Path: s.devicePath(dev)}
	if typ == rimTypeHID && int(size) >= rawHeaderSize+8 {
		sizeHid := int(*(*uint32)(unsafe.Pointer(&buf[rawHeaderSize])))
		count := int(*(*uint32)(unsafe.Pointer(&buf[rawHeaderSize+4])))
		data := buf[rawHeaderSize+8:]
		for i := 0; i < count && (i+1)*sizeHid <= len(data); i++ {
			ev.Reports = append(ev.Reports, append([]byte(nil), data[i*sizeHid:(i+1)*sizeHid]...))
		}
	}
	s.onEvent(ev)
}

// rawInputMatchesUSB reports whether a raw-input device path belongs to the
// given VID/PID (paths look like \\?\HID#VID_056A&PID_0374&COL03#7&...#{guid}).
func rawInputMatchesUSB(path string, vid, pid uint16) bool {
	u := strings.ToUpper(path)
	return strings.Contains(u, fmt.Sprintf("VID_%04X&PID_%04X", vid, pid))
}
