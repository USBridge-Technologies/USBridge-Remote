//go:build !windows

package netutil

// Wi-Fi streaming mode for a session is Windows only (see wlan_windows.go);
// desktop macOS has its own AWDL knob (awdl_darwin.go), Linux a one-time
// tune (wifitune_linux.go).
func SetWLANStreamingMode(on bool) error { return nil }
