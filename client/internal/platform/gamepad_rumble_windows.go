//go:build windows

package platform

import (
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Force feedback for a captured gamepad through XInput: the host's Moonlight
// rumble levels drive the pad's two motors with XInputSetState.

var (
	modXInputRumble    = windows.NewLazySystemDLL("xinput1_4.dll")
	procXInputGetState = modXInputRumble.NewProc("XInputGetState")
	procXInputSetState = modXInputRumble.NewProc("XInputSetState")

	rumbleMu    sync.Mutex
	rumbleSlots = map[string]int{} // pad id -> XInput slot currently vibrating
)

type xinputVibration struct{ Left, Right uint16 }

func xinputConnected(slot int) bool {
	var state [16]byte // XINPUT_STATE
	r, _, _ := procXInputGetState.Call(uintptr(slot), uintptr(unsafe.Pointer(&state[0])))
	return r == 0
}

// xinputSlotFor picks the XInput slot for a "winmm:N" pad id. WinMM and
// XInput number pads independently, so N is only a hint: it is used when that
// slot holds a pad, otherwise the first connected slot.
func xinputSlotFor(id string) int {
	if n, err := strconv.Atoi(strings.TrimPrefix(id, "winmm:")); err == nil && n >= 0 && n < 4 && xinputConnected(n) {
		return n
	}
	for i := 0; i < 4; i++ {
		if xinputConnected(i) {
			return i
		}
	}
	return -1
}

func setVibration(slot int, low, high uint16) {
	v := xinputVibration{Left: low, Right: high}
	procXInputSetState.Call(uintptr(slot), uintptr(unsafe.Pointer(&v)))
}

// SetGamepadRumble drives the pad's motors: low is the large (low-frequency)
// motor and high the small one, both 0..65535 (Moonlight's scale). 0,0 stops
// the vibration. A pad that is not an XInput device simply does not vibrate.
func SetGamepadRumble(id string, low, high uint16) {
	rumbleMu.Lock()
	defer rumbleMu.Unlock()
	slot := xinputSlotFor(id)
	if slot < 0 {
		return
	}
	rumbleSlots[id] = slot
	setVibration(slot, low, high)
}

// StopGamepadRumble silences the pad.
func StopGamepadRumble(id string) {
	rumbleMu.Lock()
	defer rumbleMu.Unlock()
	if slot, ok := rumbleSlots[id]; ok {
		setVibration(slot, 0, 0)
		delete(rumbleSlots, id)
	}
}
