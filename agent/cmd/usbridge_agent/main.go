package main

import (
	"flag"
	"log"
	"os"

	"usbridge_agent/internal/app"
	"usbridge_agent/internal/ui"
	"github.com/sirupsen/logrus"
)

// version is injected at build time via -ldflags "-X main.version=...";
// see agent/scripts/build_linux.sh (and the other platform build scripts).
var version = "dev"

func main() {
	headless := flag.Bool("headless", false, "run without a GUI (HTTP server, Sunshine, Tailscale only); a later normal launch attaches a GUI to this instance instead of starting a second one")
	flag.Parse()

	setupLogging()
	ui.SetAppVersion(version)

	if err := app.Start(*headless); err != nil {
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
		FullTimestamp: true,
		TimestampFormat: "2006/01/02 15:04:05.000000",
	})

	log.Printf("logging initialized: %s", logFilePath)
}
