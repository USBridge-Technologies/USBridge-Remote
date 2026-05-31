//go:build windows

package platform

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winmm              = syscall.NewLazyDLL("winmm.dll")
	procJoyGetNumDevs  = winmm.NewProc("joyGetNumDevs")
	procJoyGetPos      = winmm.NewProc("joyGetPos")
	procJoyGetDevCapsW = winmm.NewProc("joyGetDevCapsW")
)

// joyInfo matches the Win32 JOYINFO struct (used only to check if connected).
type joyInfo struct {
	wXpos    uint32
	wYpos    uint32
	wZpos    uint32
	wButtons uint32
}

// joyCapsW matches the Win32 JOYCAPSW struct (size = 728 bytes on 32/64-bit Windows).
type joyCapsW struct {
	wMid        uint16
	wPid        uint16
	szPname     [32]uint16 // MAXPNAMELEN=32 WCHARs
	wXmin       uint32
	wXmax       uint32
	wYmin       uint32
	wYmax       uint32
	wZmin       uint32
	wZmax       uint32
	wNumButtons uint32
	wPeriodMin  uint32
	wPeriodMax  uint32
	wRmin       uint32
	wRmax       uint32
	wUmin       uint32
	wUmax       uint32
	wVmin       uint32
	wVmax       uint32
	wCaps       uint32
	wMaxAxes    uint32
	wNumAxes    uint32
	wMaxButtons uint32
	szRegKey    [32]uint16  // MAXPNAMELEN=32 WCHARs
	szOEMVxD   [260]uint16 // MAX_JOYSTICK_OEM_VXDNAME=260 WCHARs
}

const (
	joyErrNoError = 0
	mmSysErrBase  = 96
)

// GamepadDevice describes a system gamepad.
type GamepadDevice struct {
	ID        string
	Name      string
	VendorID  string // e.g. "0x045e" from WinMM caps.wMid
	ProductID string // e.g. "0x028e" from WinMM caps.wPid
}

// EnumerateGamepads returns all gamepads currently connected to the system.
// On Windows it uses the WinMM joystick API which enumerates both XInput and DirectInput devices.
func EnumerateGamepads() []GamepadDevice {
	numDevs, _, _ := procJoyGetNumDevs.Call()
	if numDevs == 0 {
		return nil
	}

	var result []GamepadDevice
	for joyID := uintptr(0); joyID < numDevs; joyID++ {
		if !winmmJoystickConnected(joyID) {
			continue
		}
		name := winmmJoystickName(joyID)
		if name == "" {
			name = fmt.Sprintf("Gamepad %d", joyID+1)
		}
		vid, pid := winmmJoystickVIDPID(joyID)
		result = append(result, GamepadDevice{
			ID:        fmt.Sprintf("winmm:%d", joyID),
			Name:      name,
			VendorID:  vid,
			ProductID: pid,
		})
	}
	return result
}

func winmmJoystickConnected(joyID uintptr) bool {
	var info joyInfo
	ret, _, _ := procJoyGetPos.Call(joyID, uintptr(unsafe.Pointer(&info)))
	return ret == joyErrNoError
}

func winmmJoystickName(joyID uintptr) string {
	var caps joyCapsW
	ret, _, _ := procJoyGetDevCapsW.Call(
		joyID,
		uintptr(unsafe.Pointer(&caps)),
		unsafe.Sizeof(caps),
	)
	if ret != joyErrNoError {
		return ""
	}
	return windows.UTF16ToString(caps.szPname[:])
}

func winmmJoystickVIDPID(joyID uintptr) (vid, pid string) {
	var caps joyCapsW
	ret, _, _ := procJoyGetDevCapsW.Call(
		joyID,
		uintptr(unsafe.Pointer(&caps)),
		unsafe.Sizeof(caps),
	)
	if ret != joyErrNoError {
		return "", ""
	}
	return fmt.Sprintf("0x%04x", uint16(caps.wMid)), fmt.Sprintf("0x%04x", uint16(caps.wPid))
}
