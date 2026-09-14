package main

import (
	"flag"
	"log"
	"os"

	"github.com/sirupsen/logrus"
	"usbridge_agent/internal/app"
	"usbridge_agent/internal/ui"
)

// version is injected at build time via -ldflags "-X main.version=...";
// see agent/scripts/build_linux.sh (and the other platform build scripts).
var version = "dev"

func main() {
	// The XWayland steering previously lived here as a process-wide
	// os.Unsetenv("WAYLAND_DISPLAY") that ran unconditionally, before even
	// --headless was parsed. It's now internal/app.forceXWaylandForGUI,
	// scoped tightly around the one Fyne app constructor call that actually
	// needs it -- see that file's doc comment for why the process-wide
	// version broke clipboard sync, capture, and autostart on Linux.

	headless := flag.Bool("headless", false, "run without a GUI (HTTP server, Sunshine, Tailscale only); a later normal launch attaches a GUI to this instance instead of starting a second one")
	installService := flag.Bool("install-service", false, "install Windows service (requires elevation)")
	uninstallService := flag.Bool("uninstall-service", false, "uninstall Windows service (requires elevation)")
	tray := flag.Bool("tray", false, "start minimized to the system tray instead of showing the window -- used by the login-time tray helper that keeps a status icon visible while the engine runs headless")
	attach := flag.String("attach", "", "dial this admin-socket path directly instead of the normal config-based discovery, and attach a thin-client GUI to it (Windows session-launch use: the LocalSystem service already knows its own socket path, which lives under a different profile than the interactive user's)")
	flag.Parse()

	setupLogging()
	ui.SetAppVersion(version)

	if *installService {
		if err := manageService("install"); err != nil {
			log.Fatalf("install service: %v", err)
		}
		return
	}
	if *uninstallService {
		if err := manageService("uninstall"); err != nil {
			log.Fatalf("uninstall service: %v", err)
		}
		return
	}

	runMain(*headless, *tray, *attach)
}

func doStart(headless, tray bool, attach string) {
	if err := app.Start(app.StartOptions{Headless: headless, Tray: tray, Attach: attach}, version); err != nil {
		log.Fatalf("start app: %v", err)
	}
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
		FullTimestamp:   true,
		TimestampFormat: "2006/01/02 15:04:05.000000",
	})

	log.Printf("logging initialized: %s", logFilePath)
}
