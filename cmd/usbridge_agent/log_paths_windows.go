//go:build windows

package main

import (
	"os"
	"path/filepath"
)

func resolvePlatformLogDir() string {
	if exePath, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exePath), "logs")
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "logs")
	}
	return filepath.Join(".", "logs")
}
