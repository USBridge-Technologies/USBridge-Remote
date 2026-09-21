//go:build windows

package platform

import "testing"

func TestDS4RumbleReportLayout(t *testing.T) {
	r := ds4RumbleReport(0xFFFF, 0x8000)
	if len(r) != 32 || r[0] != 0x05 {
		t.Fatalf("report id/length: % x", r[:6])
	}
	if r[1]&ds4FlagRumble == 0 {
		t.Fatal("the rumble flag must be set")
	}
	if r[4] != 0x80 || r[5] != 0xFF {
		t.Fatalf("motors: right(small)=%#x left(large)=%#x", r[4], r[5])
	}
	for i, b := range r[6:] {
		if b != 0 {
			t.Fatalf("byte %d must stay zero (no lightbar/flash), got %#x", i+6, b)
		}
	}
	if z := ds4RumbleReport(0, 0); z[4] != 0 || z[5] != 0 {
		t.Fatal("(0,0) must stop both motors")
	}
}

func TestOnlyKnownPadsGetAHIDRumbleProtocol(t *testing.T) {
	for _, c := range []struct {
		vid, pid uint16
		want     bool
	}{
		{0x1532, 0x1007, true},  // Razer Raiju TE (verified)
		{0x054C, 0x09CC, true},  // DualShock 4 v2
		{0x054C, 0x0CE6, false}, // DualSense: a different report
		{0x045E, 0x028E, false}, // an Xbox pad goes through XInput
		{0x1532, 0x0A29, false}, // Razer Wolverine V2 Xbox
	} {
		if got := hidRumbleProtocolFor(c.vid, c.pid) != nil; got != c.want {
			t.Errorf("%04x:%04x: protocol=%v want %v", c.vid, c.pid, got, c.want)
		}
	}
}

func TestPickXInputSlotMatchesByUSBIdNotByNumber(t *testing.T) {
	slots := []xinputSlotInfo{
		{slot: 0, vid: 0x045E, pid: 0x028E, known: true}, // e.g. the virtual X360 of a local host
		{slot: 1, vid: 0x1532, pid: 0x0A29, known: true}, // Wolverine
	}
	// The Wolverine as WinMM sees it (its number there is irrelevant).
	if got := pickXInputSlot(0x1532, 0x0A29, slots, false, 0); got != 1 {
		t.Fatalf("Wolverine must resolve to its own slot 1, got %d", got)
	}
}

func TestAPlayStationPadNeverBorrowsAnXboxSlot(t *testing.T) {
	slots := []xinputSlotInfo{{slot: 0, vid: 0x1532, pid: 0x0A29, known: true}}
	// The Raiju has no XInput slot: nothing must vibrate, and above all not the Wolverine.
	if got := pickXInputSlot(0x1532, 0x1007, slots, true, 0); got != -1 {
		t.Fatalf("got slot %d, want none", got)
	}
	// Even when the DLL cannot tell whose the slot is, a pad with an SDL mapping is never guessed.
	if got := pickXInputSlot(0x1532, 0x1007, []xinputSlotInfo{{slot: 0, known: false}}, true, 0); got != -1 {
		t.Fatalf("got slot %d, want none", got)
	}
}

func TestLegacyGuessOnlyWhenIdsAreUnreadable(t *testing.T) {
	unknown := []xinputSlotInfo{{slot: 0}, {slot: 2}}
	if got := pickXInputSlot(0x1234, 0x5678, unknown, false, 2); got != 2 {
		t.Fatalf("hint slot should win: %d", got)
	}
	if got := pickXInputSlot(0x1234, 0x5678, unknown, false, 3); got != 0 {
		t.Fatalf("first connected slot otherwise: %d", got)
	}
	// All ids readable and none match: no guess.
	if got := pickXInputSlot(0x1234, 0x5678, []xinputSlotInfo{{slot: 0, vid: 1, pid: 2, known: true}}, false, 0); got != -1 {
		t.Fatalf("got %d, want none", got)
	}
	if got := pickXInputSlot(0x1234, 0x5678, nil, false, 0); got != -1 {
		t.Fatalf("no slots at all: %d", got)
	}
}
