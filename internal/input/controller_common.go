package input

import "sync"

type Controller struct {
	mu          sync.Mutex
	buttonState uint8
}

type hidKeySpec struct {
	windowsScan     uint16
	windowsExtended bool
	macKeyCode      uint16
}

var hidKeyTable = map[uint8]hidKeySpec{
	4:   {windowsScan: 0x1E, macKeyCode: 0x00},                        // A
	5:   {windowsScan: 0x30, macKeyCode: 0x0B},                        // B
	6:   {windowsScan: 0x2E, macKeyCode: 0x08},                        // C
	7:   {windowsScan: 0x20, macKeyCode: 0x02},                        // D
	8:   {windowsScan: 0x12, macKeyCode: 0x0E},                        // E
	9:   {windowsScan: 0x21, macKeyCode: 0x03},                        // F
	10:  {windowsScan: 0x22, macKeyCode: 0x05},                        // G
	11:  {windowsScan: 0x23, macKeyCode: 0x04},                        // H
	12:  {windowsScan: 0x17, macKeyCode: 0x22},                        // I
	13:  {windowsScan: 0x24, macKeyCode: 0x26},                        // J
	14:  {windowsScan: 0x25, macKeyCode: 0x28},                        // K
	15:  {windowsScan: 0x26, macKeyCode: 0x25},                        // L
	16:  {windowsScan: 0x32, macKeyCode: 0x2E},                        // M
	17:  {windowsScan: 0x31, macKeyCode: 0x2D},                        // N
	18:  {windowsScan: 0x18, macKeyCode: 0x1F},                        // O
	19:  {windowsScan: 0x19, macKeyCode: 0x23},                        // P
	20:  {windowsScan: 0x10, macKeyCode: 0x0C},                        // Q
	21:  {windowsScan: 0x13, macKeyCode: 0x0F},                        // R
	22:  {windowsScan: 0x1F, macKeyCode: 0x01},                        // S
	23:  {windowsScan: 0x14, macKeyCode: 0x11},                        // T
	24:  {windowsScan: 0x16, macKeyCode: 0x20},                        // U
	25:  {windowsScan: 0x2F, macKeyCode: 0x09},                        // V
	26:  {windowsScan: 0x11, macKeyCode: 0x0D},                        // W
	27:  {windowsScan: 0x2D, macKeyCode: 0x07},                        // X
	28:  {windowsScan: 0x15, macKeyCode: 0x10},                        // Y
	29:  {windowsScan: 0x2C, macKeyCode: 0x06},                        // Z
	30:  {windowsScan: 0x02, macKeyCode: 0x12},                        // 1
	31:  {windowsScan: 0x03, macKeyCode: 0x13},                        // 2
	32:  {windowsScan: 0x04, macKeyCode: 0x14},                        // 3
	33:  {windowsScan: 0x05, macKeyCode: 0x15},                        // 4
	34:  {windowsScan: 0x06, macKeyCode: 0x17},                        // 5
	35:  {windowsScan: 0x07, macKeyCode: 0x16},                        // 6
	36:  {windowsScan: 0x08, macKeyCode: 0x1A},                        // 7
	37:  {windowsScan: 0x09, macKeyCode: 0x1C},                        // 8
	38:  {windowsScan: 0x0A, macKeyCode: 0x19},                        // 9
	39:  {windowsScan: 0x0B, macKeyCode: 0x1D},                        // 0
	40:  {windowsScan: 0x1C, macKeyCode: 0x24},                        // Enter
	41:  {windowsScan: 0x01, macKeyCode: 0x35},                        // Escape
	42:  {windowsScan: 0x0E, macKeyCode: 0x33},                        // Backspace
	43:  {windowsScan: 0x0F, macKeyCode: 0x30},                        // Tab
	44:  {windowsScan: 0x39, macKeyCode: 0x31},                        // Space
	45:  {windowsScan: 0x0C, macKeyCode: 0x1B},                        // -
	46:  {windowsScan: 0x0D, macKeyCode: 0x18},                        // =
	47:  {windowsScan: 0x1A, macKeyCode: 0x21},                        // [
	48:  {windowsScan: 0x1B, macKeyCode: 0x1E},                        // ]
	49:  {windowsScan: 0x2B, macKeyCode: 0x2A},                        // \
	51:  {windowsScan: 0x27, macKeyCode: 0x29},                        // ;
	52:  {windowsScan: 0x28, macKeyCode: 0x27},                        // '
	53:  {windowsScan: 0x29, macKeyCode: 0x32},                        // `
	54:  {windowsScan: 0x33, macKeyCode: 0x2B},                        // ,
	55:  {windowsScan: 0x34, macKeyCode: 0x2F},                        // .
	56:  {windowsScan: 0x35, macKeyCode: 0x2C},                        // /
	57:  {windowsScan: 0x3A, macKeyCode: 0x39},                        // CapsLock
	58:  {windowsScan: 0x3B, macKeyCode: 0x7A},                        // F1
	59:  {windowsScan: 0x3C, macKeyCode: 0x78},                        // F2
	60:  {windowsScan: 0x3D, macKeyCode: 0x63},                        // F3
	61:  {windowsScan: 0x3E, macKeyCode: 0x76},                        // F4
	62:  {windowsScan: 0x3F, macKeyCode: 0x60},                        // F5
	63:  {windowsScan: 0x40, macKeyCode: 0x61},                        // F6
	64:  {windowsScan: 0x41, macKeyCode: 0x62},                        // F7
	65:  {windowsScan: 0x42, macKeyCode: 0x64},                        // F8
	66:  {windowsScan: 0x43, macKeyCode: 0x65},                        // F9
	67:  {windowsScan: 0x44, macKeyCode: 0x6D},                        // F10
	68:  {windowsScan: 0x57, macKeyCode: 0x67},                        // F11
	69:  {windowsScan: 0x58, macKeyCode: 0x6F},                        // F12
	73:  {windowsScan: 0x52, windowsExtended: true, macKeyCode: 0x72}, // Insert
	74:  {windowsScan: 0x47, windowsExtended: true, macKeyCode: 0x73}, // Home
	75:  {windowsScan: 0x49, windowsExtended: true, macKeyCode: 0x74}, // PageUp
	76:  {windowsScan: 0x53, windowsExtended: true, macKeyCode: 0x75}, // Delete
	77:  {windowsScan: 0x4F, windowsExtended: true, macKeyCode: 0x77}, // End
	78:  {windowsScan: 0x51, windowsExtended: true, macKeyCode: 0x79}, // PageDown
	79:  {windowsScan: 0x4D, windowsExtended: true, macKeyCode: 0x7C}, // Right
	80:  {windowsScan: 0x4B, windowsExtended: true, macKeyCode: 0x7B}, // Left
	81:  {windowsScan: 0x50, windowsExtended: true, macKeyCode: 0x7D}, // Down
	82:  {windowsScan: 0x48, windowsExtended: true, macKeyCode: 0x7E}, // Up
	224: {windowsScan: 0x1D, macKeyCode: 0x3B},                        // Left Ctrl
	225: {windowsScan: 0x2A, macKeyCode: 0x38},                        // Left Shift
	226: {windowsScan: 0x38, macKeyCode: 0x3A},                        // Left Alt/Option
	227: {windowsScan: 0x5B, windowsExtended: true, macKeyCode: 0x37}, // Left GUI/Command
	228: {windowsScan: 0x1D, windowsExtended: true, macKeyCode: 0x3E}, // Right Ctrl
	229: {windowsScan: 0x36, macKeyCode: 0x3C},                        // Right Shift
	230: {windowsScan: 0x38, windowsExtended: true, macKeyCode: 0x3D}, // Right Alt/Option
	231: {windowsScan: 0x5C, windowsExtended: true, macKeyCode: 0x36}, // Right GUI/Command
}

func hidSpec(key uint8) (hidKeySpec, bool) {
	spec, ok := hidKeyTable[key]
	return spec, ok
}

func modifierHIDKeys(modifiers uint8) []uint8 {
	out := make([]uint8, 0, 4)
	if modifiers&0x01 != 0 {
		out = append(out, 224)
	}
	if modifiers&0x02 != 0 {
		out = append(out, 225)
	}
	if modifiers&0x04 != 0 {
		out = append(out, 226)
	}
	if modifiers&0x08 != 0 {
		out = append(out, 227)
	}
	return out
}
