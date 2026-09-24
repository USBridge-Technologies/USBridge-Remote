package input

import "testing"

func TestNormalizeScanCodeFor(t *testing.T) {
	tests := []struct {
		name string
		goos string
		in   int
		want int
	}{
		{"windows is identity", "windows", 0x1C, 0x1C},
		{"windows extended", "windows", 0x148, 0x148},
		// macOS kVK codes that used to be misread as PS/2 (the double-typed keys).
		{"mac 0", "darwin", 0x1D, 0x0B},
		{"mac 8 is not Enter", "darwin", 0x1C, 0x09},
		{"mac A (keycode zero)", "darwin", 0x00, 0x1E},
		{"mac S", "darwin", 0x01, 0x1F},
		{"mac E", "darwin", 0x0E, 0x12},
		{"mac R", "darwin", 0x0F, 0x13},
		{"mac Return", "darwin", 0x24, 0x1C},
		{"mac keypad Enter", "darwin", 0x4C, 0x11C},
		{"mac Caps Lock", "darwin", 0x39, 0x3A},
		{"mac unknown", "darwin", 0x7F, 0},
		// Linux xkb keycode = evdev + 8.
		{"linux T is not Enter", "linux", 0x1C, 0x14},
		{"linux 1", "linux", 10, 0x02},
		{"linux Esc", "linux", 9, 0x01},
		{"linux F12", "linux", 96, 0x58},
		{"linux KP_Enter", "linux", 104, 0x11C},
		{"linux Up", "linux", 111, 0x148},
		{"linux below range", "linux", 8, 0},
	}
	for _, tt := range tests {
		if got := normalizeScanCodeFor(tt.goos, tt.in); got != tt.want {
			t.Errorf("%s: normalizeScanCodeFor(%q, 0x%X) = 0x%X, want 0x%X", tt.name, tt.goos, tt.in, got, tt.want)
		}
	}
}

// Every mapped macOS key must land on a distinct PS/2 code, or two physical
// keys would type the same thing.
func TestMacKeyCodeTableIsInjective(t *testing.T) {
	seen := map[int]int{}
	for mac, ps2 := range macKeyCodeToPS2 {
		if prev, dup := seen[ps2]; dup {
			t.Errorf("mac keycodes 0x%X and 0x%X both map to PS/2 0x%X", prev, mac, ps2)
		}
		seen[ps2] = mac
	}
}

// All character positions a macOS keyboard reports resolve to a VK code, so
// keys mode and the KeyUnknown fallback cover the whole main block.
func TestMacCharacterKeysResolveToVK(t *testing.T) {
	want := map[int]int16{
		0x1D: '0', 0x12: '1', 0x1C: '8', 0x19: '9',
		0x00: 'A', 0x01: 'S', 0x0E: 'E', 0x0F: 'R', 0x11: 'T', 0x10: 'Y',
		0x24: 0x0D, // Return
	}
	for mac, vk := range want {
		if got := getVKCodeFromPS2ScanCode(normalizeScanCodeFor("darwin", mac)); got != vk {
			t.Errorf("mac 0x%X -> VK 0x%X, want 0x%X", mac, got, vk)
		}
	}
}
