//go:build linux

package app

import "os"

// forceXWaylandForGUI steers Fyne's GLFW backend onto XWayland instead of
// native Wayland for the duration of GUI/driver initialization, by clearing
// WAYLAND_DISPLAY right before it and restoring it right after -- see
// cmd/usbridge_agent/main.go's (removed) forceXWaylandIfNeeded for the full
// root cause this works around (a stale content-scale query in GLFW's
// native-Wayland backend).
//
// This used to be done once, unconditionally, for the whole process
// lifetime, in main() before flag.Parse() even ran. That broke three other
// Linux subsystems that read the real WAYLAND_DISPLAY at arbitrary later
// times: clipboard/backend_linux.go's detect() (picks wl-clipboard over
// xclip -- and caches the choice forever via sync.Once, so once wiped it
// stayed wiped for the process's whole life), capture/env_linux.go (Sunshine
// KMS capture display), and autostart_linux.go (bakes it into the autostart
// unit). Confirmed live: clipboard sync failed permanently with "no
// clipboard tool available (install xclip or wl-clipboard)" even though
// wl-clipboard was installed and WAYLAND_DISPLAY was set on the real
// session, purely because detect() ran (lazily, well after main()) with
// WAYLAND_DISPLAY already gone. It also broke unconditionally in
// --headless mode, which never touches Fyne/GLFW at all and so gained
// nothing from the unset.
//
// Scoping the clear tightly around the one call that actually needs it (the
// Fyne app constructor, where GLFW's driver bootstraps and picks a
// platform) avoids all of that: every other subsystem, running before or
// after this narrow window, still sees the real value.
func forceXWaylandForGUI() (restore func()) {
	orig, had := os.LookupEnv("WAYLAND_DISPLAY")
	os.Unsetenv("WAYLAND_DISPLAY")
	return func() {
		if had {
			os.Setenv("WAYLAND_DISPLAY", orig)
		}
	}
}
