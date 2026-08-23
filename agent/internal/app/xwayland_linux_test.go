//go:build linux

package app

import (
	"os"
	"testing"
)

// TestForceXWaylandForGUIRestores reproduces the bug behind clipboard sync
// (and capture/autostart) silently and permanently breaking on Linux: the
// old process-wide os.Unsetenv("WAYLAND_DISPLAY") in main() never restored
// the value, so any code reading it later (clipboard's detect(), cached
// forever via sync.Once) saw it gone even on a real Wayland session. This
// verifies forceXWaylandForGUI clears WAYLAND_DISPLAY only for its caller
// and puts the original value back afterwards.
func TestForceXWaylandForGUIRestores(t *testing.T) {
	t.Run("restores a previously-set value", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "wayland-0")

		restore := forceXWaylandForGUI()
		if v, ok := os.LookupEnv("WAYLAND_DISPLAY"); ok {
			t.Fatalf("WAYLAND_DISPLAY still set during GUI init: %q", v)
		}
		restore()

		if v, ok := os.LookupEnv("WAYLAND_DISPLAY"); !ok || v != "wayland-0" {
			t.Fatalf("WAYLAND_DISPLAY not restored: got %q, ok=%v", v, ok)
		}
	})

	t.Run("leaves an unset value unset", func(t *testing.T) {
		os.Unsetenv("WAYLAND_DISPLAY")

		restore := forceXWaylandForGUI()
		restore()

		if v, ok := os.LookupEnv("WAYLAND_DISPLAY"); ok {
			t.Fatalf("WAYLAND_DISPLAY unexpectedly set after restore: %q", v)
		}
	})
}
