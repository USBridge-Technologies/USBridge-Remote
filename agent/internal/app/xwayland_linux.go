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
// Scoping the clear around only GUI creation -- not the whole process --
// avoids all of that. Callers must NOT call the returned restore() right
// after the fyne.App constructor, though: GLFW queries the window's
// content scale at *window* (surface) creation time, inside
// ui.Window.ShowAndRun's w.app.NewWindow call, not at app-constructor time.
// Confirmed live the hard way: restoring right after fyneapp.NewWithID left
// WAYLAND_DISPLAY back to its real value well before ShowAndRun ever ran,
// so the window still picked up native Wayland and hit the stale-scale bug
// anyway -- symptom was mouse clicks silently landing on the wrong widget
// (a Screen Capture "Request" button that visually released without its
// handler ever firing -- confirmed by the total absence of its own
// unconditional log line). Callers must instead pass restore to
// Window.SetAfterWindowCreated, which ShowAndRun calls immediately after
// creating the real window, keeping WAYLAND_DISPLAY cleared across the
// whole app-constructor-through-window-creation span.
func forceXWaylandForGUI() (restore func()) {
	orig, had := os.LookupEnv("WAYLAND_DISPLAY")
	os.Unsetenv("WAYLAND_DISPLAY")
	return func() {
		if had {
			os.Setenv("WAYLAND_DISPLAY", orig)
		}
	}
}
