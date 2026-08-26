//go:build !linux

package capture

import "runtime"

func GetLinuxEnv() string {
	return ""
}

func GetOSInfo() string {
	if runtime.GOOS == "darwin" {
		return "macOS"
	}
	if runtime.GOOS == "windows" {
		return "Windows"
	}
	return runtime.GOOS
}

func GetDisplayServer() string {
	return ""
}

// AutoCaptureMode is Linux-only (Sunshine's capture backend selection —
// "portal"/"x11"/"kms" — has no equivalent concept on other platforms). See
// env_linux.go.
func AutoCaptureMode() string {
	return ""
}
