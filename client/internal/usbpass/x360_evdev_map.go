package usbpass

// Linux evdev gamepad -> XInput state. Pure mapping (no OS calls) so it is
// testable anywhere; x360_evdev_linux.go feeds it real events.
//
// The codes are the kernel's input-event-codes.h ones as the xpad driver emits
// them for Xbox 360/One/Series pads.

const (
	evSyn = 0x00
	evKey = 0x01
	evAbs = 0x03
	evFF  = 0x15

	synReport  = 0
	synDropped = 3

	absX, absY, absZ    = 0x00, 0x01, 0x02 // left stick, left trigger
	absRX, absRY, absRZ = 0x03, 0x04, 0x05 // right stick, right trigger
	absHat0X, absHat0Y  = 0x10, 0x11
	absCount            = 0x12

	btnSouth = 0x130
)

// evdevButtons maps key codes to XInput wButtons bits.
var evdevButtons = map[uint16]uint16{
	0x130: 0x1000, // BTN_SOUTH  A
	0x131: 0x2000, // BTN_EAST   B
	0x133: 0x4000, // BTN_NORTH  X
	0x134: 0x8000, // BTN_WEST   Y
	0x136: 0x0100, // BTN_TL     LB
	0x137: 0x0200, // BTN_TR     RB
	0x13a: 0x0020, // BTN_SELECT Back
	0x13b: 0x0010, // BTN_START  Start
	0x13c: 0x0400, // BTN_MODE   Guide
	0x13d: 0x0040, // BTN_THUMBL L3
	0x13e: 0x0080, // BTN_THUMBR R3
	0x220: 0x0001, // BTN_DPAD_UP
	0x221: 0x0002, // BTN_DPAD_DOWN
	0x222: 0x0004, // BTN_DPAD_LEFT
	0x223: 0x0008, // BTN_DPAD_RIGHT
}

type evdevRange struct{ min, max int32 }

// evdevMapper accumulates evdev events into an X360State.
type evdevMapper struct {
	ranges  [absCount]evdevRange
	abs     [absCount]int32
	buttons uint16 // XInput bits held by key events (d-pad hat is added in state)
}

func newEvdevMapper() *evdevMapper {
	m := &evdevMapper{}
	for _, c := range []int{absX, absY, absRX, absRY} {
		m.ranges[c] = evdevRange{-32768, 32767}
	}
	m.ranges[absZ] = evdevRange{0, 1023}
	m.ranges[absRZ] = evdevRange{0, 1023}
	m.ranges[absHat0X] = evdevRange{-1, 1}
	m.ranges[absHat0Y] = evdevRange{-1, 1}
	// Axes rest at the middle of their range (triggers at the bottom).
	for _, c := range []int{absX, absY, absRX, absRY} {
		m.abs[c] = (m.ranges[c].min + m.ranges[c].max) / 2
	}
	return m
}

// setRange records a device axis' real range (EVIOCGABS).
func (m *evdevMapper) setRange(code uint16, min, max int32) {
	if int(code) < absCount && max > min {
		m.ranges[code] = evdevRange{min, max}
	}
}

func (m *evdevMapper) setKey(code uint16, down bool) {
	bit, ok := evdevButtons[code]
	if !ok {
		return
	}
	if down {
		m.buttons |= bit
	} else {
		m.buttons &^= bit
	}
}

func (m *evdevMapper) setAbs(code uint16, v int32) {
	if int(code) < absCount {
		m.abs[code] = v
	}
}

func scaleStick(v int32, r evdevRange) int16 {
	span := int64(r.max) - int64(r.min)
	if span <= 0 {
		return 0
	}
	x := (int64(v)-int64(r.min))*65535/span - 32768
	if x < -32768 {
		x = -32768
	}
	if x > 32767 {
		x = 32767
	}
	return int16(x)
}

func scaleTrigger(v int32, r evdevRange) uint8 {
	span := int64(r.max) - int64(r.min)
	if span <= 0 {
		return 0
	}
	x := (int64(v) - int64(r.min)) * 255 / span
	if x < 0 {
		x = 0
	}
	if x > 255 {
		x = 255
	}
	return uint8(x)
}

// invert flips an axis: evdev's Y grows downwards, XInput's grows upwards.
func invert(v int16) int16 {
	if v == -32768 {
		return 32767
	}
	return -v
}

func (m *evdevMapper) state() X360State {
	s := X360State{
		Buttons: m.buttons,
		LX:      scaleStick(m.abs[absX], m.ranges[absX]),
		LY:      invert(scaleStick(m.abs[absY], m.ranges[absY])),
		RX:      scaleStick(m.abs[absRX], m.ranges[absRX]),
		RY:      invert(scaleStick(m.abs[absRY], m.ranges[absRY])),
		LT:      scaleTrigger(m.abs[absZ], m.ranges[absZ]),
		RT:      scaleTrigger(m.abs[absRZ], m.ranges[absRZ]),
	}
	switch {
	case m.abs[absHat0X] < 0:
		s.Buttons |= 0x0004
	case m.abs[absHat0X] > 0:
		s.Buttons |= 0x0008
	}
	switch {
	case m.abs[absHat0Y] < 0:
		s.Buttons |= 0x0001
	case m.abs[absHat0Y] > 0:
		s.Buttons |= 0x0002
	}
	return s
}
