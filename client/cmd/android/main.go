// +build android

package main

import (
	"flag"
	"fmt"
	"os"

	"usbridge-client/internal/gui"
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/platform"

	"github.com/sirupsen/logrus"
)

const (
	appName = "usbridge-client"
	version = "1.0.0-android"
)

func main() {
	// Parse command-line arguments
	var (
		logLevel    = flag.String("log-level", "info", "Logging level (debug, info, warn, error)")
		showVersion = flag.Bool("version", false, "Show version")
	)
	flag.Parse()

	// Show the version if requested
	if *showVersion {
		fmt.Printf("%s version %s\n", appName, version)
		os.Exit(0)
	}

	// Set up logging for Android
	setupAndroidLogging(*logLevel)

	logrus.Infof("🚀 Starting %s version %s", appName, version)

	// Create the default configuration for Android
	config := models.DefaultConfig()

	// Wire Android IME keyboard height into overlay popup layout so popups
	// (e.g. connection editor dialog) reposition above the on-screen keyboard.
	view.KeyboardHeight = graphics.GetLastIMEH

	// Create the main window
	mainWindow := gui.NewMainWindow(config)

	// Set up the network-change callback
	platform.SetOnNetworkChangedCallback(func() {
		logrus.Info("🌐 Network change notification received, refreshing services...")
		mainWindow.RefreshNetworkState()
	})
	// Mark app as ready only after GUI has started to prevent early JNI callbacks from crashing
	mainWindow.SetOnReadyCallback(platform.SetAppReady)

	logrus.Infof("📋 Configuration:")
	logrus.Infof("  NBD port: %d", config.NBDPort)
	logrus.Infof("  Video UDP bind: %s:%d", config.VideoBindHost, config.VideoUDPPort)

	// Check access to the SD card
	checkStorageAccess()

	// Start the application
	logrus.Info("🎨 Starting the GUI")
	mainWindow.Show()
}

// checkStorageAccess checks access to external storage
func checkStorageAccess() {
	testPaths := []string{
		"/storage/emulated/0",
		"/sdcard",
		os.Getenv("EXTERNAL_STORAGE"),
	}

	for _, path := range testPaths {
		if path == "" {
			continue
		}

		if _, err := os.Stat(path); err == nil {
			logrus.Infof("✅ Storage access: %s", path)
			return
		} else {
			logrus.Warnf("⚠️ No access to %s: %v", path, err)
		}
	}

	logrus.Warn("⚠️ Warning: No access to external storage")
	logrus.Warn("⚠️ Grant permission in Settings → Apps → USBridge Client → Permissions → Files and media")
}

// setupAndroidLogging sets up logging for Android
func setupAndroidLogging(level string) {
	// Set the logging level
	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		logrus.Warnf("Invalid logging level %s, using info", level)
		logLevel = logrus.InfoLevel
	}
	logrus.SetLevel(logLevel)

	// Configure the log format
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "15:04:05",
	})

	// Redirect logrus to Android logcat (stdout may not show up in adb logcat on Android)
	platform.SetupLogrusForAndroid()
	logrus.Info("📝 Logs are written to Android logcat (adb logcat -s USBridge)")
}
