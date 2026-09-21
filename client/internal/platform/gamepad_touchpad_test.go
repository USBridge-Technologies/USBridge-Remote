package platform

import "testing"

// report builds a 64-byte DS4-layout input report with the touch bytes 33..42
// and byte 7 set, as captured from a Razer Raiju TE.
func report(byte7 byte, touch [10]byte) []byte {
	r := make([]byte, 64)
	r[0], r[7] = 0x01, byte7
	copy(r[33:43], touch[:])
	return r
}

func TestParseRealRaijuTouchReports(t *testing.T) {
	// From the live dump: finger down at (756, 489), tracking id 6.
	f, click, ok := parseDS4Report(report(0, [10]byte{0x00, 0x00, 0x06, 0xf4, 0x92, 0x1e, 0x80, 0x00, 0x00, 0x00}))
	if !ok || !f.Active || f.ID != 6 || f.X != 0x2f4 || f.Y != 0x1e9 || click {
		t.Fatalf("got %+v click=%v ok=%v", f, click, ok)
	}
	// The same touch after the finger lifted (bit 7 of the contact byte set).
	f, _, _ = parseDS4Report(report(0, [10]byte{0x00, 0x00, 0x86, 0xf4, 0x92, 0x1e, 0x80, 0x00, 0x00, 0x00}))
	if f.Active || f.ID != 6 {
		t.Fatalf("lifted finger: %+v", f)
	}
	// Click: byte 7 bit 1 (bit 0 is the PS button and must not count).
	if _, click, _ = parseDS4Report(report(0x02, [10]byte{})); !click {
		t.Fatal("touchpad click not seen")
	}
	if _, click, _ = parseDS4Report(report(0x01, [10]byte{})); click {
		t.Fatal("the PS button must not click")
	}
	// Full range of the 12-bit fields: x=1919 (0x77f), y=941 (0x3ad).
	f, _, _ = parseDS4Report(report(0, [10]byte{0, 0, 0x01, 0x7f, 0xd7, 0x3a, 0x80}))
	if f.X != 1919 || f.Y != 941 {
		t.Fatalf("range: %+v", f)
	}
}

func TestParseRejectsOtherReports(t *testing.T) {
	if _, _, ok := parseDS4Report(make([]byte, 64)); ok {
		t.Error("report id 0 is not a DS4 input report")
	}
	short := report(0, [10]byte{})[:20]
	if _, _, ok := parseDS4Report(short); ok {
		t.Error("a short report has no touch data")
	}
}

func finger(id uint8, x, y int) ds4Finger { return ds4Finger{Active: true, ID: id, X: x, Y: y} }

func TestTouchpadMouseMovesRelativelyAndNeverJumpsOnANewTouch(t *testing.T) {
	var m touchpadMouse
	if dx, dy := m.move(finger(1, 500, 500)); dx != 0 || dy != 0 {
		t.Fatalf("first contact only sets the reference: %d,%d", dx, dy)
	}
	if dx, dy := m.move(finger(1, 600, 480)); dx != 60 || dy != -12 {
		t.Fatalf("swipe: %d,%d (gain 0.6)", dx, dy)
	}
	// Lift, then touch somewhere else: no jump.
	m.move(ds4Finger{Active: false, ID: 1})
	if dx, dy := m.move(finger(2, 1500, 100)); dx != 0 || dy != 0 {
		t.Fatalf("new touch jumped: %d,%d", dx, dy)
	}
	// A new tracking id without an intervening lift is a new touch too.
	m.move(finger(2, 1500, 100))
	if dx, dy := m.move(finger(3, 200, 900)); dx != 0 || dy != 0 {
		t.Fatalf("id change jumped: %d,%d", dx, dy)
	}
}

func TestTouchpadMouseKeepsTheSubCountRemainder(t *testing.T) {
	var m touchpadMouse
	m.move(finger(1, 0, 0))
	total := 0
	for x := 1; x <= 100; x++ { // 100 single-unit steps = 60 counts, none lost to rounding
		dx, _ := m.move(finger(1, x, 0))
		total += dx
	}
	if total != 60 {
		t.Fatalf("total %d, want 60", total)
	}
}

func TestDecoderEmitsMotionAndClickEdgesOnly(t *testing.T) {
	var got []TouchpadEvent
	var d touchpadDecoder
	emit := func(e TouchpadEvent) { got = append(got, e) }
	touch := func(id byte, x, y int) [10]byte {
		return [10]byte{0, 0, id, byte(x), byte(x>>8) | byte(y&0x0F)<<4, byte(y >> 4), 0x80}
	}
	d.feed(report(0, touch(6, 100, 100)), emit) // contact: nothing to send
	d.feed(report(0, touch(6, 200, 100)), emit) // motion
	d.feed(report(0x02, touch(6, 200, 100)), emit)
	d.feed(report(0x02, touch(6, 200, 100)), emit) // still held: no event
	d.feed(report(0, touch(6, 200, 100)), emit)
	if len(got) != 3 {
		t.Fatalf("events: %+v", got)
	}
	if got[0].DX != 60 || got[0].DY != 0 || got[0].ClickChanged {
		t.Errorf("motion: %+v", got[0])
	}
	if !got[1].ClickChanged || !got[1].ClickDown {
		t.Errorf("press: %+v", got[1])
	}
	if !got[2].ClickChanged || got[2].ClickDown {
		t.Errorf("release: %+v", got[2])
	}
}
