package controller

import "sync"

// maxGamepadSlots is how many controllers one client reports. Moonlight numbers
// them 0..N-1 and announces them with an "active gamepad mask" (bit i set = pad
// i is connected); the streamer's virtual pads follow those numbers.
const maxGamepadSlots = 4

// gamepadSlots assigns each captured pad its Moonlight controller number.
//
// A pad keeps its number for as long as it is captured, so the host's pad N
// (and the game's "player N") does not change when another pad comes or goes,
// and the number is the lowest free one, so a single pad is always controller 0.
// It is used from the capture goroutines, the UI goroutine and Moonlight's
// rumble callback thread, hence the lock.
type gamepadSlots struct {
	mu    sync.Mutex
	slots map[string]int
}

// assign gives id a slot (its existing one if it has one) and reports whether
// there was room.
func (t *gamepadSlots) assign(id string) (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.slots[id]; ok {
		return s, true
	}
	used := make(map[int]bool, len(t.slots))
	for _, s := range t.slots {
		used[s] = true
	}
	for s := 0; s < maxGamepadSlots; s++ {
		if !used[s] {
			if t.slots == nil {
				t.slots = make(map[string]int)
			}
			t.slots[id] = s
			return s, true
		}
	}
	return 0, false
}

// release frees id's slot and returns it.
func (t *gamepadSlots) release(id string) (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.slots[id]
	delete(t.slots, id)
	return s, ok
}

func (t *gamepadSlots) slot(id string) (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.slots[id]
	return s, ok
}

// mask is Moonlight's active gamepad mask: one bit per assigned slot.
func (t *gamepadSlots) mask() uint16 {
	t.mu.Lock()
	defer t.mu.Unlock()
	var m uint16
	for _, s := range t.slots {
		m |= 1 << uint(s)
	}
	return m
}

// idAt returns the pad that holds slot s.
func (t *gamepadSlots) idAt(s int) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, slot := range t.slots {
		if slot == s {
			return id, true
		}
	}
	return "", false
}
