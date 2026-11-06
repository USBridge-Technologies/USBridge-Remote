//go:build !windows

package service

// InitGPUSafeMode is Windows-only (gpu_safe_mode_windows.go).
func InitGPUSafeMode(appName, version string) {}

// GPUSafeModeLevel is always 0 off Windows.
func GPUSafeModeLevel() int { return 0 }
