package platform

import "errors"

// The extras of a DualShock 4-layout pad: its touchpad, motion sensors and
// battery. WinMM and evdev only see the buttons and sticks, so the pad's own
// HID input report is read alongside the normal capture -- no driver beyond
// the OS's HID class one.
//
// A host with Sunshine's controller extensions gets them as they are (the
// touchpad contacts, the sensors, the battery), for its virtual DualShock 4.
// Any other host -- whose virtual pad is an Xbox 360 one with no touchpad --
// gets the touchpad as a relative mouse: a finger's movement becomes mouse
// motion and the pad's click the left mouse button.

// ErrNoTouchpad means the pad has no HID report this client can read.
var ErrNoTouchpad = errors.New("the pad has no readable touchpad")

// TouchpadEvent is one step of the touchpad-as-mouse: motion in mouse counts
// and/or a change of the click (left button).
type TouchpadEvent struct {
	DX, DY       int
	ClickChanged bool
	ClickDown    bool
}

const (
	ds4ReportID = 0x01
	// The USB input report is 64 bytes; finger 2 ends at byte 42.
	ds4TouchMinReport = 43
	// Touchpad size in pad units.
	DS4TouchpadWidth  = 1920
	DS4TouchpadHeight = 942
	// Nominal motion units of the report: 8192 per g, 16 per degree per
	// second (the pads' calibration only trims these).
	ds4AccelPerG    = 8192
	ds4GyroPerDegS  = 16
	standardGravity = 9.80665
	// Mouse counts per touchpad unit (the pad is 1920 x 942 units), as a fraction so
	// nothing is lost to rounding: a swipe across the whole width moves the pointer
	// about 1150 counts.
	touchpadGainNum = 3
	touchpadGainDen = 5
)

// DS4Finger is one touchpad contact of a DS4-layout input report.
type DS4Finger struct {
	Active bool
	ID     uint8 // tracking id, changes with every new touch
	X, Y   int   // 0..1919, 0..941
}

// DS4Report is what a DS4-layout USB input report (id 0x01) carries besides
// the buttons and sticks.
type DS4Report struct {
	Fingers [2]DS4Finger
	Click   bool
	// Gyro is pitch, yaw, roll; Accel is X, Y, Z -- raw, in the nominal units.
	Gyro, Accel [3]int16
	// Battery is byte 30: the level in tenths (low nibble), bit 4 the cable.
	// A clone that reports no battery leaves it 0.
	Battery uint8
}

func ds4Finger(b []byte) DS4Finger {
	return DS4Finger{
		Active: b[0]&0x80 == 0,
		ID:     b[0] & 0x7F,
		X:      int(b[1]) | int(b[2]&0x0F)<<8,
		Y:      int(b[2]>>4) | int(b[3])<<4,
	}
}

// ParseDS4Report reads a DS4-layout USB input report: byte 7 bit 1 is the
// click, 13-18 the gyro, 19-24 the accelerometer, 30 the battery, and bytes
// 35-38 and 39-42 the two contacts (bit 7 of the first byte set = not
// touching, its low 7 bits the tracking id, then X and Y as two 12-bit values).
func ParseDS4Report(rep []byte) (DS4Report, bool) {
	if len(rep) < ds4TouchMinReport || rep[0] != ds4ReportID {
		return DS4Report{}, false
	}
	r := DS4Report{Click: rep[7]&0x02 != 0, Battery: rep[30]}
	for i := 0; i < 3; i++ {
		r.Gyro[i] = int16(uint16(rep[13+2*i]) | uint16(rep[14+2*i])<<8)
		r.Accel[i] = int16(uint16(rep[19+2*i]) | uint16(rep[20+2*i])<<8)
	}
	r.Fingers[0] = ds4Finger(rep[35:39])
	r.Fingers[1] = ds4Finger(rep[39:43])
	return r, true
}

// parseDS4Report is ParseDS4Report's first finger and click.
func parseDS4Report(rep []byte) (f DS4Finger, click bool, ok bool) {
	r, ok := ParseDS4Report(rep)
	return r.Fingers[0], r.Click, ok
}

// HasMotion reports whether the pad has motion sensors at all: a resting pad
// always feels gravity, so an accelerometer reading exactly zero on every axis
// is a clone without one (a Razer Raiju).
func (r DS4Report) HasMotion() bool {
	return r.Accel != [3]int16{}
}

// AccelMS2 is the accelerometer in m/s^2 and GyroDegS the gyro in degrees per
// second, on the pad's axes -- which are Moonlight's (Sunshine and SDL use the
// DualShock's own), so a host's virtual DualShock 4 shows the same values.
func (r DS4Report) AccelMS2() (x, y, z float32) {
	k := float32(standardGravity / ds4AccelPerG)
	return float32(r.Accel[0]) * k, float32(r.Accel[1]) * k, float32(r.Accel[2]) * k
}

func (r DS4Report) GyroDegS() (x, y, z float32) {
	return float32(r.Gyro[0]) / ds4GyroPerDegS, float32(r.Gyro[1]) / ds4GyroPerDegS, float32(r.Gyro[2]) / ds4GyroPerDegS
}

// BatteryState turns byte 30 into a Moonlight battery state (LI_BATTERY_STATE_*)
// and percentage; ok is false for a pad that reports no battery (0).
func (r DS4Report) BatteryState() (state, percent uint8, ok bool) {
	if r.Battery == 0 {
		return 0, 0, false
	}
	level := r.Battery & 0x0F
	cable := r.Battery&0x10 != 0
	pct := uint8(level) * 10
	if pct > 100 {
		pct = 100
	}
	switch {
	case cable && level >= 11:
		return 0x05, 100, true // LI_BATTERY_STATE_FULL
	case cable:
		return 0x03, pct, true // LI_BATTERY_STATE_CHARGING
	default:
		return 0x02, pct, true // LI_BATTERY_STATE_DISCHARGING
	}
}

// touchpadMouse turns finger positions into relative motion.
type touchpadMouse struct {
	touching   bool
	id         uint8
	lastX      int
	lastY      int
	remX, remY int // what is left of the last step, in 1/touchpadGainDen counts
}

// move returns the mouse motion for the finger's new state. A new touch only
// sets the reference point, so lifting and re-placing a finger never makes the
// pointer jump.
func (t *touchpadMouse) move(f DS4Finger) (dx, dy int) {
	if !f.Active {
		t.touching = false
		t.remX, t.remY = 0, 0
		return 0, 0
	}
	if !t.touching || t.id != f.ID {
		t.touching, t.id = true, f.ID
		t.lastX, t.lastY = f.X, f.Y
		t.remX, t.remY = 0, 0
		return 0, 0
	}
	fx := (f.X-t.lastX)*touchpadGainNum + t.remX
	fy := (f.Y-t.lastY)*touchpadGainNum + t.remY
	dx, dy = fx/touchpadGainDen, fy/touchpadGainDen // Go truncates toward zero
	t.remX, t.remY = fx-dx*touchpadGainDen, fy-dy*touchpadGainDen
	t.lastX, t.lastY = f.X, f.Y
	return dx, dy
}

// TouchpadMouse turns the touchpad of successive reports into relative mouse
// motion and left-button edges, for a host without the controller extensions.
type TouchpadMouse struct {
	mouse     touchpadMouse
	clickDown bool
}

// Feed returns the mouse step for one report; ok is false when there is
// nothing to send.
func (d *TouchpadMouse) Feed(r DS4Report) (ev TouchpadEvent, ok bool) {
	ev.DX, ev.DY = d.mouse.move(r.Fingers[0])
	if r.Click != d.clickDown {
		d.clickDown = r.Click
		ev.ClickChanged, ev.ClickDown = true, r.Click
	}
	return ev, ev.DX != 0 || ev.DY != 0 || ev.ClickChanged
}

// Release is the event that lets go of a held click, if one is held.
func (d *TouchpadMouse) Release() (TouchpadEvent, bool) {
	if !d.clickDown {
		return TouchpadEvent{}, false
	}
	d.clickDown = false
	return TouchpadEvent{ClickChanged: true}, true
}

// touchpadDecoder feeds raw input reports to a callback as TouchpadEvents.
type touchpadDecoder struct {
	TouchpadMouse
}

func (d *touchpadDecoder) feed(rep []byte, emit func(TouchpadEvent)) {
	r, ok := ParseDS4Report(rep)
	if !ok {
		return
	}
	if ev, ok := d.Feed(r); ok {
		emit(ev)
	}
}

// TouchpadCapture is a running reader of a pad's HID input report.
type TouchpadCapture struct {
	stop func()
}

// Stop ends the reader; safe to call once.
func (c *TouchpadCapture) Stop() {
	if c != nil && c.stop != nil {
		c.stop()
	}
}
