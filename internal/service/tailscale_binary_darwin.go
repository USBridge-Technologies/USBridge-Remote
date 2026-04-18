//go:build darwin

package service

import (
	"os"
	"os/exec"
	"path/filepath"
)

func getTailscaleBinaryPath() string {
	name := "tailscale"
	if exePath, err := os.Executable(); err == nil {
		localPath := filepath.Join(filepath.Dir(exePath), name)
		if _, err := os.Stat(localPath); err == nil {
			return localPath
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	macosPath := "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
	if _, err := os.Stat(macosPath); err == nil {
		return macosPath
	}
	return ""
}
