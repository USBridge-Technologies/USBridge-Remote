//go:build windows

package service

import (
	"os"
	"os/exec"
	"path/filepath"
)

func getTailscaleBinaryPath() string {
	name := "tailscale.exe"
	if exePath, err := os.Executable(); err == nil {
		localPath := filepath.Join(filepath.Dir(exePath), name)
		if _, err := os.Stat(localPath); err == nil {
			return localPath
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	return ""
}
