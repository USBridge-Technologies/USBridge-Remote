//go:build windows || (linux && !android)

package platform

import "testing"

// The database line for the Razer Raiju TE (USB 1532:1007), verbatim.
const raijuTELine = "03000000321500000710000000000000,Razer Raiju TE,a:b1,b:b2,back:b8,dpdown:h0.4,dpleft:h0.8,dpright:h0.2,dpup:h0.1,leftshoulder:b4,leftstick:b10,lefttrigger:a3,leftx:a0,lefty:a1,rightshoulder:b5,rightstick:b11,righttrigger:a4,rightx:a2,righty:a5,start:b9,x:b0,y:b3,platform:Windows,"

func raijuTE(t *testing.T) *sdlMapping {
	t.Helper()
	guid, m, ok := parseSDLMapping(raijuTELine)
	if !ok || guid != "03000000321500000710000000000000" || m.name != "Razer Raiju TE" || !m.usable() {
		t.Fatalf("parse: guid=%q ok=%v", guid, ok)
	}
	return m
}

// unit is what winmmUnit gives for a raw 0..65535 WinMM axis value.
func unit(raw float64) float64 { return raw/65535*2 - 1 }

// raijuIdle is the pad resting, as measured: sticks near 32767, triggers at 0.
func raijuIdle() joyInput {
	return joyInput{axes: [6]float64{unit(32767), unit(32767), unit(32767), unit(32767), unit(0), unit(0)}, pov: -1}
}

func TestSDLGUIDKeyIsTheLittleEndianVIDPID(t *testing.T) {
	if got := sdlGUIDKey(0x1532, 0x1007); got != "03000000321500000710" {
		t.Fatalf("got %s", got)
	}
}

func TestRaijuIdleIsNeutral(t *testing.T) {
	if st := raijuTE(t).capture(raijuIdle()); st != (GamepadCaptureState{}) {
		t.Fatalf("idle pad is not neutral: %+v", st)
	}
}

// Measured on the real pad: L2 is the V axis with button 6, R2 the U axis with
// button 7; the right stick is Z (X) and R (Y); 0 is "up" on both sticks.
func TestRaijuTriggersAndSticksMatchTheLivePad(t *testing.T) {
	m := raijuTE(t)

	in := raijuIdle()
	in.axes[5] = unit(65535) // V
	in.buttons = 1 << 6
	if st := m.capture(in); st.LeftTrigger != 255 || st.RightTrigger != 0 {
		t.Fatalf("L2: %+v", st)
	}

	in = raijuIdle()
	in.axes[4] = unit(65535) // U
	in.buttons = 1 << 7
	if st := m.capture(in); st.RightTrigger != 255 || st.LeftTrigger != 0 {
		t.Fatalf("R2: %+v", st)
	}

	in = raijuIdle()
	in.axes[4] = unit(32768) // half pulled
	if st := m.capture(in); st.RightTrigger < 126 || st.RightTrigger > 129 {
		t.Fatalf("half-pulled R2 should be about 127, got %d", st.RightTrigger)
	}

	in = raijuIdle()
	in.axes[1] = unit(0) // left stick up: Y at its minimum
	in.axes[0] = unit(65535)
	if st := m.capture(in); st.LeftY != 32767 || st.LeftX != 32767 {
		t.Fatalf("left stick up+right: %+v", st)
	}

	in = raijuIdle()
	in.axes[3] = unit(0)     // right stick up: R at its minimum
	in.axes[2] = unit(65535) // and right: Z at its maximum
	if st := m.capture(in); st.RightY != 32767 || st.RightX != 32767 {
		t.Fatalf("right stick up+right: %+v", st)
	}

	in = raijuIdle()
	in.axes[1] = unit(65535)
	in.axes[3] = unit(65535)
	if st := m.capture(in); st.LeftY != -32767 || st.RightY != -32767 {
		t.Fatalf("sticks down: %+v", st)
	}
}

func TestRaijuButtons(t *testing.T) {
	m := raijuTE(t)
	// SDL's a/b/x/y are b1/b2/b0/b3 on this pad (cross, circle, square, triangle).
	for _, c := range []struct {
		bit  uint
		flag uint16
		name string
	}{
		{1, 0x1000, "cross=A"}, {2, 0x2000, "circle=B"}, {0, 0x4000, "square=X"}, {3, 0x8000, "triangle=Y"},
		{4, 0x0100, "L1"}, {5, 0x0200, "R1"}, {8, 0x0020, "share=Back"}, {9, 0x0010, "options=Start"},
		{10, 0x0040, "L3"}, {11, 0x0080, "R3"},
	} {
		in := raijuIdle()
		in.buttons = 1 << c.bit
		if st := m.capture(in); st.Buttons != c.flag {
			t.Errorf("%s: buttons=%#04x want %#04x", c.name, st.Buttons, c.flag)
		}
	}
	// The digital half of a trigger is not a button of its own.
	in := raijuIdle()
	in.buttons = 1<<6 | 1<<7
	if st := m.capture(in); st.Buttons != 0 {
		t.Errorf("L2/R2 buttons leaked into the button mask: %#04x", st.Buttons)
	}
}

func TestRaijuHatIsTheDpad(t *testing.T) {
	m := raijuTE(t)
	for pov, want := range map[int]uint16{
		0: 0x0001, 9000: 0x0008, 18000: 0x0002, 27000: 0x0004,
		4500: 0x0001 | 0x0008, 31500: 0x0001 | 0x0004,
	} {
		in := raijuIdle()
		in.pov = pov
		if st := m.capture(in); st.Buttons != want {
			t.Errorf("pov %d: buttons=%#04x want %#04x", pov, st.Buttons, want)
		}
	}
}

func TestEntryWithoutADpadFallsBackToTheHat(t *testing.T) {
	_, m, _ := parseSDLMapping("03000000aaaa00000001000000000000,Pad,a:b0,b:b1,leftx:a0,lefty:a1,rightx:a2,righty:a3,platform:Windows,")
	in := joyInput{pov: 0}
	if st := m.capture(in); st.Buttons != 0x0001 {
		t.Fatalf("hat up should be D-pad up: %#04x", st.Buttons)
	}
}

func TestTriggersOnButtonsAndHalfAxes(t *testing.T) {
	_, m, _ := parseSDLMapping("03000000aaaa00000002000000000000,Pad,a:b0,b:b1,leftx:a0,lefty:a1,rightx:a2,righty:a3,lefttrigger:b6,righttrigger:+a4,platform:Windows,")
	in := joyInput{pov: -1, buttons: 1 << 6}
	in.axes[4] = 1 // full push on the axis
	if st := m.capture(in); st.LeftTrigger != 255 || st.RightTrigger != 255 {
		t.Fatalf("%+v", st)
	}
	in = joyInput{pov: -1}
	in.axes[4] = -1 // negative half: nothing
	if st := m.capture(in); st.LeftTrigger != 0 || st.RightTrigger != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestInvertedAxisMarker(t *testing.T) {
	_, m, _ := parseSDLMapping("03000000aaaa00000003000000000000,Pad,a:b0,b:b1,leftx:a0~,lefty:a1,rightx:a2,righty:a3,platform:Windows,")
	in := joyInput{pov: -1}
	in.axes[0] = 1
	if st := m.capture(in); st.LeftX != -32767 {
		t.Fatalf("a0~ should invert: %+v", st)
	}
}

func TestParseRejectsJunkAndIncompleteEntries(t *testing.T) {
	for _, l := range []string{"", "# comment", "onlyone", "guid,name"} {
		if _, _, ok := parseSDLMapping(l); ok {
			t.Errorf("%q should not parse", l)
		}
	}
	// A foot pedal names no face buttons: not a gamepad.
	_, m, _ := parseSDLMapping("03000000fa2d00000100000000000000,3dRudder Foot Motion Controller,leftx:a0,lefty:a1,rightx:a5,righty:a2,platform:Windows,")
	if m.usable() {
		t.Error("an entry without face buttons must not be used as a gamepad")
	}
	// Out-of-range sources do not panic.
	_, m, _ = parseSDLMapping("03000000aaaa00000004000000000000,Pad,a:b0,b:b1,leftx:a9,lefty:a1,rightx:a2,righty:a3,lefttrigger:b99,platform:Windows,")
	m.capture(joyInput{pov: -1})
}

func TestLookupPicksTheMatchingUsableEntry(t *testing.T) {
	db := "# header\n" +
		"03000000321500000710000000000000,Razer Raiju TE,leftx:a0,platform:Windows,\n" + // not usable: skipped
		raijuTELine + "\n" +
		"03000000111100002222000000000000,Other,a:b0,platform:Windows,\n"
	m := lookupSDLMapping(db, 0x1532, 0x1007)
	if m == nil || m.name != "Razer Raiju TE" || m.src["lefttrigger"].kind != sdlAxis {
		t.Fatalf("got %+v", m)
	}
	if lookupSDLMapping(db, 0x1532, 0x9999) != nil {
		t.Error("unknown pad must not match")
	}
}

func TestRaijuPSButtonIsGuideAndTheTouchpadClickIsNotAnXboxButton(t *testing.T) {
	m := raijuTE(t) // the database entry names neither
	m.withDS4Defaults()
	in := raijuIdle()
	in.buttons = 1 << 12
	if st := m.capture(in); st.Buttons != 0x0400 {
		t.Errorf("PS button: %#04x, want Guide 0x0400", st.Buttons)
	}
	// The touchpad click belongs to the touchpad-as-mouse, not to the pad state.
	in = raijuIdle()
	in.buttons = 1 << 13
	if st := m.capture(in); st.Buttons != 0 {
		t.Errorf("touchpad click leaked into the pad buttons: %#04x", st.Buttons)
	}
	// An entry that already names a guide button keeps its own choice.
	_, own, _ := parseSDLMapping("03000000aaaa00000005000000000000,Pad,a:b0,b:b1,leftx:a0,lefty:a1,rightx:a2,righty:a3,guide:b5,platform:Windows,")
	own.withDS4Defaults()
	if own.src["guide"].index != 5 {
		t.Errorf("defaults overrode the entry: %+v", own.src["guide"])
	}
}

func TestIsDS4Family(t *testing.T) {
	for _, c := range []struct {
		vid, pid uint16
		want     bool
	}{{0x1532, 0x1007, true}, {0x054C, 0x09CC, true}, {0x1532, 0x0A29, false}, {0x045E, 0x028E, false}} {
		if got := isDS4Family(c.vid, c.pid); got != c.want {
			t.Errorf("%04x:%04x = %v", c.vid, c.pid, got)
		}
	}
}
