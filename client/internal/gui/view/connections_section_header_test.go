package view

import "testing"

func TestConnectionPlatformLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		os, protocol, want string
	}{
		{"", "", ""},
		{"usbridge", "pro", "Radxa"},
		{"Windows", "", "Opensource/Pro"},
		{"linux", "opensource", "Opensource"},
		{"darwin", "free", "Free"},
		{"macOS", "pro", "Pro"},
		{"Windows 11", "enterprise", "Enterprise"},
	}
	for _, tc := range cases {
		got := ConnectionPlatformLabel(tc.os, tc.protocol)
		if got != tc.want {
			t.Fatalf("os=%q protocol=%q: got %q, want %q", tc.os, tc.protocol, got, tc.want)
		}
	}
}
