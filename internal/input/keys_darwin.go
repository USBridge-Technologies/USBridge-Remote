//go:build darwin

package input

type hidKeySpec struct {
	macKeyCode uint16
}

var hidKeyTable = map[uint8]hidKeySpec{
	4:   {macKeyCode: 0x00}, // A
	5:   {macKeyCode: 0x0B}, // B
	6:   {macKeyCode: 0x08}, // C
	7:   {macKeyCode: 0x02}, // D
	8:   {macKeyCode: 0x0E}, // E
	9:   {macKeyCode: 0x03}, // F
	10:  {macKeyCode: 0x05}, // G
	11:  {macKeyCode: 0x04}, // H
	12:  {macKeyCode: 0x22}, // I
	13:  {macKeyCode: 0x26}, // J
	14:  {macKeyCode: 0x28}, // K
	15:  {macKeyCode: 0x25}, // L
	16:  {macKeyCode: 0x2E}, // M
	17:  {macKeyCode: 0x2D}, // N
	18:  {macKeyCode: 0x1F}, // O
	19:  {macKeyCode: 0x23}, // P
	20:  {macKeyCode: 0x0C}, // Q
	21:  {macKeyCode: 0x0F}, // R
	22:  {macKeyCode: 0x01}, // S
	23:  {macKeyCode: 0x11}, // T
	24:  {macKeyCode: 0x20}, // U
	25:  {macKeyCode: 0x09}, // V
	26:  {macKeyCode: 0x0D}, // W
	27:  {macKeyCode: 0x07}, // X
	28:  {macKeyCode: 0x10}, // Y
	29:  {macKeyCode: 0x06}, // Z
	30:  {macKeyCode: 0x12}, // 1
	31:  {macKeyCode: 0x13}, // 2
	32:  {macKeyCode: 0x14}, // 3
	33:  {macKeyCode: 0x15}, // 4
	34:  {macKeyCode: 0x17}, // 5
	35:  {macKeyCode: 0x16}, // 6
	36:  {macKeyCode: 0x1A}, // 7
	37:  {macKeyCode: 0x1C}, // 8
	38:  {macKeyCode: 0x19}, // 9
	39:  {macKeyCode: 0x1D}, // 0
	40:  {macKeyCode: 0x24}, // Enter
	41:  {macKeyCode: 0x35}, // Escape
	42:  {macKeyCode: 0x33}, // Backspace
	43:  {macKeyCode: 0x30}, // Tab
	44:  {macKeyCode: 0x31}, // Space
	45:  {macKeyCode: 0x1B}, // -
	46:  {macKeyCode: 0x18}, // =
	47:  {macKeyCode: 0x21}, // [
	48:  {macKeyCode: 0x1E}, // ]
	49:  {macKeyCode: 0x2A}, // \
	51:  {macKeyCode: 0x29}, // ;
	52:  {macKeyCode: 0x27}, // '
	53:  {macKeyCode: 0x32}, // `
	54:  {macKeyCode: 0x2B}, // ,
	55:  {macKeyCode: 0x2F}, // .
	56:  {macKeyCode: 0x2C}, // /
	57:  {macKeyCode: 0x39}, // CapsLock
	58:  {macKeyCode: 0x7A}, // F1
	59:  {macKeyCode: 0x78}, // F2
	60:  {macKeyCode: 0x63}, // F3
	61:  {macKeyCode: 0x76}, // F4
	62:  {macKeyCode: 0x60}, // F5
	63:  {macKeyCode: 0x61}, // F6
	64:  {macKeyCode: 0x62}, // F7
	65:  {macKeyCode: 0x64}, // F8
	66:  {macKeyCode: 0x65}, // F9
	67:  {macKeyCode: 0x6D}, // F10
	68:  {macKeyCode: 0x67}, // F11
	69:  {macKeyCode: 0x6F}, // F12
	73:  {macKeyCode: 0x72}, // Insert
	74:  {macKeyCode: 0x73}, // Home
	75:  {macKeyCode: 0x74}, // PageUp
	76:  {macKeyCode: 0x75}, // Delete
	77:  {macKeyCode: 0x77}, // End
	78:  {macKeyCode: 0x79}, // PageDown
	79:  {macKeyCode: 0x7C}, // Right
	80:  {macKeyCode: 0x7B}, // Left
	81:  {macKeyCode: 0x7D}, // Down
	82:  {macKeyCode: 0x7E}, // Up
	224: {macKeyCode: 0x3B}, // Left Ctrl
	225: {macKeyCode: 0x38}, // Left Shift
	226: {macKeyCode: 0x3A}, // Left Alt/Option
	227: {macKeyCode: 0x37}, // Left GUI/Command
	228: {macKeyCode: 0x3E}, // Right Ctrl
	229: {macKeyCode: 0x3C}, // Right Shift
	230: {macKeyCode: 0x3D}, // Right Alt/Option
	231: {macKeyCode: 0x36}, // Right GUI/Command
}

func hidSpec(key uint8) (hidKeySpec, bool) {
	spec, ok := hidKeyTable[key]
	return spec, ok
}
