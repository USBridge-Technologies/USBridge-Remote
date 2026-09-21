package usbpass

// A synthetic Xbox 360 wired controller (045E:028E) for the USB/IP export.
//
// Xbox One (GIP) pads cannot be passed through raw to a Windows importer: the
// Windows driver waits for an Announce, runs GIP authentication and is timing
// sensitive. Instead the client presents whatever gamepad it has as a genuine
// Xbox 360 wired controller. The importer (usbip-win2's VHCI on the agent)
// enumerates it like real hardware and Windows binds its own inbox `xusb22.sys`
// (xusb22.inf matches USB\Vid_045E&Pid_028E), so games see a normal XInput
// controller -- no ViGEmBus, no Zadig, and nothing to change on the agent.
//
// Descriptors follow a real controller's (lsusb dumps / Linux xpad): four
// interfaces, of which interface 0 (FF/5D/01, endpoints 0x81 IN / 0x01 OUT) is
// the gamepad itself.

import (
	"context"
	"encoding/binary"
	"sync"
)

const (
	x360VID       = 0x045E
	x360PID       = 0x028E
	x360BCDDevice = 0x0114

	x360EPIn  = 0x81 // gamepad state, host reads
	x360EPOut = 0x01 // rumble / LED, host writes

	x360ReportLen = 20
	x360Queue     = 8
)

// X360State is one snapshot of the gamepad in XInput terms.
type X360State struct {
	// Buttons uses the XInput wButtons layout (which is also Moonlight's):
	// DPAD U/D/L/R 0x0001-0x0008, START 0x0010, BACK 0x0020, L3 0x0040,
	// R3 0x0080, LB 0x0100, RB 0x0200, GUIDE 0x0400, A 0x1000, B 0x2000,
	// X 0x4000, Y 0x8000.
	Buttons uint16
	LT, RT  uint8
	LX, LY  int16 // positive Y is up, as in XInput
	RX, RY  int16
}

// report encodes the state as the 20-byte interrupt-IN report a real Xbox 360
// controller sends: 00 14, wButtons, LT, RT, four 16-bit axes, six reserved bytes.
func (s X360State) report() []byte {
	r := make([]byte, x360ReportLen)
	r[0], r[1] = 0x00, x360ReportLen
	binary.LittleEndian.PutUint16(r[2:], s.Buttons)
	r[4], r[5] = s.LT, s.RT
	binary.LittleEndian.PutUint16(r[6:], uint16(s.LX))
	binary.LittleEndian.PutUint16(r[8:], uint16(s.LY))
	binary.LittleEndian.PutUint16(r[10:], uint16(s.RX))
	binary.LittleEndian.PutUint16(r[12:], uint16(s.RY))
	return r
}

// X360Command is something the importer's driver wrote to the OUT endpoint.
type X360Command struct {
	Rumble      bool
	Left, Right uint8 // motor strengths 0-255
	LED         bool
	LEDPattern  uint8
}

// parseX360Out decodes the driver's OUT packets: rumble is `00 08 00 L R 00 00 00`,
// the LED ring is `01 03 pattern`. Anything else is reported as neither.
func parseX360Out(d []byte) X360Command {
	var c X360Command
	if len(d) >= 5 && d[0] == 0x00 && d[1] == 0x08 {
		c.Rumble, c.Left, c.Right = true, d[3], d[4]
	} else if len(d) >= 3 && d[0] == 0x01 && d[1] == 0x03 {
		c.LED, c.LEDPattern = true, d[2]
	}
	return c
}

func x360Endpoint(addr, interval byte) []byte {
	return []byte{7, 0x05, addr, 0x03, 0x20, 0x00, interval}
}

func x360ConfigDesc() []byte {
	d := []byte{9, 0x02, 0, 0, 4, 1, 0, 0xA0, 250}

	// Interface 0: the gamepad (FF/5D/01).
	d = append(d, 9, 0x04, 0, 0, 2, 0xFF, 0x5D, 0x01, 0)
	d = append(d, 0x11, 0x21, 0x00, 0x01, 0x01, 0x25, 0x81, 0x14, 0x00, 0x00, 0x00, 0x00, 0x13, 0x01, 0x08, 0x00, 0x00)
	d = append(d, x360Endpoint(0x81, 4)...)
	d = append(d, x360Endpoint(0x01, 8)...)

	// Interface 1: audio/headset plugin (FF/5D/03).
	d = append(d, 9, 0x04, 1, 0, 4, 0xFF, 0x5D, 0x03, 0)
	d = append(d, 0x1B, 0x21, 0x00, 0x01, 0x01, 0x01, 0x82, 0x40, 0x01, 0x02, 0x20, 0x16, 0x83,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x16, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
	d = append(d, x360Endpoint(0x82, 2)...)
	d = append(d, x360Endpoint(0x02, 4)...)
	d = append(d, x360Endpoint(0x83, 64)...)
	d = append(d, x360Endpoint(0x03, 16)...)

	// Interface 2: plugin module (FF/5D/02).
	d = append(d, 9, 0x04, 2, 0, 1, 0xFF, 0x5D, 0x02, 0)
	d = append(d, 0x09, 0x21, 0x00, 0x01, 0x01, 0x22, 0x84, 0x07, 0x00)
	d = append(d, x360Endpoint(0x84, 16)...)

	// Interface 3: Xbox security method (FF/FD/13), string index 4.
	d = append(d, 9, 0x04, 3, 0, 0, 0xFF, 0xFD, 0x13, 4)
	d = append(d, 0x06, 0x41, 0x00, 0x01, 0x01, 0x03)

	binary.LittleEndian.PutUint16(d[2:], uint16(len(d)))
	return d
}

func x360DeviceDesc() []byte {
	d := []byte{
		18, 0x01, 0x00, 0x02, // bcdUSB 2.00
		0xFF, 0xFF, 0xFF, 8, // vendor class, bMaxPacketSize0 = 8
		0, 0, 0, 0, 0, 0, // VID/PID/bcdDevice, filled below
		1, 2, 3, 1, // strings: manufacturer, product, serial; one configuration
	}
	binary.LittleEndian.PutUint16(d[8:], x360VID)
	binary.LittleEndian.PutUint16(d[10:], x360PID)
	binary.LittleEndian.PutUint16(d[12:], x360BCDDevice)
	return d
}

// X360Backend serves the controller's USB traffic.
type X360Backend struct {
	deviceDesc, configDesc []byte
	strs                   map[uint8][]byte
	onCommand              func(X360Command)
	onClose                func() // stops the state source; runs once
	closeOnce              sync.Once

	mu     sync.Mutex
	last    X360State
	pending chan []byte
	turn    sync.Mutex // one waiting IN URB at a time keeps report order
}

// NewX360Backend returns a controller that starts neutral. serial is the USB
// serial string (make it stable per physical gamepad so Windows keeps one
// device identity); onCommand receives rumble/LED commands from the importer
// and may be nil.
func NewX360Backend(serial string, onCommand func(X360Command)) *X360Backend {
	if serial == "" {
		serial = "USBRIDGE01"
	}
	b := &X360Backend{
		deviceDesc: x360DeviceDesc(),
		configDesc: x360ConfigDesc(),
		onCommand:  onCommand,
		pending:    make(chan []byte, x360Queue),
		strs: map[uint8][]byte{
			1: hidGenString("©Microsoft Corporation"),
			2: hidGenString("Controller"),
			3: hidGenString(serial),
			4: hidGenString("Xbox Security Method 3, Version 1.00, © 2005 Microsoft Corporation. All rights reserved."),
		},
	}
	// A real controller has a state to report as soon as the driver polls.
	b.pending <- b.last.report()
	return b
}

// SetState publishes a new gamepad state. Unchanged states are not resent (a
// real controller only reports changes), and when the importer polls slower
// than the source updates the oldest queued report is dropped so input never lags.
func (b *X360Backend) SetState(s X360State) {
	b.mu.Lock()
	if s == b.last {
		b.mu.Unlock()
		return
	}
	b.last = s
	b.mu.Unlock()
	rep := s.report()
	for {
		select {
		case b.pending <- rep:
			return
		default:
			select {
			case <-b.pending:
			default:
			}
		}
	}
}

// ExportedDevice wraps the backend as an exportable USB/IP device.
func (b *X360Backend) ExportedDevice(busID string) *ExportedDevice {
	return &ExportedDevice{
		BusID:      busID,
		Path:       "/sys/devices/usbridge/" + busID,
		Busnum:     8,
		Devnum:     1,
		Speed:      2, // full speed, like the real controller
		VID:        x360VID,
		PID:        x360PID,
		BCDDevice:  x360BCDDevice,
		Class:      0xFF,
		SubClass:   0xFF,
		Protocol:   0xFF,
		ConfigVal:  1,
		NumConfigs: 1,
		Interfaces: [][3]uint8{{0xFF, 0x5D, 0x01}, {0xFF, 0x5D, 0x03}, {0xFF, 0x5D, 0x02}, {0xFF, 0xFD, 0x13}},
		DeviceDesc: b.deviceDesc,
		ConfigDesc: b.configDesc,
		Backend:    b,
	}
}

func (b *X360Backend) HandleControl(_ context.Context, setup [8]byte, wLength int, _ []byte) (int32, []byte) {
	bm, req := setup[0], setup[1]
	wValue := binary.LittleEndian.Uint16(setup[2:4])

	clip := func(p []byte) (int32, []byte) {
		if wLength < len(p) {
			p = p[:wLength]
		}
		return 0, append([]byte(nil), p...)
	}

	if bm == 0x80 && req == 0x06 { // GET_DESCRIPTOR
		idx := uint8(wValue)
		switch uint8(wValue >> 8) {
		case 0x01:
			return clip(b.deviceDesc)
		case 0x02:
			return clip(b.configDesc)
		case 0x03:
			if idx == 0 {
				return clip([]byte{4, 0x03, 0x09, 0x04})
			}
			if s, ok := b.strs[idx]; ok {
				return clip(s)
			}
		}
		return errnoEPIPE, nil // includes the MS OS string (0xEE): a real controller has none
	}

	switch {
	case bm == 0x00 || bm == 0x01 || bm == 0x02: // standard host-to-device
		return 0, nil
	case bm == 0x80 && req == 0x00, bm == 0x81 && req == 0x00, bm == 0x82 && req == 0x00: // GET_STATUS
		return clip([]byte{0, 0})
	case bm == 0x80 && req == 0x08: // GET_CONFIGURATION
		return clip([]byte{1})
	case bm == 0x81 && req == 0x0A: // GET_INTERFACE
		return clip([]byte{0})
	}
	return errnoEPIPE, nil
}

func (b *X360Backend) HandleBulk(ctx context.Context, ep uint8, dirIn bool, length int, outData []byte) (int32, []byte) {
	if !dirIn {
		// Rumble/LED on 0x01; the other OUT endpoints belong to the unused
		// audio/plugin interfaces and are simply acknowledged.
		if ep&0x7f == x360EPOut&0x7f && b.onCommand != nil {
			if c := parseX360Out(outData); c.Rumble || c.LED {
				b.onCommand(c)
			}
		}
		return 0, nil
	}
	if ep&0x7f != x360EPIn&0x7f {
		// The unused interfaces' IN endpoints never have anything to say; a
		// real device NAKs them until the host gives up.
		<-ctx.Done()
		return errnoEPIPE, nil
	}
	b.turn.Lock()
	defer b.turn.Unlock()
	select {
	case rep := <-b.pending:
		if len(rep) > length {
			rep = rep[:length]
		}
		return 0, rep
	case <-ctx.Done():
		return errnoEPIPE, nil
	}
}

func (b *X360Backend) Close() error {
	b.closeOnce.Do(func() {
		if b.onClose != nil {
			b.onClose()
		}
	})
	return nil
}
