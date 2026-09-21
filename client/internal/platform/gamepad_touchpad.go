package platform

import "errors"

// The touchpad of a DualShock 4-layout pad as a relative mouse.
//
// An Xbox 360 controller (what the host presents) has no touchpad, and WinMM
// does not expose one, so the pad's own HID input report is read alongside the
// normal capture: a finger's movement becomes relative mouse motion and the
// pad's click becomes the left mouse button.

// ErrNoTouchpad means the pad has no touchpad this client can read.
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
	// The USB input report is 64 bytes; finger 1 ends at byte 38.
	ds4TouchMinReport = 39
	// Mouse counts per touchpad unit (the pad is 1920 x 942 units), as a fraction so
	// nothing is lost to rounding: a swipe across the whole width moves the pointer
	// about 1150 counts.
	touchpadGainNum = 3
	touchpadGainDen = 5
)

// ds4Finger is finger 1 of a DS4-layout input report.
type ds4Finger struct {
	Active bool
	ID     uint8 // tracking id, changes with every new touch
	X, Y   int   // 0..1919, 0..941
}

// parseDS4Report reads finger 1 and the touchpad click from a DS4-layout USB
// input report (id 0x01): byte 7 bit 1 is the click; byte 35 is the finger's
// contact (bit 7 set = not touching, low 7 bits = tracking id) and bytes 36..38
// hold X and Y as two 12-bit values.
func parseDS4Report(rep []byte) (f ds4Finger, click bool, ok bool) {
	if len(rep) < ds4TouchMinReport || rep[0] != ds4ReportID {
		return ds4Finger{}, false, false
	}
	c := rep[35]
	f.Active = c&0x80 == 0
	f.ID = c & 0x7F
	f.X = int(rep[36]) | int(rep[37]&0x0F)<<8
	f.Y = int(rep[37]>>4) | int(rep[38])<<4
	return f, rep[7]&0x02 != 0, true
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
func (t *touchpadMouse) move(f ds4Finger) (dx, dy int) {
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

// touchpadDecoder feeds input reports to a callback as TouchpadEvents.
type touchpadDecoder struct {
	mouse     touchpadMouse
	clickDown bool
}

func (d *touchpadDecoder) feed(rep []byte, emit func(TouchpadEvent)) {
	f, click, ok := parseDS4Report(rep)
	if !ok {
		return
	}
	ev := TouchpadEvent{}
	ev.DX, ev.DY = d.mouse.move(f)
	if click != d.clickDown {
		d.clickDown = click
		ev.ClickChanged, ev.ClickDown = true, click
	}
	if ev.DX != 0 || ev.DY != 0 || ev.ClickChanged {
		emit(ev)
	}
}

// TouchpadCapture is a running touchpad reader.
type TouchpadCapture struct {
	stop func()
}

// Stop ends the reader; safe to call once.
func (c *TouchpadCapture) Stop() {
	if c != nil && c.stop != nil {
		c.stop()
	}
}
