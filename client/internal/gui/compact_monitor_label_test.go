package gui

import "testing"

func TestCompactMonitorMenuLabel(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"AMD Radeon RX 6800", "AMD Radeon RX 6800"},
		{"Display 1 AMD Radeon RX 6800", "AMD Radeon RX 6800"},
		{"Display 1 - AMD Radeon", "AMD Radeon"},
		{"Monitor 2: Samsung Odyssey", "Samsung Odyssey"},
		{"display 01 – HDMI", "HDMI"},
		{"Screen 3. Generic PnP", "Generic PnP"},
		{"DisplayPort", "DisplayPort"}, // no index — leave alone
	}
	for _, tc := range tests {
		if got := compactMonitorMenuLabel(tc.in); got != tc.want {
			t.Errorf("compactMonitorMenuLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
