package view

import "testing"

func TestCompactLANHostForDisplay(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"  ", ""},
		{"192.168.1.10", "192.168.1.10"},
		{"45d2f6ff464957eab080.device.usbridge.io", "45d2f6ff464957eab080"},
		{"ABC.Device.UsBridge.IO", "ABC"},
		{"box.local", "box.local"},
		{"something.device.usbridge.io.extra", "something.device.usbridge.io.extra"},
	}
	for _, tc := range tests {
		if got := CompactLANHostForDisplay(tc.in); got != tc.want {
			t.Errorf("CompactLANHostForDisplay(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestConnectionCardLANAddressOrNone(t *testing.T) {
	if got := connectionCardLANAddressOrNone(""); got != "none" {
		t.Fatalf("empty -> %q, want none", got)
	}
	if got := connectionCardLANAddressOrNone("45d2f6ff464957eab080.device.usbridge.io"); got != "45d2f6ff464957eab080" {
		t.Fatalf("device host -> %q", got)
	}
}
