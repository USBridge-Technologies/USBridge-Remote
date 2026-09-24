package input

import "runtime"

// NormalizeScanCode converts the scan code GLFW reports in
// fyne.KeyEvent.Physical.ScanCode into a Windows PS/2 Set-1 scan code
// (extended keys as base+0x100), the single number space every scan-code
// helper in this package and in the video widget works in.
//
// GLFW's scan code is not one universal number space:
//   - Windows: the raw PS/2 Set-1 code from WM_KEYDOWN -- already normalized.
//   - Linux (X11 and Wayland): the xkb keycode, i.e. evdev keycode + 8. For
//     the main block evdev keycodes equal PS/2 Set-1 codes, so subtracting 8
//     is enough there; the navigation cluster needs a table.
//   - macOS: the Carbon virtual keycode (kVK_*), an unrelated numbering where
//     e.g. digit 0 is 0x1D and digit 8 is 0x1C -- the latter collides with the
//     PS/2 code for Enter. Reading macOS codes as PS/2 made 0/8/A/S/E/R fall
//     outside the "character key" ranges, so they were sent twice (once as a
//     raw VK from KeyDown, once as TypedRune text) and 8 also sent Enter.
//
// Returns 0 for a code with no PS/2 equivalent.
func NormalizeScanCode(scanCode int) int {
	return normalizeScanCodeFor(runtime.GOOS, scanCode)
}

func normalizeScanCodeFor(goos string, scanCode int) int {
	switch goos {
	case "darwin":
		return macKeyCodeToPS2[scanCode]
	case "linux":
		if ps2, ok := linuxExtendedToPS2[scanCode]; ok {
			return ps2
		}
		// xkb keycodes 9..96 are evdev 1..88 (Esc..F12), identical to PS/2.
		if scanCode >= 9 && scanCode <= 96 {
			return scanCode - 8
		}
		return 0
	default:
		return scanCode
	}
}

// linuxExtendedToPS2 covers xkb keycodes outside the evdev==PS/2 main block.
var linuxExtendedToPS2 = map[int]int{
	104: 0x11C, // KP_Enter
	105: 0x11D, // Control_R
	106: 0x135, // KP_Divide
	108: 0x138, // Alt_R
	110: 0x147, // Home
	111: 0x148, // Up
	112: 0x149, // Prior
	113: 0x14B, // Left
	114: 0x14D, // Right
	115: 0x14F, // End
	116: 0x150, // Down
	117: 0x151, // Next
	118: 0x152, // Insert
	119: 0x153, // Delete
	133: 0x15B, // Super_L
	134: 0x15C, // Super_R
}

// macKeyCodeToPS2 maps Carbon kVK_* codes (HIToolbox/Events.h) to PS/2 Set-1.
// kVK_ANSI_A is 0x00, which an int-keyed map lookup still resolves correctly.
var macKeyCodeToPS2 = map[int]int{
	0x00: 0x1E,  // A
	0x01: 0x1F,  // S
	0x02: 0x20,  // D
	0x03: 0x21,  // F
	0x04: 0x23,  // H
	0x05: 0x22,  // G
	0x06: 0x2C,  // Z
	0x07: 0x2D,  // X
	0x08: 0x2E,  // C
	0x09: 0x2F,  // V
	0x0A: 0x56,  // ISO section (non-US backslash)
	0x0B: 0x30,  // B
	0x0C: 0x10,  // Q
	0x0D: 0x11,  // W
	0x0E: 0x12,  // E
	0x0F: 0x13,  // R
	0x10: 0x15,  // Y
	0x11: 0x14,  // T
	0x12: 0x02,  // 1
	0x13: 0x03,  // 2
	0x14: 0x04,  // 3
	0x15: 0x05,  // 4
	0x16: 0x07,  // 6
	0x17: 0x06,  // 5
	0x18: 0x0D,  // =
	0x19: 0x0A,  // 9
	0x1A: 0x08,  // 7
	0x1B: 0x0C,  // -
	0x1C: 0x09,  // 8
	0x1D: 0x0B,  // 0
	0x1E: 0x1B,  // ]
	0x1F: 0x18,  // O
	0x20: 0x16,  // U
	0x21: 0x1A,  // [
	0x22: 0x17,  // I
	0x23: 0x19,  // P
	0x24: 0x1C,  // Return
	0x25: 0x26,  // L
	0x26: 0x24,  // J
	0x27: 0x28,  // '
	0x28: 0x25,  // K
	0x29: 0x27,  // ;
	0x2A: 0x2B,  // backslash
	0x2B: 0x33,  // ,
	0x2C: 0x35,  // /
	0x2D: 0x31,  // N
	0x2E: 0x32,  // M
	0x2F: 0x34,  // .
	0x30: 0x0F,  // Tab
	0x31: 0x39,  // Space
	0x32: 0x29,  // `
	0x33: 0x0E,  // Delete (Backspace)
	0x35: 0x01,  // Escape
	0x36: 0x15C, // Right Command
	0x37: 0x15B, // Command
	0x38: 0x2A,  // Shift
	0x39: 0x3A,  // Caps Lock
	0x3A: 0x38,  // Option
	0x3B: 0x1D,  // Control
	0x3C: 0x36,  // Right Shift
	0x3D: 0x138, // Right Option
	0x3E: 0x11D, // Right Control
	0x4C: 0x11C, // Keypad Enter
	0x72: 0x152, // Help / Insert
	0x73: 0x147, // Home
	0x74: 0x149, // Page Up
	0x75: 0x153, // Forward Delete
	0x77: 0x14F, // End
	0x79: 0x151, // Page Down
	0x7B: 0x14B, // Left
	0x7C: 0x14D, // Right
	0x7D: 0x150, // Down
	0x7E: 0x148, // Up
}
