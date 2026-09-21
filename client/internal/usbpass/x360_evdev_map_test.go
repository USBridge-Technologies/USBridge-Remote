package usbpass

import "testing"

func TestEvdevButtonsAndHat(t *testing.T) {
	m := newEvdevMapper()
	m.setKey(0x130, true) // A
	m.setKey(0x133, true) // X (BTN_NORTH)
	m.setKey(0x136, true) // LB
	m.setAbs(absHat0X, -1)
	m.setAbs(absHat0Y, 1)
	if got, want := m.state().Buttons, uint16(0x1000|0x4000|0x0100|0x0004|0x0002); got != want {
		t.Fatalf("buttons = %#x, want %#x", got, want)
	}
	m.setKey(0x130, false)
	m.setAbs(absHat0X, 0)
	m.setAbs(absHat0Y, 0)
	if got, want := m.state().Buttons, uint16(0x4000|0x0100); got != want {
		t.Fatalf("after release buttons = %#x, want %#x", got, want)
	}
	m.setKey(0x999, true) // unknown key is ignored
	if m.state().Buttons != 0x4000|0x0100 {
		t.Fatal("unknown key changed the state")
	}
}

func TestEvdevDpadButtons(t *testing.T) {
	m := newEvdevMapper()
	m.setKey(0x220, true) // BTN_DPAD_UP
	m.setKey(0x223, true) // BTN_DPAD_RIGHT
	if got := m.state().Buttons; got != 0x0001|0x0008 {
		t.Fatalf("dpad buttons = %#x", got)
	}
}

func TestEvdevSticksInvertY(t *testing.T) {
	m := newEvdevMapper()
	if s := m.state(); s.LX != 0 && s.LX != -1 || s.LY != 0 && s.LY != -1 && s.LY != 1 {
		t.Fatalf("stick does not rest at centre: %+v", s)
	}
	m.setAbs(absX, 32767)
	m.setAbs(absY, -32768) // evdev up
	m.setAbs(absRX, -32768)
	m.setAbs(absRY, 32767) // evdev down
	s := m.state()
	if s.LX != 32767 || s.LY != 32767 || s.RX != -32768 || s.RY != -32767 {
		t.Fatalf("sticks = %+v (Y must be inverted so up is positive)", s)
	}
}

func TestEvdevRangeScaling(t *testing.T) {
	m := newEvdevMapper()
	// A pad reporting sticks 0..255 and triggers 0..255.
	for _, c := range []uint16{absX, absY, absRX, absRY} {
		m.setRange(c, 0, 255)
	}
	m.setRange(absZ, 0, 255)
	m.setRange(absRZ, 0, 255)
	m.setAbs(absX, 0)
	m.setAbs(absRX, 255)
	m.setAbs(absZ, 255)
	m.setAbs(absRZ, 128)
	s := m.state()
	if s.LX != -32768 || s.RX != 32767 {
		t.Fatalf("stick range not scaled: %+v", s)
	}
	if s.LT != 255 || s.RT != 128 {
		t.Fatalf("triggers = %d/%d, want 255/128", s.LT, s.RT)
	}
}

func TestEvdevDefaultTriggerRange(t *testing.T) {
	m := newEvdevMapper()
	m.setAbs(absZ, 1023)
	m.setAbs(absRZ, 512)
	if s := m.state(); s.LT != 255 || s.RT != 127 {
		t.Fatalf("triggers = %d/%d, want 255/127", s.LT, s.RT)
	}
}

func TestEvdevOutOfRangeIsClamped(t *testing.T) {
	m := newEvdevMapper()
	m.setAbs(absX, 100000)
	m.setAbs(absZ, -50)
	if s := m.state(); s.LX != 32767 || s.LT != 0 {
		t.Fatalf("not clamped: %+v", s)
	}
}
