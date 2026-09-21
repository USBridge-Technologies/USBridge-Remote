//go:build windows

package platform

import (
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

// Force feedback for a captured gamepad: the host's Moonlight rumble levels go
// to the pad's two motors. Xbox-class pads are driven through XInput
// (XInputSetState); PlayStation-layout DirectInput pads through their HID output
// report (gamepad_rumble_hid_windows.go). A pad with neither path does not vibrate.

var (
	rumbleMu      sync.Mutex
	rumbleTargets = map[string]*rumbleTarget{} // pad id -> how to vibrate it (nil-effect entries are cached too)
)

// rumbleTarget is the way one captured pad vibrates.
type rumbleTarget struct {
	xinputSlot int // -1 when the pad is not vibrated through XInput
	hid        *hidRumbler
}

func (t *rumbleTarget) apply(low, high uint16) {
	switch {
	case t.hid != nil:
		t.hid.set(low, high)
	case t.xinputSlot >= 0:
		xinputSetVibration(t.xinputSlot, low, high)
	}
}

func (t *rumbleTarget) close() {
	switch {
	case t.hid != nil:
		t.hid.close()
	case t.xinputSlot >= 0:
		xinputSetVibration(t.xinputSlot, 0, 0)
	}
}

// xinputSlotInfo is a connected XInput slot and, when the DLL can tell, the USB
// id of the pad in it.
type xinputSlotInfo struct {
	slot     int
	vid, pid uint16
	known    bool
}

// pickXInputSlot chooses the XInput slot that is the same physical pad as a
// "winmm:N" one. WinMM and XInput number pads independently, so the pad is
// matched by USB id. A pad WinMM sees but XInput does not (a PlayStation-layout
// pad) gets no slot: guessing would vibrate somebody else's Xbox pad.
//
// Only when the DLL cannot report ids at all (older Windows) and the pad has no
// SDL mapping (i.e. it may well be an Xbox pad) is the old guess used: WinMM's
// own number if that slot holds a pad, else the first connected one.
func pickXInputSlot(vid, pid uint16, slots []xinputSlotInfo, hasMapping bool, hint int) int {
	unknown := false
	for _, s := range slots {
		if !s.known {
			unknown = true
			continue
		}
		if s.vid == vid && s.pid == pid {
			return s.slot
		}
	}
	if !unknown || hasMapping {
		return -1
	}
	for _, s := range slots {
		if s.slot == hint {
			return hint
		}
	}
	if len(slots) > 0 {
		return slots[0].slot
	}
	return -1
}

func connectedXInputSlots() []xinputSlotInfo {
	var out []xinputSlotInfo
	for i := 0; i < xinputMaxSlots; i++ {
		if !xinputConnected(i) {
			continue
		}
		vid, pid, ok := xinputVIDPID(i)
		out = append(out, xinputSlotInfo{slot: i, vid: vid, pid: pid, known: ok})
	}
	return out
}

// winmmVIDPIDOf is the USB id of WinMM joystick joyID.
func winmmVIDPIDOf(joyID uintptr) (vid, pid uint16, ok bool) {
	var caps joyCapsW
	ret, _, _ := procJoyGetDevCapsW.Call(joyID, uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps))
	if ret != joyErrNoError {
		return 0, 0, false
	}
	return caps.wMid, caps.wPid, true
}

// resolveRumbleTarget works out how to vibrate a pad id ("xinput:N" or "winmm:N").
func resolveRumbleTarget(id string) *rumbleTarget {
	none := &rumbleTarget{xinputSlot: -1}
	if n, ok := strings.CutPrefix(id, "xinput:"); ok {
		slot, err := strconv.Atoi(n)
		if err != nil || slot < 0 || slot >= xinputMaxSlots || !xinputConnected(slot) {
			return none
		}
		return &rumbleTarget{xinputSlot: slot}
	}
	joyID, err := strconv.Atoi(strings.TrimPrefix(id, "winmm:"))
	if err != nil || joyID < 0 {
		return none
	}
	vid, pid, ok := winmmVIDPIDOf(uintptr(joyID))
	if !ok {
		return none
	}
	if r := newHIDRumbler(vid, pid); r != nil {
		return &rumbleTarget{xinputSlot: -1, hid: r}
	}
	slot := pickXInputSlot(vid, pid, connectedXInputSlots(), sdlMappingFor(vid, pid) != nil, joyID)
	return &rumbleTarget{xinputSlot: slot}
}

// SetGamepadRumble drives the pad's motors: low is the large (low-frequency)
// motor and high the small one, both 0..65535 (Moonlight's scale). 0,0 stops
// the vibration. A pad without a rumble path simply does not vibrate.
func SetGamepadRumble(id string, low, high uint16) {
	rumbleMu.Lock()
	defer rumbleMu.Unlock()
	t, ok := rumbleTargets[id]
	if !ok {
		t = resolveRumbleTarget(id)
		rumbleTargets[id] = t
	}
	t.apply(low, high)
}

// StopGamepadRumble silences the pad and forgets how it was being driven, so
// the next capture resolves it afresh.
func StopGamepadRumble(id string) {
	rumbleMu.Lock()
	defer rumbleMu.Unlock()
	if t, ok := rumbleTargets[id]; ok {
		t.close()
		delete(rumbleTargets, id)
	}
}
