//go:build windows

package usbpass

// XInput source for the synthetic Xbox 360 controller: on a Windows client any
// gamepad the OS exposes through XInput (Xbox 360, Xbox One and Series pads on
// their inbox drivers, and many third-party pads) is read here, which is why a
// pad like an Xbox One controller no longer needs a raw USB passthrough.

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modXInput          = windows.NewLazySystemDLL("xinput1_4.dll")
	procXInputGetState = modXInput.NewProc("XInputGetState")
	procXInputSetState = modXInput.NewProc("XInputSetState")
)

// xinputState mirrors XINPUT_STATE.
type xinputState struct {
	Packet  uint32
	Buttons uint16
	LT, RT  uint8
	LX, LY  int16
	RX, RY  int16
}

// xinputRead returns the gamepad in the given XInput slot (0-3).
func xinputRead(slot int) (X360State, bool) {
	var s xinputState
	r, _, _ := procXInputGetState.Call(uintptr(slot), uintptr(unsafe.Pointer(&s)))
	if r != 0 {
		return X360State{}, false
	}
	return X360State{Buttons: s.Buttons, LT: s.LT, RT: s.RT, LX: s.LX, LY: s.LY, RX: s.RX, RY: s.RY}, true
}

// xinputRumble drives the pad's two motors (0-255 each).
func xinputRumble(slot int, left, right uint8) {
	v := struct{ L, R uint16 }{uint16(left) * 257, uint16(right) * 257}
	procXInputSetState.Call(uintptr(slot), uintptr(unsafe.Pointer(&v)))
}

// xinputFirstConnected returns the first connected XInput slot, or -1.
func xinputFirstConnected() int {
	for i := 0; i < 4; i++ {
		if _, ok := xinputRead(i); ok {
			return i
		}
	}
	return -1
}
