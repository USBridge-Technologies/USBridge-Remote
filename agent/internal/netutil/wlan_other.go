//go:build !windows

package netutil

// SetWLANStreamingMode is Windows only (see wlan_windows.go); macOS has
// its own AWDL knob (awdl_darwin.go).
func SetWLANStreamingMode(on bool) error { return nil }
