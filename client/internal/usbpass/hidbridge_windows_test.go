//go:build windows

package usbpass

import "testing"

func TestNormalizeHIDIDMatchesRawInputPath(t *testing.T) {
	instance := `HID\VID_056A&PID_0374&COL03\7&F7316DF&0&0002`
	rawPath := `\\?\HID#VID_056A&PID_0374&Col03#7&f7316df&0&0002#{4d1e55b2-f16f-11cf-88cb-001111000030}`
	if normalizeHIDID(instance) != normalizeHIDID(rawPath) {
		t.Fatalf("instance %q and raw input path %q do not normalize equally: %q vs %q",
			instance, rawPath, normalizeHIDID(instance), normalizeHIDID(rawPath))
	}
	other := `\\?\HID#VID_056A&PID_0374&Col04#7&f7316df&0&0003#{4d1e55b2-f16f-11cf-88cb-001111000030}`
	if normalizeHIDID(instance) == normalizeHIDID(other) {
		t.Fatal("different collections must not match")
	}
}

func TestColAndMINumbers(t *testing.T) {
	if n := colNumber(`HID\VID_056A&PID_0374&COL04\7&F7316DF&0&0003`); n != 4 {
		t.Fatalf("colNumber = %d", n)
	}
	if n := colNumber(`HID\VID_046D&PID_C548&MI_03\8&5671617&0&0000`); n != 0 {
		t.Fatalf("no-COL node gave %d", n)
	}
	if n := miNumber(`USB\VID_046D&PID_C548&MI_02\7&2A88ABA2&0&0002`); n != 2 {
		t.Fatalf("miNumber = %d", n)
	}
	if n := miNumber(""); n != -1 {
		t.Fatalf("empty interface key gave %d", n)
	}
}

func TestLiveKindMouseKeyboardAreSilent(t *testing.T) {
	mk := func(page, usage uint16, queryOnly bool) *winHIDCollection {
		return &winHIDCollection{queryOnly: queryOnly, pp: &ppData{UsagePage: page, Usage: usage}}
	}
	if k := mk(0x01, 0x02, true).liveKind(); k != liveNone {
		t.Fatalf("exclusive mouse = %v, want liveNone", k)
	}
	if k := mk(0x01, 0x06, true).liveKind(); k != liveNone {
		t.Fatalf("exclusive keyboard = %v, want liveNone", k)
	}
	if k := mk(0x0D, 0x02, true).liveKind(); k != liveRaw {
		t.Fatalf("exclusive pen = %v, want liveRaw", k)
	}
	if k := mk(0x01, 0x02, false).liveKind(); k != liveRead {
		t.Fatalf("readable collection = %v, want liveRead", k)
	}
}
