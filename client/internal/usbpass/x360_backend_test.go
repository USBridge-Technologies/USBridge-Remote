package usbpass

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"
)

func TestX360ConfigDescriptorLayout(t *testing.T) {
	cfg := x360ConfigDesc()
	if int(binary.LittleEndian.Uint16(cfg[2:])) != len(cfg) || len(cfg) != 153 {
		t.Fatalf("wTotalLength=%d, len=%d; a real controller's configuration is 153 bytes",
			binary.LittleEndian.Uint16(cfg[2:]), len(cfg))
	}
	if cfg[4] != 4 {
		t.Fatalf("bNumInterfaces = %d, want 4", cfg[4])
	}
	// Walk the descriptors: interfaces in order with the expected class triples
	// and endpoint counts, and every descriptor sized correctly.
	type ifc struct{ class, sub, proto, eps byte }
	want := []ifc{{0xFF, 0x5D, 0x01, 2}, {0xFF, 0x5D, 0x03, 4}, {0xFF, 0x5D, 0x02, 1}, {0xFF, 0xFD, 0x13, 0}}
	var got []ifc
	eps := map[byte]bool{}
	for i := int(cfg[0]); i < len(cfg); {
		l := int(cfg[i])
		if l == 0 || i+l > len(cfg) {
			t.Fatalf("bad descriptor length %d at %d", l, i)
		}
		switch cfg[i+1] {
		case 0x04:
			got = append(got, ifc{cfg[i+5], cfg[i+6], cfg[i+7], cfg[i+4]})
		case 0x05:
			eps[cfg[i+2]] = true
		}
		i += l
	}
	if len(got) != len(want) {
		t.Fatalf("interfaces: %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("interface %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, ep := range []byte{0x81, 0x01, 0x82, 0x02, 0x83, 0x03, 0x84} {
		if !eps[ep] {
			t.Fatalf("endpoint %#x missing", ep)
		}
	}
}

func TestX360DeviceDescriptor(t *testing.T) {
	d := x360DeviceDesc()
	if len(d) != 18 || d[4] != 0xFF || d[5] != 0xFF || d[6] != 0xFF {
		t.Fatalf("device descriptor: % X", d)
	}
	if binary.LittleEndian.Uint16(d[8:]) != 0x045E || binary.LittleEndian.Uint16(d[10:]) != 0x028E {
		t.Fatalf("VID/PID = %04x:%04x", binary.LittleEndian.Uint16(d[8:]), binary.LittleEndian.Uint16(d[10:]))
	}
}

func TestX360ReportEncoding(t *testing.T) {
	s := X360State{Buttons: 0x1000 | 0x0100 | 0x0004, LT: 200, RT: 17, LX: -32768, LY: 32767, RX: 1, RY: -1}
	r := s.report()
	want := []byte{0x00, 0x14, 0x04, 0x11, 200, 17, 0x00, 0x80, 0xFF, 0x7F, 0x01, 0x00, 0xFF, 0xFF, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(r, want) {
		t.Fatalf("report\n got % X\nwant % X", r, want)
	}
	if got := (X360State{}).report(); len(got) != 20 || got[0] != 0 || got[1] != 0x14 {
		t.Fatalf("neutral report: % X", got)
	}
}

func TestParseX360Out(t *testing.T) {
	if c := parseX360Out([]byte{0x00, 0x08, 0x00, 0xC0, 0x40, 0, 0, 0}); !c.Rumble || c.Left != 0xC0 || c.Right != 0x40 {
		t.Fatalf("rumble parse: %+v", c)
	}
	if c := parseX360Out([]byte{0x01, 0x03, 0x06}); !c.LED || c.LEDPattern != 6 {
		t.Fatalf("LED parse: %+v", c)
	}
	if c := parseX360Out([]byte{0x05, 0x05}); c.Rumble || c.LED {
		t.Fatalf("unknown command decoded: %+v", c)
	}
}

func TestX360InterruptIn(t *testing.T) {
	b := NewX360Backend("SER1", nil)
	ctx := context.Background()

	// The very first poll gets the initial neutral state.
	st, rep := b.HandleBulk(ctx, x360EPIn, true, 32, nil)
	if st != 0 || !bytes.Equal(rep, X360State{}.report()) {
		t.Fatalf("first poll: st=%d % X", st, rep)
	}
	// Unchanged state is not resent: the next poll waits until unlinked.
	c, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if st, _ := b.HandleBulk(c, x360EPIn, true, 32, nil); st == 0 {
		t.Fatal("idle poll must not complete with data")
	}
	// Changes come out in order, and a queue overrun drops the oldest.
	for i := 0; i < x360Queue+3; i++ {
		b.SetState(X360State{LX: int16(i + 1)})
	}
	first := int16(3 + 1) // the 3 oldest were dropped
	for k := 0; k < x360Queue; k++ {
		st, rep := b.HandleBulk(ctx, x360EPIn, true, 32, nil)
		if st != 0 || int16(binary.LittleEndian.Uint16(rep[6:])) != first+int16(k) {
			t.Fatalf("report %d: st=%d LX=%d, want %d", k, st, int16(binary.LittleEndian.Uint16(rep[6:])), first+int16(k))
		}
	}
	// Clipped to the URB length.
	b.SetState(X360State{Buttons: 1})
	if _, rep := b.HandleBulk(ctx, x360EPIn, true, 4, nil); len(rep) != 4 {
		t.Fatalf("report not clipped: %v", rep)
	}
}

func TestX360OutAndOtherEndpoints(t *testing.T) {
	var got []X360Command
	b := NewX360Backend("", func(c X360Command) { got = append(got, c) })
	ctx := context.Background()
	if st, _ := b.HandleBulk(ctx, x360EPOut, false, 8, []byte{0x00, 0x08, 0x00, 10, 20, 0, 0, 0}); st != 0 {
		t.Fatalf("rumble OUT st=%d", st)
	}
	if st, _ := b.HandleBulk(ctx, 0x02, false, 8, []byte{1, 2, 3}); st != 0 {
		t.Fatalf("OUT to an unused endpoint should be acknowledged, st=%d", st)
	}
	if len(got) != 1 || got[0].Left != 10 || got[0].Right != 20 {
		t.Fatalf("commands = %+v", got)
	}
	// An IN endpoint of an unused interface stays idle until unlinked.
	c, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if st, _ := b.HandleBulk(c, 0x82, true, 32, nil); st == 0 {
		t.Fatal("unused IN endpoint returned data")
	}
}

func TestX360ControlRequests(t *testing.T) {
	b := NewX360Backend("ABC", nil)
	ctx := context.Background()
	ctl := func(bm, req byte, wValue, wLength uint16) (int32, []byte) {
		var s [8]byte
		s[0], s[1] = bm, req
		binary.LittleEndian.PutUint16(s[2:], wValue)
		binary.LittleEndian.PutUint16(s[6:], wLength)
		return b.HandleControl(ctx, s, int(wLength), nil)
	}
	if st, d := ctl(0x80, 0x06, 0x0100, 64); st != 0 || len(d) != 18 {
		t.Fatalf("device descriptor: st=%d len=%d", st, len(d))
	}
	if st, d := ctl(0x80, 0x06, 0x0200, 9); st != 0 || len(d) != 9 || binary.LittleEndian.Uint16(d[2:]) != 153 {
		t.Fatalf("config header: st=%d % X", st, d)
	}
	if st, d := ctl(0x80, 0x06, 0x0303, 255); st != 0 || len(d) < 2 || int(d[0]) != len(d) || d[1] != 0x03 {
		t.Fatalf("serial string: st=%d % X", st, d)
	}
	if st, _ := ctl(0x80, 0x06, 0x03EE, 255); st == 0 {
		t.Fatal("a real controller has no MS OS string descriptor")
	}
	if st, _ := ctl(0x00, 0x09, 1, 0); st != 0 {
		t.Fatal("SET_CONFIGURATION not acknowledged")
	}
	if st, d := ctl(0x80, 0x00, 0, 2); st != 0 || len(d) != 2 {
		t.Fatalf("GET_STATUS: st=%d %v", st, d)
	}
	exp := b.ExportedDevice("9-8")
	if exp.VID != 0x045E || exp.PID != 0x028E || len(exp.Interfaces) != 4 || exp.Class != 0xFF {
		t.Fatalf("exported device: %+v", exp)
	}
}
