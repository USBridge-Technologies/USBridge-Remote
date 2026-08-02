package main

import (
	"flag"
	"log"
	"os"
	"runtime"

	"usbridge_agent/internal/app"
	"usbridge_agent/internal/ui"
	"github.com/sirupsen/logrus"
)

// version is injected at build time via -ldflags "-X main.version=...";
// see agent/scripts/build_linux.sh (and the other platform build scripts).
var version = "dev"

func main() {
	forceXWaylandIfNeeded()

	headless := flag.Bool("headless", false, "run without a GUI (HTTP server, Sunshine, Tailscale only); a later normal launch attaches a GUI to this instance instead of starting a second one")
	flag.Parse()

	setupLogging()
	ui.SetAppVersion(version)

	if err := app.Start(*headless); err != nil {
		log.Fatalf("start app: %v", err)
	}
}

// forceXWaylandIfNeeded steers Fyne's GLFW backend onto XWayland instead of
// native Wayland on Linux, by clearing WAYLAND_DISPLAY before any GLFW/GL
// initialization happens (GLFW picks native Wayland whenever that env var
// is set and falls back to X11 otherwise, as long as DISPLAY is also set --
// XWayland provides that automatically on every compositor this app is
// realistically run under, so this never leaves the app without a display
// to attach to).
//
// Root cause this works around: GLFW's native-Wayland platform backend
// queries the window's content scale/DPI at surface-creation time, before
// the compositor has actually assigned the window to an output -- on this
// project's own real KDE Plasma (kwin_wayland) + NVIDIA test machine this
// reliably came back stale/wrong, and Fyne's canvas ends up laying out
// (and hit-testing mouse clicks) against that first wrong scale until
// something forces GLFW to requery it. A real user-driven window resize
// is exactly such a trigger -- confirmed live: resizing the window (after
// an ~1s stutter while GLFW/the compositor renegotiate) snaps the layout
// and click coordinates back to matching each other -- which is the same
// bug manifesting three ways in the field: a cramped-looking default
// layout (wrong initial scale), a window that "doesn't rebuild" on some
// resizes (scale only re-syncs on some resize events, not the layout
// engine being broken), and mouse clicks landing on the wrong widget
// (hit-testing done in the stale scale while the compositor already moved
// on). X11 (even proxied through XWayland) has no equivalent content-scale
// negotiation race -- DPI is queried once, synchronously, at window
// creation, which is why forcing that path sidesteps the whole class of
// bug instead of patching each symptom separately.
func forceXWaylandIfNeeded() {
	if runtime.GOOS != "linux" {
		return
	}
	os.Unsetenv("WAYLAND_DISPLAY")
}

func setupLogging() {
	logFilePath := appLogPath()
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.SetOutput(os.Stdout)
		log.Printf("logging fallback to stdout: %v", err)
		return
	}

	output, err := setupPlatformLogOutput(logFile)
	if err != nil {
		log.SetOutput(logFile)
		log.Printf("logging fallback to file only: %v", err)
		return
	}

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetOutput(output)
	
	// Configure logrus to use the same output
	logrus.SetOutput(output)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
		TimestampFormat: "2006/01/02 15:04:05.000000",
	})

	log.Printf("logging initialized: %s", logFilePath)
}
