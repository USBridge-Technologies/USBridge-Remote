package platform

// Platform-agnostic Wacom "IntuosV2" pen-tablet protocol logic, shared by
// every capture source that can hand this package a raw HID input report --
// today that's pen_capture_darwin.go's IOHIDManager tap, and (via WebHID)
// the browser/wasm client, which gets the exact same raw report bytes from
// Chrome instead of IOKit. None of this file talks to any OS API itself.

// WacomVendorID is Wacom's USB vendor ID -- used to filter which HID devices
// a capture source should even attempt to open (IOHIDManager matching on
// darwin, WebHID's requestDevice filter in the browser).
const WacomVendorID = 0x056A

// PenCaptureState is one decoded pen sample, in raw device units. Coordinate/
// pressure normalization to Moonlight's 0.0..1.0 wire format happens in the
// caller (see DerivePenEvent), via PenRangeFor(pid) -- different models
// sharing this exact byte layout still report different logical-max ranges
// (a Cintiq Pro 32's digitizer is ~9x a CTL-4100's), so the range has to be
// looked up per model rather than assumed fixed.
type PenCaptureState struct {
	X, Y         uint32
	Pressure     uint16
	TiltX, TiltY int8
	Rotation     int16
	InRange      bool
	TipSwitch    bool
	Button1      bool
	Button2      bool
	Eraser       bool
}

// PenMaxX/PenMaxY/PenMaxPressure are the CTL-4100 "Intuos S" ranges -- the
// one model in wacomIntuosV2Ranges actually live-verified against real
// hardware -- and PenRangeFor's fallback for any PID not in that table.
const (
	PenMaxX        = 15200
	PenMaxY        = 9500
	PenMaxPressure = 4095
)

// PenRange is one model's digitizer/pressure normalization range.
type PenRange struct {
	MaxX, MaxY  uint32
	MaxPressure uint16
}

// wacomIntuosV2Ranges maps a Wacom product ID to its real digitizer/pressure
// ranges for every model this project confirmed -- via
// `gh api search/code -q '"IntuosV2ReportParser" repo:OpenTabletDriver/OpenTabletDriver path:.../Configurations/Wacom'`,
// not by guessing from model-name similarity -- OpenTabletDriver itself
// tags with the exact "IntuosV2.IntuosV2ReportParser" report-parser class,
// i.e. every model sharing decodePenReport's byte layout (report ID 0x10,
// 24-bit X/Y, uint16 LE pressure at offset 8, ...). Values are copied
// straight from each model's Configurations/Wacom/<PID>.json
// Digitizer/Pen specification. PID 0x0374 (CTL-4100) is the only entry
// live-verified against real hardware; the rest are trusted from
// OpenTabletDriver's own parser-class tag on the (reasonable) assumption
// that tag is accurate -- if it's ever wrong for one model, only that
// model's normalization is off, not CTL-4100's.
//
// Name-similarity is not a safe way to guess this table: CTH-680 and
// CTL-470 sound like the same generation as CTL-4100 but actually use the
// completely different (10-byte) "Intuos" parser class, not this one, and
// would need a real second decoder, not just a range entry.
var wacomIntuosV2Ranges = map[uint16]PenRange{
	0x0374: {15200, 9500, 4095},   // CTL-4100 (Intuos S / Bamboo) -- live-verified
	0x0375: {21600, 13500, 4095},  // CTL-6100 (Intuos M / Bamboo)
	0x0376: {15200, 9500, 4095},   // CTL-4100WL
	0x0377: {15200, 9500, 4095},   // CTL-4100WL (Bluetooth report variant)
	0x03C5: {15200, 9500, 4095},   // CTL-4100WL (Bluetooth report variant)
	0x0378: {21600, 13500, 4095},  // CTL-6100WL
	0x03C7: {21600, 13500, 4095},  // CTL-6100WL (Bluetooth report variant)
	0x0392: {31920, 19950, 8191},  // PTH-460 (Intuos Pro Small)
	0x03DC: {31920, 19950, 8191},  // PTH-460 (alt PID)
	0x0357: {44800, 29600, 8191},  // PTH-660 (Intuos Pro Medium)
	0x0358: {62200, 43200, 8191},  // PTH-860 (Intuos Pro Large)
	0x03CE: {25632, 14418, 4095},  // DTC-121
	0x03A6: {29434, 16556, 4095},  // DTC-133
	0x03D0: {96012, 54356, 8191},  // DTH-227 (Cintiq Pro 22)
	0x03C0: {120032, 67868, 8191}, // DTH-271 (Cintiq Pro 27)
	0x034F: {59552, 33848, 8191},  // DTH-1320 (Cintiq Pro 13)
	0x0352: {140384, 79316, 8191}, // DTH-3220 (Cintiq Pro 32)
	0x0390: {69632, 39518, 8191},  // DTK-1660 (Cintiq 16)
	0x03AE: {69632, 39518, 8191},  // DTK-1660 (alt PID)
}

// PenRangeFor returns the digitizer/pressure normalization range for a
// Wacom product ID, falling back to the CTL-4100 defaults for any PID not
// in wacomIntuosV2Ranges. A byte-compatible device this project hasn't
// catalogued yet is far more likely a close relative of the small consumer
// CTL-4100 than the ~9x-larger Cintiq Pro 32, so that stays the safer
// default over guessing a large-format range for an unknown device.
func PenRangeFor(pid uint16) (maxX, maxY uint32, maxPressure uint16) {
	if r, ok := wacomIntuosV2Ranges[pid]; ok {
		return r.MaxX, r.MaxY, r.MaxPressure
	}
	return PenMaxX, PenMaxY, PenMaxPressure
}

// decodePenReport parses one raw Wacom "IntuosV2" family HID input report
// (report ID 0x10, empirically confirmed byte-for-byte against a real
// CTL-4100 "Intuos S" against OpenTabletDriver's
// Configurations/Parsers/Wacom/IntuosV2/IntuosV2Report.cs, the reference
// open-source parser for this tablet generation -- Wacom's own HID report
// descriptor for this family uses a fully vendor-defined usage page, so
// there is no standards-based way to decode this short of matching a known
// driver's byte layout):
//
//	byte[0]      report ID (0x10)
//	byte[1]      bit0 tip switch, bit1/bit2 barrel buttons, bit4 eraser, bit6
//	             in-range. bit5 is also set while the tip is down (live
//	             capture against real hardware showed 0x40 while hovering
//	             close but not touching, and 0x61 -- bit0|bit5|bit6 -- while
//	             touching), so it's excluded from InRange: treating it as
//	             "proximity" made a real touch-then-lift sequence emit a
//	             spurious "left the tablet" cancellation the instant the tip
//	             came up, because bit5 dropped out exactly at that moment
//	             while the pen was still genuinely hovering (bit6 stayed
//	             set). OpenTabletDriver's IntuosV2Report.cs names this
//	             bit "NearProximity" and IntuosV2.json doesn't map bit6 to
//	             anything -- it's possible OTD only cares about bit5 because
//	             its own hover-vs-touch handling lives elsewhere, or that
//	             bit numbering differs slightly on this firmware; bit6 is
//	             what actually matches the "in range for the whole hover,
//	             not just while touching" behavior this decoder needs.
//	byte[2..5)   X, 24-bit little-endian (byte4 is the high byte; always 0 in
//	             range, since MaxX=15200 fits in 16 bits)
//	byte[5..8)   Y, same 24-bit layout
//	byte[8..10)  pressure, uint16 little-endian, 0..4095
//	byte[10]     tilt X, signed byte (degrees)
//	byte[11]     tilt Y, signed byte (degrees)
//	byte[12..14) rotation, int16 little-endian (Art Pen/airbrush only; 0 on a
//	             standard pen)
//	byte[16]     hover distance (unused here)
func decodePenReport(buf []byte) (PenCaptureState, bool) {
	if len(buf) < 14 || buf[0] != 0x10 {
		return PenCaptureState{}, false
	}
	penByte := buf[1]
	x := uint32(buf[2]) | uint32(buf[3])<<8 | uint32(buf[4])<<16
	y := uint32(buf[5]) | uint32(buf[6])<<8 | uint32(buf[7])<<16
	pressure := uint16(buf[8]) | uint16(buf[9])<<8
	return PenCaptureState{
		X:         x,
		Y:         y,
		Pressure:  pressure,
		TiltX:     int8(buf[10]),
		TiltY:     int8(buf[11]),
		Rotation:  int16(uint16(buf[12]) | uint16(buf[13])<<8),
		InRange:   penByte&0x40 != 0,
		TipSwitch: penByte&0x01 != 0,
		Button1:   penByte&0x02 != 0,
		Button2:   penByte&0x04 != 0,
		Eraser:    penByte&0x10 != 0,
	}, true
}
