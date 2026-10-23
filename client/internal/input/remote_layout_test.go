package input

import "testing"

func TestRemoteLayoutHungarian(t *testing.T) {
	const hu = "0000040E"
	cases := []struct {
		r    rune
		want RemoteKey
	}{
		// The reported bug: 0 sits left of 1, where US has the backtick.
		{'0', RemoteKey{ScanCode: 0x29}},
		{'ö', RemoteKey{ScanCode: 0x0B}},
		{'z', RemoteKey{ScanCode: 0x15}}, // QWERTZ
		{'Z', RemoteKey{ScanCode: 0x15, Shift: true}},
		{'@', RemoteKey{ScanCode: 0x2F, AltGr: true}},
		{'ű', RemoteKey{ScanCode: 0x2B}},
		{'^', RemoteKey{ScanCode: 0x04, AltGr: true, Dead: true}},
	}
	for _, c := range cases {
		got, ok := RemoteLayoutKey(hu, c.r)
		if !ok || got != c.want {
			t.Errorf("%q: got %+v ok=%v, want %+v", c.r, got, ok, c.want)
		}
	}
}

// The US table must press what typing on a KVM always pressed.
func TestRemoteLayoutUSMatchesCommonRuneMap(t *testing.T) {
	for r := rune(0x20); r < 0x7F; r++ {
		k, ok := RemoteLayoutKey(RemoteLayoutUS, r)
		if !ok {
			t.Errorf("%q missing from the US table", r)
			continue
		}
		info, ok := CommonRuneMap[r]
		if !ok {
			continue
		}
		if usHID[k.ScanCode] != info.KeyCode || k.Shift != (info.Modifiers&2 != 0) || k.AltGr || k.Dead {
			t.Errorf("%q: table %+v, CommonRuneMap HID %d mods %d", r, k, info.KeyCode, info.Modifiers)
		}
	}
}

// PS/2 Set-1 -> HID usage for the US character keys.
var usHID = map[int]int{
	0x02: 30, 0x03: 31, 0x04: 32, 0x05: 33, 0x06: 34, 0x07: 35, 0x08: 36, 0x09: 37, 0x0A: 38, 0x0B: 39,
	0x0C: 45, 0x0D: 46, 0x10: 20, 0x11: 26, 0x12: 8, 0x13: 21, 0x14: 23, 0x15: 28, 0x16: 24, 0x17: 12,
	0x18: 18, 0x19: 19, 0x1A: 47, 0x1B: 48, 0x1E: 4, 0x1F: 22, 0x20: 7, 0x21: 9, 0x22: 10, 0x23: 11,
	0x24: 13, 0x25: 14, 0x26: 15, 0x27: 51, 0x28: 52, 0x29: 53, 0x2B: 49, 0x2C: 29, 0x2D: 27, 0x2E: 6,
	0x2F: 25, 0x30: 5, 0x31: 17, 0x32: 16, 0x33: 54, 0x34: 55, 0x35: 56, 0x39: 44,
}

func TestEveryRemoteLayoutKeyHasAVK(t *testing.T) {
	for _, l := range RemoteLayouts() {
		for _, e := range remoteLayoutTables[remoteLayoutIndex(l.ID)].entries {
			if VKCodeFromPS2ScanCode(int(e&0xFF)) == 0 {
				t.Errorf("%s: %q on scan 0x%02X has no VK", l.Name, rune(e>>16), e&0xFF)
			}
		}
	}
}

func TestResolveRemoteLayout(t *testing.T) {
	if got := ResolveRemoteLayout("0000040E"); got != "0000040E" {
		t.Errorf("explicit = %s", got)
	}
	if got := ResolveRemoteLayout("DEADBEEF"); got != RemoteLayoutUS {
		t.Errorf("unknown = %s, want US", got)
	}
}

func TestKLIDFromHKL(t *testing.T) {
	variant := func(id uint16) string {
		if id == 0x0001 {
			return "0001040E"
		}
		return ""
	}
	for hkl, want := range map[uint32]string{
		0x040E040E: "0000040E", // Hungarian
		0x0409040E: "00000409", // Hungarian language, US layout
		0xF001040E: "0001040E", // Hungarian 101-key (a variant)
		0x00000419: "00000419",
	} {
		if got := klidFromHKL(hkl, variant); got != want {
			t.Errorf("%08X -> %q, want %q", hkl, got, want)
		}
	}
}

func TestLayoutForLanguage(t *testing.T) {
	for tag, want := range map[string]string{
		"hu-HU": "0000040E", "hu_HU": "0000040E", "hu": "0000040E",
		"en-US": "00000409", "en-GB": "00000809", "de-CH": "00000807",
		"fr_CA": "00001009", "pt-BR": "00000416", "pt-PT": "00000816",
		"es-ES": "0000040A", "es-MX": "0000080A", "ru-RU": "00000419",
		"ja-JP": "",
	} {
		if got := layoutForLanguage(tag); got != want {
			t.Errorf("%s -> %q, want %q", tag, got, want)
		}
	}
}
