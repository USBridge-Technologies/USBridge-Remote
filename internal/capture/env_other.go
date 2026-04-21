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
