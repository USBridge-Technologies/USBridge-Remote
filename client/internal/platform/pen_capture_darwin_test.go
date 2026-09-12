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
