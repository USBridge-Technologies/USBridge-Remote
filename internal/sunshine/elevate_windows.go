//go:build windows

package sunshine

import (
	"fmt"
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modShell32          = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = modShell32.NewProc("ShellExecuteExW")
)

const (
	seeMaskNoCloseProcess = 0x00000040
	swShowNormal          = 1
)

// shellExecuteInfo mirrors SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	cbSize         uint32
	fMask          uint32
	hwnd           uintptr
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       uintptr
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      uintptr
	dwHotKey       uint32
	hIconOrMonitor uintptr
	hProcess       uintptr
}

// launchElevated launches exePath via ShellExecuteExW with the "runas" verb,
// triggering a UAC prompt. Returns the process HANDLE (as uintptr) which the
// caller must pass to terminateElevated when done.
func launchElevated(exePath string) (uintptr, error) {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return 0, fmt.Errorf("encode verb: %w", err)
	}
	file, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return 0, fmt.Errorf("encode path: %w", err)
	}
	sei := shellExecuteInfo{
		fMask:  seeMaskNoCloseProcess,
		lpVerb: verb,
		lpFile: file,
		nShow:  swShowNormal,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))
	r, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if r == 0 {
		return 0, fmt.Errorf("ShellExecuteExW: %w", err)
	}
	return sei.hProcess, nil
}

// terminateElevated kills the elevated process and closes the handle.
func terminateElevated(handle uintptr) {
	if handle == 0 {
		return
	}
	h := windows.Handle(handle)
	_ = windows.TerminateProcess(h, 1)
	_ = windows.CloseHandle(h)
}

// waitElevatedAsync waits in a goroutine for the elevated process to exit,
// then logs and calls onExit (which must be safe to call from any goroutine).
func waitElevatedAsync(handle uintptr, onExit func()) {
	go func() {
		if handle == 0 {
			return
		}
		h := windows.Handle(handle)
		_, _ = windows.WaitForSingleObject(h, windows.INFINITE)
		log.Printf("[sunshine] elevated process exited")
		_ = windows.CloseHandle(h)
		if onExit != nil {
			onExit()
		}
	}()
}

// elevatedRunning returns true if the elevated process handle is still alive.
func elevatedRunning(handle uintptr) bool {
	if handle == 0 {
		return false
	}
	h := windows.Handle(handle)
	r, _ := windows.WaitForSingleObject(h, 0) // 0 = non-blocking check
	return r == uint32(windows.WAIT_TIMEOUT)
}
