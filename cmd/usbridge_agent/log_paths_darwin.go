//go:build darwin

package main

import (
	"os"
	"path/filepath"
)

func resolvePlatformLogDir() string {
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		return filepath.Join(homeDir, "Library", "Logs", "USBridgeAgent")
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "logs")
	}
	if exePath, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exePath), "logs")
	}
	return filepath.Join(".", "logs")
}
