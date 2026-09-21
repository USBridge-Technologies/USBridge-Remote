package usbpass

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"
)

func TestWacomModelCTL4100(t *testing.T) {
	m := wacomModelFor(0x056a, 0x0374)
	if m == nil {
		t.Fatal("no CTL-4100 model")
	}
	// The HID descriptor in the config descriptor announces the report descriptor's length.
	if n := int(m.ConfigDesc[25]) | int(m.ConfigDesc[26])<<8; n != len(m.Ifaces[0].ReportDesc) {
		t.Errorf("config says %d report descriptor bytes, model has %d", n, len(m.Ifaces[0].ReportDesc))
	}
	if int(m.ConfigDesc[2])|int(m.ConfigDesc[3])<<8 != len(m.ConfigDesc) {
		t.Error("config descriptor total length is wrong")
	}

	b := newWacomModelBackend(m)
	get := func(id uint8, n int) (int32, []byte) {
		return b.HandleControl(context.Background(), [8]byte{0xA1, 0x01, id, 0x03, 0, 0, byte(n), 0}, n, nil)
	}
	// The reports Wacom's Windows driver reads during initialisation.
	for id, want := range map[uint8]string{
		0x14: "1431454830305232303137373232", // the serial number
		0x07: "07010011010000000000000000000000",
		0x0C: "0c0000000000000000",
	} {
		st, data := get(id, len(want)/2)
		if st != 0 || !bytes.Equal(data, mustHex(t, want)) {
			t.Errorf("GET_REPORT %#02x: st=%d data=%x, want %s", id, st, data, want)
		}
	}
	// A report the real tablet refuses is refused here too.
	if st, _ := get(0xD0, 9); st == 0 {
		t.Error("GET_REPORT 0xD0 should STALL like the real tablet")
	}
	// A mode written by the driver is read back.
	if st, _ := b.HandleControl(context.Background(), [8]byte{0x21, 0x09, 0x02, 0x03, 0, 0, 2, 0}, 2, []byte{0x02, 0x01}); st != 0 {
		t.Fatalf("SET_REPORT mode: st=%d", st)
	}
	if _, data := get(0x02, 2); !bytes.Equal(data, []byte{0x02, 0x01}) {
		t.Errorf("mode read back as %x", data)
	}
}

func TestWacomWireReport(t *testing.T) {
	m := wacomModelFor(0x056a, 0x0374)
	pen := make([]byte, 27)
	pen[0], pen[1] = 0x10, 0x61
	if got := m.wireReport(pen); !bytes.Equal(got, pen) {
		t.Errorf("a wire report must pass unchanged, got %x", got)
	}
	wrapped := make([]byte, 193) // Wacom Windows driver: 0xDC + report padded to 192
	wrapped[0] = 0xDC
	copy(wrapped[1:], pen)
	if got := m.wireReport(wrapped); !bytes.Equal(got, pen) {
		t.Errorf("wrapped report: got %x", got)
	}
	if m.wireReport([]byte{0x77, 1, 2}) != nil || m.wireReport([]byte{0x10, 1}) != nil || m.wireReport(nil) != nil {
		t.Error("unknown or short reports must be dropped")
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Every tablet in the database must produce a self-consistent USB device: the
// configuration announces each interface's report descriptor with its real length,
// the descriptor is served whole, and input reports reach the right interface.
func TestWacomDBModelsAreConsistent(t *testing.T) {
	pids := wacomDBPIDs()
	if len(pids) < 50 {
		t.Fatalf("database has only %d tablets", len(pids))
	}
	multi := 0
	for _, pid := range pids {
		m := wacomModelFromDB(wacomVendorID, pid, 0x0100, "Wacom Co.,Ltd.", "Tablet", "SN", nil, nil)
		if m == nil {
			t.Errorf("%04x: no model", pid)
			continue
		}
		if len(m.Ifaces) > 1 {
			multi++
		}
		b := newWacomModelBackend(m)
		cfg := b.configDesc
		if int(cfg[2])|int(cfg[3])<<8 != len(cfg) || int(cfg[4]) != len(m.Ifaces) {
			t.Errorf("%04x: config descriptor length/interface count wrong", pid)
		}
		seen := 0
		cur := -1
		for i := 0; i+2 <= len(cfg) && cfg[i] != 0; i += int(cfg[i]) {
			switch cfg[i+1] {
			case 0x04:
				cur = int(cfg[i+2])
			case 0x21:
				for _, f := range m.Ifaces {
					if int(f.Number) == cur {
						seen++
						if n := int(cfg[i+7]) | int(cfg[i+8])<<8; n != len(f.ReportDesc) {
							t.Errorf("%04x if%d: config announces %d bytes, descriptor has %d", pid, cur, n, len(f.ReportDesc))
						}
					}
				}
			}
		}
		if seen != len(m.Ifaces) {
			t.Errorf("%04x: %d of %d interfaces found in the configuration", pid, seen, len(m.Ifaces))
		}
		for _, f := range m.Ifaces {
			st, rd := b.HandleControl(context.Background(), [8]byte{0x81, 0x06, 0x00, 0x22, f.Number, 0, 0xFF, 0x0F}, 4095, nil)
			if st != 0 || !bytes.Equal(rd, f.ReportDesc) {
				t.Errorf("%04x if%d: report descriptor not served whole (st=%d, %d bytes)", pid, f.Number, st, len(rd))
			}
			for id, n := range f.inSizes {
				if id == 0 || n == 0 {
					continue
				}
				rep := append([]byte{id}, make([]byte, n)...)
				if _, got := m.route(rep, int(f.Number)); !bytes.Equal(got, rep) {
					t.Errorf("%04x if%d: input report %#02x not routed", pid, f.Number, id)
				}
			}
		}
	}
	if multi == 0 {
		t.Error("no multi-interface tablet in the database")
	}
}
