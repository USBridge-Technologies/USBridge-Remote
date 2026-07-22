// Package autostart registers (or unregisters) the running agent binary
// with each OS's native "launch at login" mechanism: an XDG autostart
// .desktop entry on Linux, a LaunchAgent plist on macOS, and a Run registry
// value on Windows.
package autostart

import (
	"os"
	"strings"
)

// LaunchTarget resolves the executable path that should be registered for
// auto-start, plus the arguments it should be launched with.
//
// Under an AppImage, $APPIMAGE names the outer .AppImage file itself, not
// the ephemeral per-launch squashfs mount path os.Executable() would return
// — registering the mount path would point autostart at a location that no
// longer exists the moment this process exits.
//
// The registered command always launches with --headless: autostart's job
// is to have the agent (HTTP/Sunshine/Tailscale) up and running every boot,
// not to pop a window unasked. Opening the app normally afterwards attaches
// a GUI to that already-running instance instead of starting a second one —
// see app.Start.
func LaunchTarget() (exe string, args []string, err error) {
	if v := strings.TrimSpace(os.Getenv("APPIMAGE")); v != "" {
		return v, []string{"--headless"}, nil
	}
	exe, err = os.Executable()
	if err != nil {
		return "", nil, err
	}
	return exe, []string{"--headless"}, nil
}
