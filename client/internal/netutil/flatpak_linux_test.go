//go:build linux && !android

package netutil

import "testing"

func TestSandboxDetection(t *testing.T) {
	for _, tc := range []struct {
		snap, flatpak string
		sandboxed     bool
	}{
		{"", "", false},
		{"/snap/usbridge-client/x1", "", true},
		{"", "io.usbridge.Client", true},
	} {
		t.Setenv("SNAP", tc.snap)
		t.Setenv("FLATPAK_ID", tc.flatpak)
		if got := inSandbox(); got != tc.sandboxed {
			t.Errorf("SNAP=%q FLATPAK_ID=%q: inSandbox() = %v", tc.snap, tc.flatpak, got)
		}
		if got := UDPBufferAdjustable(); got == tc.sandboxed {
			t.Errorf("SNAP=%q FLATPAK_ID=%q: UDPBufferAdjustable() = %v", tc.snap, tc.flatpak, got)
		}
	}
	// The header button with `snap connect` commands stays snap-only.
	t.Setenv("SNAP", "")
	t.Setenv("FLATPAK_ID", "io.usbridge.Client")
	if InSnap() {
		t.Error("InSnap() is true in a Flatpak")
	}
}
