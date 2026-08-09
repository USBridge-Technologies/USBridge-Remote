//go:build android
// +build android

package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"

	"usbridge-client/internal/gui"
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/platform"
	"usbridge-client/internal/update"

	"github.com/sirupsen/logrus"
)

const appName = "usbridge-client"

// embeddedVersion is client/VERSION, copied to this directory by
// scripts/build_android_gradle.sh before each build (embed can't reach a
// parent directory, and the Android gomobile/fyne build has no ldflags -X
// injection step like cmd/main.go's desktop build does).
//
//go:embed VERSION
var embeddedVersion string

var version = strings.TrimSpace(embeddedVersion)

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
	startPprofIfEnabled()

	logrus.Infof("🚀 Starting %s version %s", appName, version)

	gui.SetAppVersion(version)
	view.SetAppVersion(version)

	// Create the default configuration for Android
	config := models.DefaultConfig()

	// Wire Android IME keyboard height into overlay popup layout so popups
	// (e.g. connection editor dialog) reposition above the on-screen keyboard.
	view.KeyboardHeight = graphics.GetLastIMEH

	// Create the main window
	mainWindow := gui.NewMainWindow(config)

	// Mandatory startup update check, gated on user confirmation — a forced
	// silent update was jarring for a window the user is actively looking
	// at, so this asks first via a "new version available, update now?"
	// dialog. Android can't silently replace its own APK the way desktop
	// platforms do even after a "yes": apply() (see
	// internal/update/apply_android.go) hands the already-downloaded,
	// already-verified APK straight to the system PackageInstaller via
	// these hooks (internal/platform's JNI bridge to MainActivity —
	// F-Droid's approach), instead of redirecting to a browser download of
	// the GitHub release page. Android's own signing-cert check on install
	// is the final MITM/tamper gate on top of this package's Ed25519
	// manifest + SHA256 checks. Run in the background rather than blocking
	// startup like the desktop build does — Android is far more
	// ANR-sensitive about a slow main-thread network call before the
	// first frame is shown.
	update.InstallAPK = platform.InstallAPK
	update.CanRequestInstall = platform.CanRequestPackageInstalls
	update.RequestInstallPermission = platform.RequestInstallPermission
	go func() {
		manifest := update.Check(context.Background(), version)
		if manifest == nil {
			return
		}
		mainWindow.ShowUpdateAvailableDialog(manifest.Version, version, func(confirmed bool) {
			if !confirmed {
				logrus.Info("update declined by user")
				return
			}
			// A progress dialog while this downloads — Android APKs run
			// tens of MB, and a self-update that just sits there with no
			// visible activity is indistinguishable from having frozen.
			progress := mainWindow.ShowUpdateProgressDialog(manifest.Version)
			go func() {
				err := update.DownloadAndApply(context.Background(), manifest, progress.Update)
				progress.Close()
				if err != nil {
					logrus.Errorf("failed to apply update: %v", err)
				}
			}()
		})
	}()

	// Set up the network-change callback
	platform.SetOnNetworkChangedCallback(func() {
		logrus.Info("🌐 Network change notification received, refreshing services...")
		mainWindow.RefreshNetworkState()
	})
	// Mark app as ready only after GUI has started to prevent early JNI callbacks from crashing
	mainWindow.SetOnReadyCallback(platform.SetAppReady)

	logrus.Infof("📋 Configuration:")
	logrus.Infof("  Video UDP bind: %s:%d", config.VideoBindHost, config.VideoUDPPort)

	// Check access to the SD card
	checkStorageAccess()

	// Start the application
	logrus.Info("🎨 Starting the GUI")
	mainWindow.Show()
}

// startPprofIfEnabled starts a loopback-only net/http/pprof server, gated by
// the presence of a marker file (rather than always-on, since other apps on
// the same Android device *can* reach 127.0.0.1 — unlike iOS's per-app
// network sandbox). Enable for a profiling session with:
//
//	adb shell touch /sdcard/usbridge_pprof
//	adb forward tcp:6060 tcp:6060
//	go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=10
//
// Disable again with `adb shell rm /sdcard/usbridge_pprof`.
func startPprofIfEnabled() {
	const marker = "/sdcard/usbridge_pprof"
	if _, err := os.Stat(marker); err != nil {
		return
	}
	const addr = "127.0.0.1:6060"
	go func() {
		logrus.Warnf("pprof debug server listening on http://%s/debug/pprof/ (marker file %s present)", addr, marker)
		if err := http.ListenAndServe(addr, nil); err != nil {
			logrus.Errorf("pprof server failed: %v", err)
		}
	}()
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
