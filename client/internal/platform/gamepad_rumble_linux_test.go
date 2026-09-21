//go:build linux && !android

package platform

import (
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestFFEffectLayoutMatchesKernel(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout is only mirrored for 64-bit Linux")
	}
	if got := unsafe.Sizeof(linuxFFEffect{}); got != 48 {
		t.Fatalf("sizeof(struct ff_effect) = %d, want 48", got)
	}
	if req := linuxIoctlW(linuxEviocsffN, 48); req != 0x40304580 {
		t.Fatalf("EVIOCSFF = %#x, want 0x40304580", req)
	}
	if req := linuxIoctlW(linuxEviocrmffN, 4); req != 0x40044581 {
		t.Fatalf("EVIOCRMFF = %#x, want 0x40044581", req)
	}
}

// TestLiveRumble vibrates a real pad for a moment. Opt in with
// USBRIDGE_RUMBLE_EVDEV=/dev/input/eventN (the pad's node).
func TestLiveRumble(t *testing.T) {
	path := os.Getenv("USBRIDGE_RUMBLE_EVDEV")
	if path == "" {
		t.Skip("set USBRIDGE_RUMBLE_EVDEV to a gamepad event node to run")
	}
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	st := &rumbleState{fd: fd, effectID: -1}
	defer func() {
		rumbleMu.Lock()
		st.closeLocked()
		rumbleMu.Unlock()
	}()

	rumbleMu.Lock()
	err = st.playLocked(0xFFFF, 0)
	rumbleMu.Unlock()
	if err != nil {
		t.Fatalf("play large motor: %v", err)
	}
	time.Sleep(600 * time.Millisecond)

	rumbleMu.Lock()
	err = st.playLocked(0, 0xFFFF) // update the running effect in place: small motor
	rumbleMu.Unlock()
	if err != nil {
		t.Fatalf("update to small motor: %v", err)
	}
	time.Sleep(600 * time.Millisecond)

	rumbleMu.Lock()
	st.stopLocked()
	rumbleMu.Unlock()
	if st.effectID < 0 {
		t.Fatal("the kernel never assigned an effect id")
	}
}
