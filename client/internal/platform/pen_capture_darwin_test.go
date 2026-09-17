//go:build darwin && !ios

package platform

import "testing"

// Raw report bytes below are real IOHIDDeviceRegisterInputReportCallback
// captures from a physical Wacom Intuos S (CTL-4100, vid=0x056a pid=0x0374),
// recorded live via client/cmd/pentest against this exact decoder -- not
// synthesized -- so this test also guards the InRange bit regression fixed
// in decodePenReport's doc comment (bit5 vs bit6).
func TestDecodePenReportFarAway(t *testing.T) {
	raw := []byte{0x10, 0x00, 0x7b, 0x1e, 0x00, 0xaf, 0x06, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	state, ok := decodePenReport(raw)
	if !ok {
		t.Fatal("decodePenReport returned ok=false for a valid report")
	}
	if state.InRange {
		t.Error("InRange = true, want false (penByte=0x00, pen far from tablet)")
	}
	if state.TipSwitch {
		t.Error("TipSwitch = true, want false")
	}
}

func TestDecodePenReportHoveringNotTouching(t *testing.T) {
	raw := []byte{0x10, 0x40, 0x00, 0x13, 0x00, 0x9c, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	state, ok := decodePenReport(raw)
	if !ok {
		t.Fatal("decodePenReport returned ok=false for a valid report")
	}
	if !state.InRange {
		t.Error("InRange = false, want true (penByte=0x40, hovering close)")
	}
	if state.TipSwitch {
		t.Error("TipSwitch = true, want false (not touching)")
	}
	if state.Pressure != 0 {
		t.Errorf("Pressure = %d, want 0 while hovering", state.Pressure)
	}
}

func TestDecodePenReportTouching(t *testing.T) {
	// penByte=0x61 (bit0 tip + bit5 + bit6), X=0x001869=6249, Y=0x000c90=3216,
	// pressure=0x005e=94.
	raw := []byte{
		0x10, 0x61, 0x69, 0x18, 0x00, 0x90, 0x0c, 0x00,
		0x5e, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	state, ok := decodePenReport(raw)
	if !ok {
		t.Fatal("decodePenReport returned ok=false for a valid report")
	}
	if !state.InRange {
		t.Error("InRange = false, want true while touching")
	}
	if !state.TipSwitch {
		t.Error("TipSwitch = false, want true (penByte has bit0 set)")
	}
	if state.X != 6249 {
		t.Errorf("X = %d, want 6249", state.X)
	}
	if state.Y != 3216 {
		t.Errorf("Y = %d, want 3216", state.Y)
	}
	if state.Pressure != 94 {
		t.Errorf("Pressure = %d, want 94", state.Pressure)
	}
	if state.Button1 || state.Button2 || state.Eraser {
		t.Errorf("unexpected button/eraser bits set: %+v", state)
	}
}

func TestDecodePenReportRejectsShortOrWrongReportID(t *testing.T) {
	if _, ok := decodePenReport([]byte{0x10, 0x00, 0x00}); ok {
		t.Error("expected ok=false for a too-short buffer")
	}
	longEnoughWrongID := make([]byte, 14)
	longEnoughWrongID[0] = 0x11
	if _, ok := decodePenReport(longEnoughWrongID); ok {
		t.Error("expected ok=false for a non-0x10 report ID")
	}
}

// TestPenRangeForKnownModel guards the one entry live-verified against real
// hardware (see wacomIntuosV2Ranges' doc comment) -- if this ever regresses,
// every X/Y/pressure sample sent for a real CTL-4100 goes wrong silently.
func TestPenRangeForKnownModel(t *testing.T) {
	maxX, maxY, maxPressure := PenRangeFor(0x0374) // CTL-4100
	if maxX != PenMaxX || maxY != PenMaxY || maxPressure != PenMaxPressure {
		t.Errorf("PenRangeFor(0x0374) = (%d, %d, %d), want (%d, %d, %d)",
			maxX, maxY, maxPressure, PenMaxX, PenMaxY, PenMaxPressure)
	}

	// Spot-check one Pro-line and one Cintiq Pro entry sourced from
	// OpenTabletDriver's own JSON specs -- not live-verified hardware, but
	// this pins the table against an accidental typo during a future edit.
	if maxX, maxY, maxPressure := PenRangeFor(0x0358); maxX != 62200 || maxY != 43200 || maxPressure != 8191 {
		t.Errorf("PenRangeFor(0x0358) [PTH-860] = (%d, %d, %d), want (62200, 43200, 8191)", maxX, maxY, maxPressure)
	}
	if maxX, maxY, maxPressure := PenRangeFor(0x0352); maxX != 140384 || maxY != 79316 || maxPressure != 8191 {
		t.Errorf("PenRangeFor(0x0352) [DTH-3220] = (%d, %d, %d), want (140384, 79316, 8191)", maxX, maxY, maxPressure)
	}
}

// TestPenRangeForUnknownModelFallsBackToCTL4100 documents the deliberate
// choice to default to the smallest/most-common model's range rather than
// guess a large-format one for a device this project hasn't catalogued.
func TestPenRangeForUnknownModelFallsBackToCTL4100(t *testing.T) {
	maxX, maxY, maxPressure := PenRangeFor(0xFFFF)
	if maxX != PenMaxX || maxY != PenMaxY || maxPressure != PenMaxPressure {
		t.Errorf("PenRangeFor(unknown) = (%d, %d, %d), want CTL-4100 defaults (%d, %d, %d)",
			maxX, maxY, maxPressure, PenMaxX, PenMaxY, PenMaxPressure)
	}
}
