package service

import "testing"

func TestApplySwapWinAlt(t *testing.T) {
	defer SetSwapWinAlt(false)

	SetSwapWinAlt(false)
	if vk, m := applySwapWinAlt(0x5B, 0x08); vk != 0x5B || m != 0x08 {
		t.Fatalf("off: got %#x %#x", vk, m)
	}

	SetSwapWinAlt(true)
	cases := []struct {
		vk, wantVK   int16
		mods, wantMs int8
	}{
		{0x5B, 0xA4, 0x08, 0x04}, // LWin down -> LAlt, Meta bit -> Alt bit
		{0x5C, 0xA5, 0x00, 0x00}, // RWin -> RAlt
		{0xA4, 0x5B, 0x04, 0x08}, // LAlt -> LWin
		{0xA5, 0x5C, 0x06, 0x0A}, // RAlt with Ctrl -> RWin, Ctrl kept
		{0x43, 0x43, 0x04, 0x08}, // Alt+C -> Win+C (Cmd+C on a Mac)
		{0x09, 0x09, 0x0C, 0x0C}, // both held: unchanged
	}
	for _, c := range cases {
		if vk, m := applySwapWinAlt(c.vk, c.mods); vk != c.wantVK || m != c.wantMs {
			t.Errorf("%#x/%#x: got %#x/%#x, want %#x/%#x", c.vk, c.mods, vk, m, c.wantVK, c.wantMs)
		}
	}
}
