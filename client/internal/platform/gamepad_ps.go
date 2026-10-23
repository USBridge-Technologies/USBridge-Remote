package platform

import (
	"hash/crc32"
	"time"
)

// PlayStation pads read and driven through their own HID reports: which
// family a pad is, what its input report carries besides buttons and sticks
// (gamepad_touchpad.go's DS4Report), and the output report that sets its
// motors, light and -- on a DualSense -- adaptive triggers.

// PadFamily is the report layout a pad speaks.
type PadFamily int

const (
	PadOther   PadFamily = iota
	PadDS4               // DualShock 4 and its clones (Razer Raiju)
	PadDS5               // DualSense
	PadDS5Edge           // DualSense Edge: a DualSense with two back paddles and two Fn buttons
)

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

// PadFamilyOf is the family of a USB id.
func PadFamilyOf(vid, pid uint16) PadFamily {
	if isDS4Family(vid, pid) {
		return PadDS4
	}
	if vid == 0x054C {
		switch pid {
		case 0x0CE6:
			return PadDS5
		case 0x0DF2:
			return PadDS5Edge
		}
	}
	return PadOther
}

// IsDualSense reports whether the family is a DualSense of either kind.
func (f PadFamily) IsDualSense() bool { return f == PadDS5 || f == PadDS5Edge }

const (
	ds5ReportUSB = 0x01
	ds5ReportBT  = 0x31
	ds4ReportBT  = 0x11
	// The DualSense's touchpad, in pad units.
	DS5TouchpadWidth  = 1920
	DS5TouchpadHeight = 1080
)

// Moonlight's extended buttons (buttonFlags2 << 16, Limelight.h).
const (
	ExtPaddle1  uint32 = 0x010000
	ExtPaddle2  uint32 = 0x020000
	ExtPaddle3  uint32 = 0x040000
	ExtPaddle4  uint32 = 0x080000
	ExtTouchpad uint32 = 0x100000
	ExtMisc     uint32 = 0x200000
)

// ParsePadReport reads a PlayStation pad's input report of either family,
// over USB or Bluetooth (the full report a driver switched the pad to).
func ParsePadReport(rep []byte, family PadFamily) (DS4Report, bool) {
	switch family {
	case PadDS4:
		if len(rep) > 2 && rep[0] == ds4ReportBT {
			// Bluetooth: two bytes more before the USB report's own layout.
			b := make([]byte, len(rep)-2)
			b[0] = ds4ReportID
			copy(b[1:], rep[3:])
			return ParseDS4Report(b)
		}
		return ParseDS4Report(rep)
	case PadDS5, PadDS5Edge:
		return parseDS5Report(rep, family == PadDS5Edge)
	}
	return DS4Report{}, false
}

// parseDS5Report reads a DualSense input report (hid-playstation's
// dualsense_input_report): byte 10 the PS (bit 0), touchpad click (1), mute
// (2) and on an Edge its Fn and paddle buttons (4-7), 16-21 the gyro, 22-27
// the accelerometer, 33-40 the two contacts, 53 the battery. Over Bluetooth
// all of it is one byte later.
func parseDS5Report(rep []byte, edge bool) (DS4Report, bool) {
	off := 0
	switch {
	case len(rep) >= 64 && rep[0] == ds5ReportUSB:
	case len(rep) >= 65 && rep[0] == ds5ReportBT:
		off = 1
	default:
		return DS4Report{}, false
	}
	b := func(i int) byte { return rep[i+off] }
	r := DS4Report{Family: PadDS5, Click: b(10)&0x02 != 0, Battery: b(53)}
	if b(10)&0x04 != 0 {
		r.Extra |= ExtMisc
	}
	if edge {
		r.Family = PadDS5Edge
		for bit, flag := range map[byte]uint32{0x80: ExtPaddle1, 0x40: ExtPaddle2, 0x20: ExtPaddle3, 0x10: ExtPaddle4} {
			if b(10)&bit != 0 {
				r.Extra |= flag
			}
		}
	}
	for i := 0; i < 3; i++ {
		r.Gyro[i] = int16(uint16(b(16+2*i)) | uint16(b(17+2*i))<<8)
		r.Accel[i] = int16(uint16(b(22+2*i)) | uint16(b(23+2*i))<<8)
	}
	r.Fingers[0] = ds4Finger(rep[33+off : 37+off])
	r.Fingers[1] = ds4Finger(rep[37+off : 41+off])
	return r, true
}

// psOutState is what the host last asked of a PlayStation pad. Every output
// report carries all of it, as the pad applies whole reports.
type psOutState struct {
	low, high uint16
	rgb       *[3]uint8
	// Adaptive-trigger effects (mode and ten parameters) of the right and
	// left trigger, and which of them still has to be written.
	right, left [11]byte
	trigDirty   uint8 // dsTriggerRight | dsTriggerLeft
	// When the host last sent haptics: the voice coils play them, so the
	// motors' rumble emulation stays off meanwhile.
	hapticsAt time.Time
	btSeq     uint8
}

const (
	dsTriggerRight = 0x04
	dsTriggerLeft  = 0x08
	// The DualSense's USB output report as the descriptor declares it, and
	// its Bluetooth one.
	ds5OutputUSBLen = 48
	ds5OutputBTLen  = 78
)

// setTriggers records an IDX_DS_ADAPTIVE_TRIGGERS update.
func (st *psOutState) setTriggers(flags, typeLeft, typeRight uint8, left, right [10]byte) {
	if flags&dsTriggerRight != 0 {
		st.right[0] = typeRight
		copy(st.right[1:], right[:])
		st.trigDirty |= dsTriggerRight
	}
	if flags&dsTriggerLeft != 0 {
		st.left[0] = typeLeft
		copy(st.left[1:], left[:])
		st.trigDirty |= dsTriggerLeft
	}
}

// ds4Output is the DualShock 4 USB output report 0x05 for st.
func ds4Output(st *psOutState) []byte {
	rep := make([]byte, ds4OutputLen)
	rep[0], rep[1] = 0x05, 0x01
	rep[4] = byte(st.high >> 8) // right / fast motor
	rep[5] = byte(st.low >> 8)  // left / heavy motor
	if st.rgb != nil {
		rep[1] |= 0x02
		rep[6], rep[7], rep[8] = st.rgb[0], st.rgb[1], st.rgb[2]
	}
	return rep
}

const ds4OutputLen = 32

// ds5Output is the DualSense output report for st: 0x02 over USB, 0x31
// with its CRC over Bluetooth (hid-playstation's dualsense_output_report_*).
// The common part: valid flags 0/1, motors, the two trigger effects, the
// lightbar. The trigger effects written are marked clean.
func ds5Output(st *psOutState, bluetooth bool) []byte {
	var rep []byte
	var c []byte // the common part
	if bluetooth {
		rep = make([]byte, ds5OutputBTLen)
		rep[0] = ds5ReportBT
		rep[1] = st.btSeq << 4
		rep[2] = 0x10 // tag: output report
		st.btSeq = (st.btSeq + 1) & 0x0F
		c = rep[3:]
	} else {
		rep = make([]byte, ds5OutputUSBLen)
		rep[0] = 0x02
		c = rep[1:]
	}
	// While the voice coils play the host's haptics, the motors' emulation
	// stays off: valid flag 1 (haptics select) would switch the coils over to it.
	if time.Since(st.hapticsAt) > 200*time.Millisecond {
		c[0] |= 0x01 | 0x02 // compatible vibration, haptics select
		c[2] = byte(st.high >> 8)
		c[3] = byte(st.low >> 8)
	}
	if st.trigDirty&dsTriggerRight != 0 {
		c[0] |= dsTriggerRight
		copy(c[10:21], st.right[:])
	}
	if st.trigDirty&dsTriggerLeft != 0 {
		c[0] |= dsTriggerLeft
		copy(c[21:32], st.left[:])
	}
	st.trigDirty = 0
	if st.rgb != nil {
		c[1] |= 0x04 // lightbar
		c[44], c[45], c[46] = st.rgb[0], st.rgb[1], st.rgb[2]
	}
	if bluetooth {
		crc := crc32.Update(crc32.ChecksumIEEE([]byte{0xA2}), crc32.IEEETable, rep[:ds5OutputBTLen-4])
		rep[74], rep[75], rep[76], rep[77] = byte(crc), byte(crc>>8), byte(crc>>16), byte(crc>>24)
	}
	return rep
}

// psOutputReport is the output report of a pad of family f for st.
func psOutputReport(f PadFamily, st *psOutState, bluetooth bool) []byte {
	if f.IsDualSense() {
		return ds5Output(st, bluetooth)
	}
	return ds4Output(st)
}
