//go:build !darwin && !windows

package main

import (
	"os"
	"path/filepath"
)

func resolvePlatformLogDir() string {
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "logs")
	}
	if exePath, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exePath), "logs")
	}
	return filepath.Join(".", "logs")
}
