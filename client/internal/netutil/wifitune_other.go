//go:build !windows && (!linux || android)

package netutil

// The Wi-Fi streaming tune is Windows and desktop Linux only (see
// wifitune_windows.go / wifitune_linux.go).

func WiFiTuneStatus() (applicable, done bool) { return false, false }

func ApplyWiFiTune() error { return nil }

func WLANConnected() bool { return false }
