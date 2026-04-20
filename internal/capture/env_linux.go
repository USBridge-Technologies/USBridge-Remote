//go:build linux

package capture

import (
	"os"
)

func GetLinuxEnv() string {
	waylandDisplay := os.Getenv("WAYLAND_DISPLAY")
	if waylandDisplay != "" {
		return "Wayland"
	}
	x11Display := os.Getenv("DISPLAY")
	if x11Display != "" {
		return "X11"
	}
	return "Unknown"
}

func GetOSInfo() string {
	return "Linux (" + GetLinuxEnv() + ")"
}
