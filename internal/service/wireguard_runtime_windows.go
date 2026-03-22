//go:build windows

package service

import (
	"fmt"
	"os"
	"path/filepath"
)

func ensureWireGuardRuntimeAvailable() error {
	exePath, err := os.Executable()
	if err != nil {
		return nil
	}

	exeDir := filepath.Dir(exePath)
	wintunDLL := filepath.Join(exeDir, "wintun.dll")
	if _, err := os.Stat(wintunDLL); err == nil {
		return nil
	}

	return fmt.Errorf("missing WireGuard runtime dependency: %s was not found. Place wintun.dll next to %s or install the WireGuard runtime/driver on this machine", wintunDLL, filepath.Base(exePath))
}
