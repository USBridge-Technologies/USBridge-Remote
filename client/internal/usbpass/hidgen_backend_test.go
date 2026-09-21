package usbpass

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

type fakeHIDPort struct {
	ids      [ppReportTypes]map[uint8]bool
	sets     [][]byte
	setErr   error
	feature  map[uint8][]byte
	lastType uint8
}

func (p *fakeHIDPort) idSets() [ppReportTypes]map[uint8]bool { return p.ids }
func (p *fakeHIDPort) setReport(typ, id uint8, data []byte, hasIDs bool) error {
	p.lastType = typ
	p.sets = append(p.sets, append([]byte{id}, data...))
	return p.setErr
}
func (p *fakeHIDPort) getReport(typ, id uint8, length int, hasIDs bool) ([]byte, error) {
	r, ok := p.feature[id]
	if !ok {
		return nil, errors.New("no such report")
	}
	if len(r) > length {
		r = r[:length]
	}
	return r, nil
}

func newFakeBackend(t *testing.T) (*hidGenBackend, *fakeHIDPort) {
	t.Helper()
	mouse, err := reconstructReportDescriptor(syntheticMousePP())
	if err != nil {
		t.Fatal(err)
	}
	port := &fakeHIDPort{
		ids:     [ppReportTypes]map[uint8]bool{{0: true}, {}, {2: true}},
		feature: map[uint8][]byte{2: {2, 0xAA, 0xBB}},
	}
	b := newHIDGenBackend(hidGenInfo{
		VID: 0x056A, PID: 0x0374, BCD: 0x0100,
		Manufacturer: "Wacom", Product: "Intuos S", Serial: "1EH00R",
		Interfaces: []*hidGenIface{{Number: 0, ReportDesc: mouse, HasIDs: false, MaxInput: 3, Ports: []hidGenPort{port}}},
	})
	return b, port
}

func ctl(bm, req byte, wValue, wIndex, wLength uint16) [8]byte {
	var s [8]byte
	s[0], s[1] = bm, req
	binary.LittleEndian.PutUint16(s[2:], wValue)
	binary.LittleEndian.PutUint16(s[4:], wIndex)
	binary.LittleEndian.PutUint16(s[6:], wLength)
	return s
}

func TestHIDGenDescriptors(t *testing.T) {
	b, _ := newFakeBackend(t)
	ctx := context.Background()

	st, dev := b.HandleControl(ctx, ctl(0x80, 0x06, 0x0100, 0, 18), 18, nil)
	if st != 0 || len(dev) != 18 {
		t.Fatalf("device descriptor: st=%d len=%d", st, len(dev))
	}
	if binary.LittleEndian.Uint16(dev[8:]) != 0x056A || binary.LittleEndian.Uint16(dev[10:]) != 0x0374 {
		t.Fatalf("VID/PID not carried into the device descriptor: % X", dev)
	}
	if dev[14] != 1 || dev[15] != 2 || dev[16] != 3 {
		t.Fatalf("string indices missing: % X", dev[14:17])
	}

	st, cfg := b.HandleControl(ctx, ctl(0x80, 0x06, 0x0200, 0, 255), 255, nil)
	if st != 0 || int(binary.LittleEndian.Uint16(cfg[2:])) != len(cfg) {
		t.Fatalf("config descriptor wTotalLength does not match its size: st=%d % X", st, cfg)
	}
	// The HID descriptor inside the config must advertise the served report descriptor's exact length.
	st, rd := b.HandleControl(ctx, ctl(0x81, 0x06, 0x2200, 0, 1024), 1024, nil)
	if st != 0 {
		t.Fatalf("report descriptor: st=%d", st)
	}
	if got := int(binary.LittleEndian.Uint16(cfg[9+9+7:])); got != len(rd) {
		t.Fatalf("HID descriptor wDescriptorLength = %d, report descriptor is %d bytes", got, len(rd))
	}

	st, s := b.HandleControl(ctx, ctl(0x80, 0x06, 0x0302, 0x0409, 255), 255, nil)
	if st != 0 || len(s) < 2 || int(s[0]) != len(s) {
		t.Fatalf("product string: st=%d % X", st, s)
	}
	if st, _ := b.HandleControl(ctx, ctl(0x80, 0x06, 0x0309, 0x0409, 255), 255, nil); st == 0 {
		t.Fatal("unknown string index should fail")
	}
}

func TestHIDGenInterruptInOrderAndDropOldest(t *testing.T) {
	b, _ := newFakeBackend(t)
	ctx := context.Background()
	for i := 0; i < hidGenQueue+5; i++ {
		b.push(0, []byte{byte(i), 0, 0})
	}
	// The oldest 5 were dropped; the rest come out in order.
	for want := 5; want < hidGenQueue+5; want++ {
		st, rep := b.HandleBulk(ctx, 0x81, true, 64, nil)
		if st != 0 || rep[0] != byte(want) {
			t.Fatalf("got st=%d report %v, want first byte %d", st, rep, want)
		}
	}
	// Truncated to the URB length.
	b.push(0, []byte{1, 2, 3})
	if _, rep := b.HandleBulk(ctx, 0x81, true, 2, nil); len(rep) != 2 {
		t.Fatalf("report not clipped to URB length: %v", rep)
	}
}

func TestHIDGenInterruptInUnlink(t *testing.T) {
	b, _ := newFakeBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int32, 1)
	go func() { st, _ := b.HandleBulk(ctx, 0x81, true, 64, nil); done <- st }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case st := <-done:
		if st == 0 {
			t.Fatal("cancelled URB must not complete successfully")
		}
	case <-time.After(time.Second):
		t.Fatal("waiting IN URB did not return after unlink")
	}
	// A wrong endpoint or direction is refused.
	if st, _ := b.HandleBulk(context.Background(), 0x05, true, 8, nil); st == 0 {
		t.Fatal("unknown endpoint accepted")
	}
	if st, _ := b.HandleBulk(context.Background(), 0x01, false, 8, []byte{1}); st == 0 {
		t.Fatal("OUT to an IN-only interface accepted")
	}
}

func TestHIDGenReportRoundTrip(t *testing.T) {
	b, port := newFakeBackend(t)
	ctx := context.Background()

	// SET_REPORT(feature, id 2) reaches the device with its payload (this used to be dropped).
	st, _ := b.HandleControl(ctx, ctl(0x21, 0x09, 0x0302, 0, 3), 3, []byte{0x11, 0x22, 0x33})
	if st != 0 || len(port.sets) != 1 || !bytes.Equal(port.sets[0], []byte{2, 0x11, 0x22, 0x33}) || port.lastType != 3 {
		t.Fatalf("SET_REPORT not forwarded: st=%d sets=%v type=%d", st, port.sets, port.lastType)
	}
	port.setErr = errors.New("device refused")
	if st, _ := b.HandleControl(ctx, ctl(0x21, 0x09, 0x0302, 0, 1), 1, []byte{1}); st == 0 {
		t.Fatal("a failed SET_REPORT must be reported to the importer")
	}
	// Input reports cannot be set.
	if st, _ := b.HandleControl(ctx, ctl(0x21, 0x09, 0x0100, 0, 1), 1, []byte{1}); st == 0 {
		t.Fatal("SET_REPORT of an input report accepted")
	}

	st, got := b.HandleControl(ctx, ctl(0xA1, 0x01, 0x0302, 0, 8), 8, nil)
	if st != 0 || !bytes.Equal(got, []byte{2, 0xAA, 0xBB}) {
		t.Fatalf("GET_REPORT(feature): st=%d % X", st, got)
	}
	if st, _ := b.HandleControl(ctx, ctl(0xA1, 0x01, 0x0309, 0, 8), 8, nil); st == 0 {
		t.Fatal("GET_REPORT for a missing report should fail")
	}

	for _, req := range []byte{0x0A, 0x0B} {
		if st, _ := b.HandleControl(ctx, ctl(0x21, req, 0, 0, 0), 0, nil); st != 0 {
			t.Fatalf("class request %#x not acknowledged", req)
		}
	}
	if st, p := b.HandleControl(ctx, ctl(0xA1, 0x03, 0, 0, 1), 1, nil); st != 0 || len(p) != 1 || p[0] != 1 {
		t.Fatalf("GET_PROTOCOL: st=%d %v", st, p)
	}
	if st, _ := b.HandleControl(ctx, ctl(0x00, 0x09, 1, 0, 0), 0, nil); st != 0 {
		t.Fatal("SET_CONFIGURATION not acknowledged")
	}
}

func TestHIDGenMultiInterfaceEndpoints(t *testing.T) {
	rd, _ := reconstructReportDescriptor(syntheticMousePP())
	p := &fakeHIDPort{ids: [ppReportTypes]map[uint8]bool{{0: true}, {}, {}}}
	b := newHIDGenBackend(hidGenInfo{VID: 1, PID: 2, Interfaces: []*hidGenIface{
		{Number: 0, ReportDesc: rd, MaxInput: 3, Ports: []hidGenPort{p}},
		{Number: 2, ReportDesc: rd, MaxInput: 3, Ports: []hidGenPort{p}},
	}})
	if b.ifaces[0].ep != 0x81 || b.ifaces[1].ep != 0x82 {
		t.Fatalf("endpoints = %#x %#x", b.ifaces[0].ep, b.ifaces[1].ep)
	}
	b.push(1, []byte{9, 9, 9})
	if st, rep := b.HandleBulk(context.Background(), 0x82, true, 8, nil); st != 0 || rep[0] != 9 {
		t.Fatalf("interface 2 report lost: st=%d %v", st, rep)
	}
	// wIndex selects the interface by its real number, not its position.
	if st, d := b.HandleControl(context.Background(), ctl(0x81, 0x06, 0x2200, 2, 512), 512, nil); st != 0 || len(d) != len(rd) {
		t.Fatalf("report descriptor for interface 2: st=%d len=%d", st, len(d))
	}
	if st, _ := b.HandleControl(context.Background(), ctl(0x81, 0x06, 0x2200, 1, 512), 512, nil); st == 0 {
		t.Fatal("nonexistent interface 1 served a descriptor")
	}
}

func TestHIDGenConfigDescriptorLayout(t *testing.T) {
	rd, _ := reconstructReportDescriptor(syntheticMousePP())
	cfg := hidGenConfigDesc([]*hidGenIface{{Number: 0, ReportDesc: rd, MaxInput: 200, ep: 0x81}})
	if len(cfg) != 9+9+9+7 || cfg[4] != 1 {
		t.Fatalf("unexpected config layout: % X", cfg)
	}
	// An endpoint can't be larger than a full-speed packet.
	if mps := binary.LittleEndian.Uint16(cfg[9+9+9+4:]); mps != hidGenMaxPacket {
		t.Fatalf("wMaxPacketSize = %d, want %d", mps, hidGenMaxPacket)
	}
}

// timeoutCtx is a context that ends after d (helper shared with the live test).
func timeoutCtx(d time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	_ = cancel // released when the context times out
	return ctx
}

func TestHIDGenUndeclaredReports(t *testing.T) {
	b, port := newFakeBackend(t)
	ctx := context.Background()
	// A vendor driver's hidden init write is acknowledged but never forwarded.
	if st, _ := b.HandleControl(ctx, ctl(0x21, 0x09, 0x0383, 0, 3), 3, []byte{0x83, 0x02, 0x00}); st != 0 {
		t.Fatalf("undeclared SET_REPORT should be acknowledged, st=%d", st)
	}
	if len(port.sets) != 0 {
		t.Fatalf("undeclared SET_REPORT reached the device: %v", port.sets)
	}
	// Reading an undeclared report is honestly refused.
	if st, _ := b.HandleControl(ctx, ctl(0xA1, 0x01, 0x030C, 0, 9), 9, nil); st == 0 {
		t.Fatal("undeclared GET_REPORT should fail")
	}
}
