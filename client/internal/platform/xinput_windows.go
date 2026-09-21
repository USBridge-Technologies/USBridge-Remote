//go:build windows

package platform

import (
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Thin XInput layer shared by gamepad enumeration, capture and rumble.
//
// Xbox-class pads (Xbox 360, One, Series and clones such as the Razer
// Wolverine V2) are XInput devices. WinMM/DirectInput reports their two
// triggers as one combined axis, so they must be read through XInput to get
// separate, full-range triggers and the Guide button.

var (
	modXInput          = windows.NewLazySystemDLL("xinput1_4.dll")
	procXInputGetState = modXInput.NewProc("XInputGetState")
	procXInputSetState = modXInput.NewProc("XInputSetState")

	xinputOrdinalsOnce sync.Once
	procGetStateEx     uintptr // XInputGetStateEx, ordinal 100: includes the Guide button
	procGetCapsEx      uintptr // XInputGetCapabilitiesEx, ordinal 108: VID/PID
)

// xinputMaxSlots is XUSER_MAX_COUNT.
const xinputMaxSlots = 4

// xinputGamepad mirrors XINPUT_GAMEPAD. wButtons already uses Moonlight's
// button layout, and triggers (0..255) and stick Y (positive is up) match too.
type xinputGamepad struct {
	Buttons uint16
	LT, RT  uint8
	LX, LY  int16
	RX, RY  int16
}

// xinputState mirrors XINPUT_STATE.
type xinputState struct {
	Packet uint32
	Pad    xinputGamepad
}

// xinputCapsEx mirrors XINPUT_CAPABILITIES_EX.
type xinputCapsEx struct {
	Type, SubType uint8
	Flags         uint16
	Pad           xinputGamepad
	VibL, VibR    uint16
	VendorID      uint16
	ProductID     uint16
	RevisionID    uint16
	_             uint32
}

func resolveXInputOrdinals() {
	xinputOrdinalsOnce.Do(func() {
		if err := modXInput.Load(); err != nil {
			return
		}
		h := windows.Handle(modXInput.Handle())
		if p, err := windows.GetProcAddressByOrdinal(h, 100); err == nil {
			procGetStateEx = p
		}
		if p, err := windows.GetProcAddressByOrdinal(h, 108); err == nil {
			procGetCapsEx = p
		}
	})
}

// xinputGet reads one XInput slot. It uses XInputGetStateEx when the DLL has
// it (adds the Guide button) and the documented XInputGetState otherwise.
func xinputGet(slot int) (xinputState, bool) {
	resolveXInputOrdinals()
	var s xinputState
	var r uintptr
	if procGetStateEx != 0 {
		r, _, _ = syscall.SyscallN(procGetStateEx, uintptr(slot), uintptr(unsafe.Pointer(&s)))
	} else {
		r, _, _ = procXInputGetState.Call(uintptr(slot), uintptr(unsafe.Pointer(&s)))
	}
	return s, r == 0
}

func xinputConnected(slot int) bool {
	_, ok := xinputGet(slot)
	return ok
}

// xinputVIDPID returns a connected slot's USB vendor/product id, or ok=false
// when the DLL cannot tell (older Windows) or the slot is empty.
func xinputVIDPID(slot int) (vid, pid uint16, ok bool) {
	resolveXInputOrdinals()
	if procGetCapsEx == 0 {
		return 0, 0, false
	}
	var c xinputCapsEx
	// XInputGetCapabilitiesEx(1, userIndex, flags, caps): the leading 1 is the
	// API version constant the DLL expects.
	r, _, _ := syscall.SyscallN(procGetCapsEx, 1, uintptr(slot), 0, uintptr(unsafe.Pointer(&c)))
	if r != 0 {
		return 0, 0, false
	}
	return c.VendorID, c.ProductID, true
}

type xinputVibration struct{ Left, Right uint16 }

func xinputSetVibration(slot int, low, high uint16) {
	v := xinputVibration{Left: low, Right: high}
	procXInputSetState.Call(uintptr(slot), uintptr(unsafe.Pointer(&v)))
}

// xinputToCapture converts an XInput state to the capture state Moonlight
// expects; the layouts already agree, so this is a field copy.
func xinputToCapture(p xinputGamepad) GamepadCaptureState {
	return GamepadCaptureState{
		Buttons:      p.Buttons,
		LeftX:        p.LX,
		LeftY:        p.LY,
		RightX:       p.RX,
		RightY:       p.RY,
		LeftTrigger:  p.LT,
		RightTrigger: p.RT,
	}
}
