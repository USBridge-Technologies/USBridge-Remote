package platform

import (
	"hash/crc32"
	"testing"
)

func TestParseDualSenseUSBAndBluetooth(t *testing.T) {
	usb := make([]byte, 64)
	usb[0] = 0x01
	usb[10] = 0x02 | 0x04 | 0x80                 // touchpad click, mute, right paddle (Edge)
	usb[16], usb[17] = 0x10, 0x00                // gyro x = 16
	usb[33] = 0x05                               // finger 0 touching, id 5
	usb[34], usb[35], usb[36] = 0x80, 0x07, 0x40 // x = 0x780, y = 0x400
	usb[37] = 0x80                               // finger 1 up
	usb[53] = 0x1A                               // charging, level 10
	r, ok := ParsePadReport(usb, PadDS5Edge)
	if !ok || !r.Click || r.Extra != ExtMisc|ExtPaddle1 || r.Gyro[0] != 16 {
		t.Fatalf("usb: %+v ok=%v", r, ok)
	}
	if f := r.Fingers[0]; !f.Active || f.ID != 5 || f.X != 0x780 || f.Y != 0x400 || r.Fingers[1].Active {
		t.Fatalf("fingers: %+v", r.Fingers)
	}
	if st, pct, ok := r.BatteryState(); !ok || st != 0x03 || pct != 100 {
		t.Fatalf("battery: %d %d %v", st, pct, ok)
	}
	// A plain DualSense has no paddles.
	if r, _ := ParsePadReport(usb, PadDS5); r.Extra != ExtMisc {
		t.Fatalf("plain DualSense extra = %#x", r.Extra)
	}
	bt := append([]byte{0x31, 0x00}, usb[1:]...)
	bt = append(bt, make([]byte, 14)...)
	r2, ok := ParsePadReport(bt, PadDS5Edge)
	if !ok || r2.Extra != r.Extra || r2.Fingers != r.Fingers {
		t.Fatalf("bluetooth differs: %+v", r2)
	}
	if w, h := r2.TouchpadSize(); w != 1920 || h != 1080 {
		t.Fatalf("touchpad %dx%d", w, h)
	}
	// The DualSense's short Bluetooth report is not one.
	if _, ok := ParsePadReport([]byte{0x01, 1, 2, 3, 4, 5, 6, 7, 8, 9}, PadDS5); ok {
		t.Fatal("short report parsed")
	}
}

func TestDualSenseOutputReport(t *testing.T) {
	st := &psOutState{low: 0xFF00, high: 0x8000, rgb: &[3]uint8{1, 2, 3}}
	st.setTriggers(dsTriggerRight, 0, 0x26, [10]byte{}, [10]byte{9, 8})
	rep := ds5Output(st, false)
	if len(rep) != 48 || rep[0] != 0x02 {
		t.Fatalf("id/len: %d % x", len(rep), rep[:4])
	}
	if rep[1] != 0x01|0x02|dsTriggerRight || rep[2] != 0x04 || rep[3] != 0x80 || rep[4] != 0xFF {
		t.Fatalf("flags/motors: % x", rep[:5])
	}
	if rep[11] != 0x26 || rep[12] != 9 || rep[13] != 8 || rep[45] != 1 || rep[47] != 3 {
		t.Fatalf("trigger/light: % x", rep)
	}
	// Written once: the next report leaves the trigger alone.
	if again := ds5Output(st, false); again[1]&dsTriggerRight != 0 {
		t.Fatal("trigger effect resent")
	}
	bt := ds5Output(st, true)
	if len(bt) != 78 || bt[0] != 0x31 || bt[2] != 0x10 || bt[4] != 0x04 {
		t.Fatalf("bluetooth: % x", bt[:8])
	}
	crc := crc32.Update(crc32.ChecksumIEEE([]byte{0xA2}), crc32.IEEETable, bt[:74])
	if got := uint32(bt[74]) | uint32(bt[75])<<8 | uint32(bt[76])<<16 | uint32(bt[77])<<24; got != crc {
		t.Fatalf("crc %08x want %08x", got, crc)
	}
}

func TestPadFamilies(t *testing.T) {
	for _, c := range []struct {
		vid, pid uint16
		want     PadFamily
	}{
		{0x054C, 0x09CC, PadDS4}, {0x1532, 0x1007, PadDS4}, {0x054C, 0x0CE6, PadDS5},
		{0x054C, 0x0DF2, PadDS5Edge}, {0x045E, 0x028E, PadOther},
	} {
		if got := PadFamilyOf(c.vid, c.pid); got != c.want {
			t.Errorf("%04x:%04x = %v, want %v", c.vid, c.pid, got, c.want)
		}
	}
}
