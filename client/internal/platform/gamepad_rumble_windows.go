//go:build windows

package platform

import (
	"strconv"
	"strings"
	"sync"
)

// Force feedback for a captured gamepad through XInput: the host's Moonlight
// rumble levels drive the pad's two motors with XInputSetState.

var (
	rumbleMu    sync.Mutex
	rumbleSlots = map[string]int{} // pad id -> XInput slot currently vibrating
)

// xinputSlotFor resolves a pad id to its XInput slot. "xinput:N" names the slot
// exactly. For a legacy "winmm:N" id, WinMM and XInput number pads
// independently, so N is only a hint: it is used when that slot holds a pad,
// otherwise the first connected slot.
func xinputSlotFor(id string) int {
	if n, ok := strings.CutPrefix(id, "xinput:"); ok {
		if slot, err := strconv.Atoi(n); err == nil && slot >= 0 && slot < xinputMaxSlots && xinputConnected(slot) {
			return slot
		}
		return -1
	}
	if slot, err := strconv.Atoi(strings.TrimPrefix(id, "winmm:")); err == nil && slot >= 0 && slot < xinputMaxSlots && xinputConnected(slot) {
		return slot
	}
	for i := 0; i < xinputMaxSlots; i++ {
		if xinputConnected(i) {
			return i
		}
	}
	return -1
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
	xinputSetVibration(slot, low, high)
}

// StopGamepadRumble silences the pad.
func StopGamepadRumble(id string) {
	rumbleMu.Lock()
	defer rumbleMu.Unlock()
	if slot, ok := rumbleSlots[id]; ok {
		xinputSetVibration(slot, 0, 0)
		delete(rumbleSlots, id)
	}
}
