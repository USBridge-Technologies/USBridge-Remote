//go:build windows

package input

type hidKeySpec struct {
	windowsScan     uint16
	windowsExtended bool
}

var hidKeyTable = map[uint8]hidKeySpec{
	4:   {windowsScan: 0x1E},                        // A
	5:   {windowsScan: 0x30},                        // B
	6:   {windowsScan: 0x2E},                        // C
	7:   {windowsScan: 0x20},                        // D
	8:   {windowsScan: 0x12},                        // E
	9:   {windowsScan: 0x21},                        // F
	10:  {windowsScan: 0x22},                        // G
	11:  {windowsScan: 0x23},                        // H
	12:  {windowsScan: 0x17},                        // I
	13:  {windowsScan: 0x24},                        // J
	14:  {windowsScan: 0x25},                        // K
	15:  {windowsScan: 0x26},                        // L
	16:  {windowsScan: 0x32},                        // M
	17:  {windowsScan: 0x31},                        // N
	18:  {windowsScan: 0x18},                        // O
	19:  {windowsScan: 0x19},                        // P
	20:  {windowsScan: 0x10},                        // Q
	21:  {windowsScan: 0x13},                        // R
	22:  {windowsScan: 0x1F},                        // S
	23:  {windowsScan: 0x14},                        // T
	24:  {windowsScan: 0x16},                        // U
	25:  {windowsScan: 0x2F},                        // V
	26:  {windowsScan: 0x11},                        // W
	27:  {windowsScan: 0x2D},                        // X
	28:  {windowsScan: 0x15},                        // Y
	29:  {windowsScan: 0x2C},                        // Z
	30:  {windowsScan: 0x02},                        // 1
	31:  {windowsScan: 0x03},                        // 2
	32:  {windowsScan: 0x04},                        // 3
	33:  {windowsScan: 0x05},                        // 4
	34:  {windowsScan: 0x06},                        // 5
	35:  {windowsScan: 0x07},                        // 6
	36:  {windowsScan: 0x08},                        // 7
	37:  {windowsScan: 0x09},                        // 8
	38:  {windowsScan: 0x0A},                        // 9
	39:  {windowsScan: 0x0B},                        // 0
	40:  {windowsScan: 0x1C},                        // Enter
	41:  {windowsScan: 0x01},                        // Escape
	42:  {windowsScan: 0x0E},                        // Backspace
	43:  {windowsScan: 0x0F},                        // Tab
	44:  {windowsScan: 0x39},                        // Space
	45:  {windowsScan: 0x0C},                        // -
	46:  {windowsScan: 0x0D},                        // =
	47:  {windowsScan: 0x1A},                        // [
	48:  {windowsScan: 0x1B},                        // ]
	49:  {windowsScan: 0x2B},                        // \
	51:  {windowsScan: 0x27},                        // ;
	52:  {windowsScan: 0x28},                        // '
	53:  {windowsScan: 0x29},                        // `
	54:  {windowsScan: 0x33},                        // ,
	55:  {windowsScan: 0x34},                        // .
	56:  {windowsScan: 0x35},                        // /
	57:  {windowsScan: 0x3A},                        // CapsLock
	58:  {windowsScan: 0x3B},                        // F1
	59:  {windowsScan: 0x3C},                        // F2
	60:  {windowsScan: 0x3D},                        // F3
	61:  {windowsScan: 0x3E},                        // F4
	62:  {windowsScan: 0x3F},                        // F5
	63:  {windowsScan: 0x40},                        // F6
	64:  {windowsScan: 0x41},                        // F7
	65:  {windowsScan: 0x42},                        // F8
	66:  {windowsScan: 0x43},                        // F9
	67:  {windowsScan: 0x44},                        // F10
	68:  {windowsScan: 0x57},                        // F11
	69:  {windowsScan: 0x58},                        // F12
	73:  {windowsScan: 0x52, windowsExtended: true}, // Insert
	74:  {windowsScan: 0x47, windowsExtended: true}, // Home
	75:  {windowsScan: 0x49, windowsExtended: true}, // PageUp
	76:  {windowsScan: 0x53, windowsExtended: true}, // Delete
	77:  {windowsScan: 0x4F, windowsExtended: true}, // End
	78:  {windowsScan: 0x51, windowsExtended: true}, // PageDown
	79:  {windowsScan: 0x4D, windowsExtended: true}, // Right
	80:  {windowsScan: 0x4B, windowsExtended: true}, // Left
	81:  {windowsScan: 0x50, windowsExtended: true}, // Down
	82:  {windowsScan: 0x48, windowsExtended: true}, // Up
	224: {windowsScan: 0x1D},                        // Left Ctrl
	225: {windowsScan: 0x2A},                        // Left Shift
	226: {windowsScan: 0x38},                        // Left Alt/Option
	227: {windowsScan: 0x5B, windowsExtended: true}, // Left GUI/Command
	228: {windowsScan: 0x1D, windowsExtended: true}, // Right Ctrl
	229: {windowsScan: 0x36},                        // Right Shift
	230: {windowsScan: 0x38, windowsExtended: true}, // Right Alt/Option
	231: {windowsScan: 0x5C, windowsExtended: true}, // Right GUI/Command
}

func hidSpec(key uint8) (hidKeySpec, bool) {
	spec, ok := hidKeyTable[key]
	return spec, ok
}
