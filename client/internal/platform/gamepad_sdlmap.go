//go:build windows || (linux && !android)

package platform

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// SDL game controller mappings ("gamecontrollerdb.txt").
//
// A DirectInput gamepad reaches WinMM as raw axes and buttons with no meaning
// attached. Its Xbox-style meaning ("this button is A, that axis is the left
// trigger") is what SDL_GameControllerDB records for thousands of pads, and
// what Steam and most engines use. Reading that database instead of assuming
// one layout is what makes a PlayStation-compatible pad (Razer Raiju, DualShock
// clones, ...) come out right: its sticks, triggers and face buttons sit on
// different axes and buttons than an Xbox pad's.
//
// The parser and the state conversion are OS-independent; only the database
// lookup and the WinMM plumbing are Windows-specific (gamepad_sdldb_windows.go,
// gamepad_capture_windows.go).

// sdlKind is what a mapping entry points at on the physical pad.
type sdlKind int

const (
	sdlNone sdlKind = iota
	sdlButton
	sdlAxis
	sdlHat
)

// sdlSource is one "leftx:a0" / "a:b1" / "dpup:h0.1" value.
type sdlSource struct {
	kind sdlKind
	// index is the button number, the axis number, or the hat direction bit
	// (1 up, 2 right, 4 down, 8 left).
	index int
	// half restricts an axis to its positive (+1) or negative (-1) half
	// ("+a3", "-a3"); 0 is the whole range.
	half   int
	invert bool // "a3~"
}

// sdlMapping is one database line, keyed by SDL's own control names.
type sdlMapping struct {
	name string
	src  map[string]sdlSource
}

// Moonlight (XInput) button flags for the SDL button names.
var sdlButtonFlags = map[string]uint16{
	"dpup":          0x0001,
	"dpdown":        0x0002,
	"dpleft":        0x0004,
	"dpright":       0x0008,
	"start":         0x0010,
	"back":          0x0020,
	"leftstick":     0x0040,
	"rightstick":    0x0080,
	"leftshoulder":  0x0100,
	"rightshoulder": 0x0200,
	"guide":         0x0400,
	"a":        0x1000,
	"b":        0x2000,
	"x":        0x4000,
	"y":        0x8000,
}

// parseSDLSource parses "b1", "a3", "+a3", "-a3", "a3~" or "h0.4".
func parseSDLSource(v string) (sdlSource, bool) {
	s := sdlSource{}
	switch {
	case strings.HasPrefix(v, "+"):
		s.half, v = 1, v[1:]
	case strings.HasPrefix(v, "-"):
		s.half, v = -1, v[1:]
	}
	if strings.HasSuffix(v, "~") {
		s.invert, v = true, v[:len(v)-1]
	}
	if len(v) < 2 {
		return s, false
	}
	switch v[0] {
	case 'b':
		n, err := strconv.Atoi(v[1:])
		if err != nil || n < 0 {
			return s, false
		}
		s.kind, s.index = sdlButton, n
	case 'a':
		n, err := strconv.Atoi(v[1:])
		if err != nil || n < 0 {
			return s, false
		}
		s.kind, s.index = sdlAxis, n
	case 'h':
		// "h<hat>.<mask>": only hat 0 exists on WinMM.
		hat, mask, ok := strings.Cut(v[1:], ".")
		if !ok || hat != "0" {
			return s, false
		}
		m, err := strconv.Atoi(mask)
		if err != nil || m <= 0 || m > 15 {
			return s, false
		}
		s.kind, s.index = sdlHat, m
	default:
		return s, false
	}
	return s, true
}

// parseSDLMapping parses one "guid,name,a:b1,..." line. Comment lines and
// lines that are not a mapping return ok=false.
func parseSDLMapping(line string) (guid string, m *sdlMapping, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil, false
	}
	parts := strings.Split(line, ",")
	if len(parts) < 3 {
		return "", nil, false
	}
	m = &sdlMapping{name: parts[1], src: map[string]sdlSource{}}
	for _, kv := range parts[2:] {
		k, v, found := strings.Cut(kv, ":")
		if !found || k == "platform" || k == "crc" || k == "hint" {
			continue
		}
		if src, good := parseSDLSource(v); good {
			m.src[k] = src
		}
	}
	return strings.ToLower(parts[0]), m, true
}

// usable reports whether the mapping describes a whole gamepad: the face
// buttons and both sticks. Entries that only name a few controls (a foot pedal,
// a flight stick) are not used to drive a gamepad.
func (m *sdlMapping) usable() bool {
	for _, k := range []string{"a", "b", "leftx", "lefty", "rightx", "righty"} {
		if m.src[k].kind == sdlNone {
			return false
		}
	}
	return true
}

// joyInput is one WinMM sample in device-independent form.
type joyInput struct {
	// axes are WinMM's X, Y, Z, R, U, V, each scaled to -1 (minimum) .. +1 (maximum).
	axes    [6]float64
	buttons uint32
	// pov is the hat in hundredths of a degree, or -1 when centred.
	pov int
}

// sdlAxisToJoy maps an SDL DirectInput axis number (X, Y, Z, Rx, Ry, Rz) to
// the position of that axis in joyInput.axes (WinMM's X, Y, Z, R, U, V).
//
// WinMM numbers the axes after the first three by the order the pad's HID
// report declares them, not by usage. For the Razer Raiju TE (1532:1007) the
// sticks' second axes sit on R and the analog triggers on U and V, measured
// live: SDL's Rz is WinMM's R, Ry (right trigger) is U and Rx (left trigger)
// is V.
var sdlAxisToJoy = [6]int{0, 1, 2, 5, 4, 3}

// axisValue is the source's value in -1..+1 (0..1 for a half axis), or false
// when the source is not an axis or is out of range for this pad.
func (in *joyInput) axisValue(s sdlSource) (float64, bool) {
	if s.kind != sdlAxis || s.index >= len(sdlAxisToJoy) {
		return 0, false
	}
	v := in.axes[sdlAxisToJoy[s.index]]
	if s.invert {
		v = -v
	}
	switch s.half {
	case 1:
		v = math.Max(v, 0)
	case -1:
		v = math.Max(-v, 0)
	}
	return v, true
}

// hatMask converts a WinMM hat to SDL's direction bits (1 up, 2 right, 4 down, 8 left).
func hatMask(pov int) int {
	if pov < 0 || pov >= 36000 {
		return 0
	}
	deg := pov / 100
	mask := 0
	if deg <= 45 || deg >= 315 {
		mask |= 1
	}
	if deg >= 45 && deg <= 135 {
		mask |= 2
	}
	if deg >= 135 && deg <= 225 {
		mask |= 4
	}
	if deg >= 225 && deg <= 315 {
		mask |= 8
	}
	return mask
}

// down reports whether a source counts as pressed.
func (in *joyInput) down(s sdlSource) bool {
	switch s.kind {
	case sdlButton:
		return s.index < 32 && in.buttons&(1<<uint(s.index)) != 0
	case sdlHat:
		return hatMask(in.pov)&s.index != 0
	case sdlAxis:
		v, _ := in.axisValue(s)
		if s.half == 0 {
			v = (v + 1) / 2 // a full-range axis rests in the middle
		}
		return v > 0.5
	}
	return false
}

func clampUnit(v float64) float64 { return math.Max(-1, math.Min(1, v)) }

// stick reads a stick axis as int16. Y axes are flipped: SDL (and DirectInput)
// have down positive, Moonlight/XInput have up positive.
func (in *joyInput) stick(s sdlSource, flip bool) int16 {
	v, ok := in.axisValue(s)
	if !ok {
		return 0
	}
	if flip {
		v = -v
	}
	return int16(math.Round(clampUnit(v) * 32767))
}

// trigger reads a trigger as 0..255 from an axis (rest at its minimum) or a
// button.
func (in *joyInput) trigger(s sdlSource) uint8 {
	var v float64
	switch s.kind {
	case sdlAxis:
		a, _ := in.axisValue(s)
		if s.half == 0 {
			a = (a + 1) / 2
		}
		v = a
	case sdlButton, sdlHat:
		if in.down(s) {
			v = 1
		}
	}
	return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255))
}

// capture converts a raw sample to the Moonlight controller state using the
// mapping.
func (m *sdlMapping) capture(in joyInput) GamepadCaptureState {
	var st GamepadCaptureState
	dpadMapped := false
	for name, flag := range sdlButtonFlags {
		src, ok := m.src[name]
		if !ok {
			continue
		}
		if flag&0x000F != 0 {
			dpadMapped = true
		}
		if in.down(src) {
			st.Buttons |= flag
		}
	}
	if !dpadMapped {
		// The entry names no D-pad: the pad's hat is the D-pad.
		mask := hatMask(in.pov)
		for bit, flag := range map[int]uint16{1: 0x0001, 4: 0x0002, 8: 0x0004, 2: 0x0008} {
			if mask&bit != 0 {
				st.Buttons |= flag
			}
		}
	}
	st.LeftX = in.stick(m.src["leftx"], false)
	st.LeftY = in.stick(m.src["lefty"], true)
	st.RightX = in.stick(m.src["rightx"], false)
	st.RightY = in.stick(m.src["righty"], true)
	st.LeftTrigger = in.trigger(m.src["lefttrigger"])
	st.RightTrigger = in.trigger(m.src["righttrigger"])
	return st
}

// sdlGUIDKey is the first 20 hex digits of the GUID SDL gives a DirectInput
// pad: bus type 3 (USB), then the vendor and product ids as little-endian
// 16-bit values, each followed by 0000. The remaining 12 digits (version and
// padding) vary between entries and are ignored.
func sdlGUIDKey(vid, pid uint16) string {
	le := func(v uint16) string { return fmt.Sprintf("%02x%02x", v&0xFF, v>>8) }
	return "03000000" + le(vid) + "0000" + le(pid)
}

// lookupSDLMapping finds the first usable mapping for a DirectInput pad with
// this vendor/product id in a gamecontrollerdb.txt body.
func lookupSDLMapping(db string, vid, pid uint16) *sdlMapping {
	key := sdlGUIDKey(vid, pid)
	for _, line := range strings.Split(db, "\n") {
		if len(line) < len(key) || !strings.EqualFold(line[:len(key)], key) {
			continue
		}
		if _, m, ok := parseSDLMapping(line); ok && m.usable() {
			return m
		}
	}
	return nil
}

// isDS4Family reports whether a USB id is a pad with the DualShock 4 button
// layout (Sony's own and the Razer Raiju family). Their HID descriptor numbers
// the buttons square, cross, circle, triangle, L1, R1, L2, R2, Share, Options,
// L3, R3, PS, touchpad click, but the SDL entries of some of them (the Raiju TE
// on Windows) name no guide or touchpad button.
func isDS4Family(vid, pid uint16) bool {
	switch vid {
	case 0x054C:
		return pid == 0x05C4 || pid == 0x09CC || pid == 0x0BA0
	case 0x1532:
		switch pid {
		case 0x1000, 0x1004, 0x1007, 0x1009, 0x100A:
			return true
		}
	}
	return false
}

// withDS4Defaults adds the PS button (guide, button 13) when the mapping does not
// name it. The touchpad click (button 14) is deliberately not mapped to an Xbox
// button: the touchpad is a relative mouse (gamepad_touchpad.go), and its click
// the mouse's left button.
func (m *sdlMapping) withDS4Defaults() {
	if _, ok := m.src["guide"]; !ok {
		m.src["guide"] = sdlSource{kind: sdlButton, index: 12}
	}
}
