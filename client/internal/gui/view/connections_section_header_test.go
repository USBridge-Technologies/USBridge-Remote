package view

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

func TestConnectionPlatformLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		os, protocol, want string
	}{
		{"", "", ""},
		{"usbridge", "pro", "Radxa"},
		{"Windows", "", ""},
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

func TestAppendConnectionSortBadgesHidesZeroCounts(t *testing.T) {
	t.Parallel()
	placeholder := canvas.NewRectangle(color.Transparent)
	items := []fyne.CanvasObject{placeholder}
	nop := func(string) func() { return nil }

	out := appendConnectionSortBadges(items, ConnectionsSummary{}, "", nop)
	if len(out) != 2 {
		t.Fatalf("empty summary should keep 0 KVM and hide Agent/Unknown, got %d items", len(out))
	}

	out = appendConnectionSortBadges(items, ConnectionsSummary{AgentCount: 2, KVMCount: 1}, "", nop)
	if len(out) != 3 {
		t.Fatalf("non-zero Agent+KVM should add those two badges, got %d items", len(out))
	}

	out = appendConnectionSortBadges(items, ConnectionsSummary{UnknownCount: 3}, "", nop)
	if len(out) != 3 {
		t.Fatalf("Unknown should appear only when > 0 (plus 0 KVM), got %d items", len(out))
	}
}
