//go:build linux

package input

type hidKeySpec struct {
	linuxKey uint16
}

// HID usage code → Linux KEY_* code mapping (US layout)
var hidKeyTable = map[uint8]hidKeySpec{
	4:   {30},  // A
	5:   {48},  // B
	6:   {46},  // C
	7:   {32},  // D
	8:   {18},  // E
	9:   {33},  // F
	10:  {34},  // G
	11:  {35},  // H
	12:  {23},  // I
	13:  {36},  // J
	14:  {37},  // K
	15:  {38},  // L
	16:  {50},  // M
	17:  {49},  // N
	18:  {24},  // O
	19:  {25},  // P
	20:  {16},  // Q
	21:  {19},  // R
	22:  {31},  // S
	23:  {20},  // T
	24:  {22},  // U
	25:  {47},  // V
	26:  {17},  // W
	27:  {45},  // X
	28:  {21},  // Y
	29:  {44},  // Z
	30:  {2},   // 1
	31:  {3},   // 2
	32:  {4},   // 3
	33:  {5},   // 4
	34:  {6},   // 5
	35:  {7},   // 6
	36:  {8},   // 7
	37:  {9},   // 8
	38:  {10},  // 9
	39:  {11},  // 0
	40:  {28},  // Enter
	41:  {1},   // Escape
	42:  {14},  // Backspace
	43:  {15},  // Tab
	44:  {57},  // Space
	45:  {12},  // -
	46:  {13},  // =
	47:  {26},  // [
	48:  {27},  // ]
	49:  {43},  // backslash
	51:  {39},  // ;
	52:  {40},  // '
	53:  {41},  // `
	54:  {51},  // ,
	55:  {52},  // .
	56:  {53},  // /
	57:  {58},  // CapsLock
	58:  {59},  // F1
	59:  {60},  // F2
	60:  {61},  // F3
	61:  {62},  // F4
	62:  {63},  // F5
	63:  {64},  // F6
	64:  {65},  // F7
	65:  {66},  // F8
	66:  {67},  // F9
	67:  {68},  // F10
	68:  {87},  // F11
	69:  {88},  // F12
	73:  {110}, // Insert
	74:  {102}, // Home
	75:  {104}, // PageUp
	76:  {111}, // Delete
	77:  {107}, // End
	78:  {109}, // PageDown
	79:  {106}, // Right
	80:  {105}, // Left
	81:  {108}, // Down
	82:  {103}, // Up
	224: {29},  // Left Ctrl
	225: {42},  // Left Shift
	226: {56},  // Left Alt
	227: {125}, // Left Meta
	228: {97},  // Right Ctrl
	229: {54},  // Right Shift
	230: {100}, // Right Alt
	231: {126}, // Right Meta
}

func hidSpec(key uint8) (hidKeySpec, bool) {
	spec, ok := hidKeyTable[key]
	return spec, ok
}
