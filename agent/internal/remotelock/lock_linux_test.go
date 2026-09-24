//go:build linux

package remotelock

import "testing"

func TestDropFilteredEvent_MatchesWinMacPolicy(t *testing.T) {
	// Buttons / keys always drop.
	if !dropFilteredEvent(evKey, 0x110) { // BTN_LEFT
		t.Fatal("BTN_LEFT must drop")
	}
	if !dropFilteredEvent(evKey, 30) { // KEY_A
		t.Fatal("KEY_A must drop")
	}
	// Wheel drops; pointer motion must pass (otherwise the cursor sticks).
	if !dropFilteredEvent(evRel, relWheel) {
		t.Fatal("REL_WHEEL must drop")
	}
	if !dropFilteredEvent(evRel, relHWheel) {
		t.Fatal("REL_HWHEEL must drop")
	}
	if dropFilteredEvent(evRel, 0) { // REL_X
		t.Fatal("REL_X must pass")
	}
	if dropFilteredEvent(evRel, 1) { // REL_Y
		t.Fatal("REL_Y must pass")
	}
	if dropFilteredEvent(evAbs, absX) {
		t.Fatal("ABS_X must pass")
	}
	if dropFilteredEvent(evSyn, 0) {
		t.Fatal("SYN_REPORT must pass")
	}
}

func withFakes(t *testing.T, down *bool) (grabs *[]bool) {
	t.Helper()
	var calls []bool
	oldG, oldK := grabIoctl, keysDownFn
	grabIoctl = func(fd int, on bool) error { calls = append(calls, on); return nil }
	keysDownFn = func(fd int) bool { return *down }
	t.Cleanup(func() { grabIoctl, keysDownFn = oldG, oldK })
	return &calls
}

// The stuck-button bug: grabbing while a button is held hides its release
// from the compositor. The grab must wait until everything is released.
func TestGrab_DeferredWhileButtonHeld(t *testing.T) {
	down := true
	calls := withFakes(t, &down)
	d := &grabbedDev{fd: 3}

	grab(d)
	if d.grabbed || len(*calls) != 0 {
		t.Fatalf("grabbed while a button was held: grabbed=%v ioctls=%v", d.grabbed, *calls)
	}
	down = false
	grab(d)
	if !d.grabbed || len(*calls) != 1 || !(*calls)[0] {
		t.Fatalf("grab should land once released: grabbed=%v ioctls=%v", d.grabbed, *calls)
	}
}

func TestGrab_NoRepeatWhenAlreadyGrabbed(t *testing.T) {
	down := false
	calls := withFakes(t, &down)
	d := &grabbedDev{fd: 3}
	grab(d)
	grab(d)
	if len(*calls) != 1 {
		t.Fatalf("expected a single grab ioctl, got %v", *calls)
	}
}

func TestUngrab_AlwaysReleasesEvenWithKeysDown(t *testing.T) {
	down := true
	calls := withFakes(t, &down)
	d := &grabbedDev{fd: 3, grabbed: true}
	ungrab(d)
	if d.grabbed || len(*calls) != 1 || (*calls)[0] {
		t.Fatalf("ungrab must always release: grabbed=%v ioctls=%v", d.grabbed, *calls)
	}
}

func TestAnyBitSet(t *testing.T) {
	if anyBitSet(make([]byte, 96)) {
		t.Error("empty bitmap reported as pressed")
	}
	b := make([]byte, 96)
	b[0x110/8] = 1 << (0x110 % 8) // BTN_LEFT
	if !anyBitSet(b) {
		t.Error("BTN_LEFT bit not detected")
	}
}

// keysDown against a real evdev node: an idle device must report clean and
// a non-evdev fd must fail safe (report "down" so we never grab blindly).
func TestKeysDown_FailsSafeOnBadFd(t *testing.T) {
	if !keysDown(-1) {
		t.Error("ioctl failure must be treated as keys-down")
	}
}
