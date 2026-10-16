//go:build windows

package app

import (
	"log"

	"golang.org/x/sys/windows"
)

// raiseEventName is a per-session (Local\) auto-reset event: a second
// launch sets it, the running GUI shows and focuses its window. Until
// 2026-10-16 both functions here were no-ops on the assumption that the
// user would reopen the window from the tray -- in practice a Start menu
// launch while the service's tray helper ran just exited, so the user saw
// a tray icon and no window.
const raiseEventName = `Local\USBridgeAgentRaiseGUI`

var user32AllowSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")

const asfwAny = ^uintptr(0) // ASFW_ANY

func watchRaiseRequests() {
	name, _ := windows.UTF16PtrFromString(raiseEventName)
	ev, err := windows.CreateEvent(nil, 0, 0, name)
	if err != nil {
		log.Printf("[app] raise event: %v -- a second launch will not bring this window forward", err)
		return
	}
	go func() {
		for {
			if r, err := windows.WaitForSingleObject(ev, windows.INFINITE); err != nil || r != windows.WAIT_OBJECT_0 {
				return
			}
			raiseThisGUI()
		}
	}()
}

// raiseOtherGUI signals the running GUI. pid is unused: Windows' byte-range
// lock keeps gui.lock unreadable to us, so it is usually 0 anyway.
func raiseOtherGUI(int) {
	name, _ := windows.UTF16PtrFromString(raiseEventName)
	ev, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		log.Printf("[app] raise event: %v", err)
		return
	}
	defer windows.CloseHandle(ev)
	// This launch is the foreground process (the user just started it);
	// hand that right on so the other GUI's window can actually come to
	// the front instead of only flashing in the taskbar.
	_, _, _ = user32AllowSetForegroundWindow.Call(asfwAny)
	_ = windows.SetEvent(ev)
}
