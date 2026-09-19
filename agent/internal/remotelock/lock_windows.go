//go:build windows

package remotelock

import (
	"log"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14
	hcAction     = 0
	wmQuit       = 0x0012
	wmNCHitTest  = 0x0084
	htMinButton  = 8

	llmhfInjected = 0x00000001
	llkhfInjected = 0x00000010

	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonDown   = 0x0204
	wmRButtonUp     = 0x0205
	wmRButtonDblClk = 0x0206
	wmMButtonDown   = 0x0207
	wmMButtonUp     = 0x0208
	wmMButtonDblClk = 0x0209
	wmMouseWheel    = 0x020A
	wmXButtonDown   = 0x020B
	wmXButtonUp     = 0x020C
	wmMouseHWheel   = 0x020E
	smtoAbortIfHung = 0x0002
	gaRoot          = 2
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookExW        = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx      = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx           = user32.NewProc("CallNextHookEx")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW       = user32.NewProc("PostThreadMessageW")
	procWindowFromPoint          = user32.NewProc("WindowFromPoint")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetAncestor              = user32.NewProc("GetAncestor")
	procSendMessageTimeoutW      = user32.NewProc("SendMessageTimeoutW")
)

type point struct {
	X, Y int32
}

type msllhookstruct struct {
	Pt        point
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type kbdllhookstruct struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

var (
	mouseCallback    = syscall.NewCallback(lowLevelMouseProc)
	keyboardCallback = syscall.NewCallback(lowLevelKeyboardProc)

	hookMu     sync.Mutex
	mouseHook  uintptr
	keyHook    uintptr
	hookThread uint32
	ourPID     = uint32(os.Getpid())
)

func setHookEnabled(on bool) {
	if on {
		startHook()
		return
	}
	stopHook()
}

func hookInstalled() bool {
	hookMu.Lock()
	defer hookMu.Unlock()
	return mouseHook != 0
}

func setX11Window(uintptr) {}

func startHook() {
	hookMu.Lock()
	already := mouseHook != 0
	hookMu.Unlock()
	if already {
		return
	}

	ready := make(chan bool, 1)
	go func() {
		runtime.LockOSThread()
		mh, _, err := procSetWindowsHookExW.Call(whMouseLL, mouseCallback, 0, 0)
		if mh == 0 {
			log.Printf("[remotelock] SetWindowsHookExW(mouse) failed: %v", err)
			ready <- false
			return
		}
		kh, _, err := procSetWindowsHookExW.Call(whKeyboardLL, keyboardCallback, 0, 0)
		if kh == 0 {
			log.Printf("[remotelock] SetWindowsHookExW(keyboard) failed: %v", err)
			procUnhookWindowsHookEx.Call(mh)
			ready <- false
			return
		}
		hookMu.Lock()
		if mouseHook != 0 {
			// Lost the race: another start already installed hooks.
			hookMu.Unlock()
			procUnhookWindowsHookEx.Call(mh)
			procUnhookWindowsHookEx.Call(kh)
			ready <- true
			return
		}
		mouseHook = mh
		keyHook = kh
		hookThread = windows.GetCurrentThreadId()
		hookMu.Unlock()
		ready <- true
		log.Printf("[remotelock] blocking injected input on this process's windows")

		var m msg
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			r := int32(ret)
			if r == 0 || r == -1 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}

		hookMu.Lock()
		if mouseHook == mh {
			procUnhookWindowsHookEx.Call(mouseHook)
			mouseHook = 0
		}
		if keyHook == kh {
			procUnhookWindowsHookEx.Call(keyHook)
			keyHook = 0
		}
		if hookThread == windows.GetCurrentThreadId() {
			hookThread = 0
		}
		hookMu.Unlock()
	}()
	<-ready
}

func stopHook() {
	hookMu.Lock()
	tid := hookThread
	mh, kh := mouseHook, keyHook
	mouseHook, keyHook, hookThread = 0, 0, 0
	hookMu.Unlock()
	if mh != 0 {
		procUnhookWindowsHookEx.Call(mh)
	}
	if kh != 0 {
		procUnhookWindowsHookEx.Call(kh)
	}
	if tid != 0 {
		procPostThreadMessageW.Call(uintptr(tid), wmQuit, 0, 0)
	}
}

func lowLevelMouseProc(nCode, wParam, lParam uintptr) uintptr {
	if int32(nCode) == hcAction && isArmed() && lParam != 0 && isMouseButtonOrWheel(wParam) {
		info := (*msllhookstruct)(unsafe.Pointer(lParam))
		if info.Flags&llmhfInjected != 0 && windowIsOursAt(info.Pt) && !isMinButton(info.Pt) {
			return 1
		}
	}
	return callNext(nCode, wParam, lParam)
}

func lowLevelKeyboardProc(nCode, wParam, lParam uintptr) uintptr {
	if int32(nCode) == hcAction && isArmed() && lParam != 0 {
		info := (*kbdllhookstruct)(unsafe.Pointer(lParam))
		if info.Flags&llkhfInjected != 0 && foregroundIsOurs() {
			return 1
		}
	}
	return callNext(nCode, wParam, lParam)
}

func isMouseButtonOrWheel(wParam uintptr) bool {
	switch wParam {
	case wmLButtonDown, wmLButtonUp, wmLButtonDblClk,
		wmRButtonDown, wmRButtonUp, wmRButtonDblClk,
		wmMButtonDown, wmMButtonUp, wmMButtonDblClk,
		wmXButtonDown, wmXButtonUp,
		wmMouseWheel, wmMouseHWheel:
		return true
	}
	return false
}

func windowIsOursAt(pt point) bool {
	hwnd, _, _ := procWindowFromPoint.Call(pointArg(pt))
	return hwndBelongsToUs(hwnd)
}

func foregroundIsOurs() bool {
	hwnd, _, _ := procGetForegroundWindow.Call()
	return hwndBelongsToUs(hwnd)
}

func hwndBelongsToUs(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid == ourPID
}

func isMinButton(pt point) bool {
	hwnd, _, _ := procWindowFromPoint.Call(pointArg(pt))
	if hwnd == 0 {
		return false
	}
	root, _, _ := procGetAncestor.Call(hwnd, gaRoot)
	if root == 0 {
		root = hwnd
	}
	lParam := uintptr(uint32(uint16(pt.X)) | uint32(uint16(pt.Y))<<16)
	var hit uintptr
	ret, _, _ := procSendMessageTimeoutW.Call(
		root, wmNCHitTest, 0, lParam, smtoAbortIfHung, 50,
		uintptr(unsafe.Pointer(&hit)),
	)
	return ret != 0 && int32(hit) == htMinButton
}

func pointArg(pt point) uintptr {
	return uintptr(uint64(uint32(pt.X)) | uint64(uint32(pt.Y))<<32)
}

func callNext(nCode, wParam, lParam uintptr) uintptr {
	r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
	return r
}
