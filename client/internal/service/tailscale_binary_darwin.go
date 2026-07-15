//go:build darwin

package service

import (
	"os"
	"os/exec"
	"path/filepath"
)

func getTailscaleBinaryPath() string {
	name := "tailscale"
	// Check next to the app binary first (bundled copy).
	if exePath, err := os.Executable(); err == nil {
		localPath := filepath.Join(filepath.Dir(exePath), name)
		if _, err := os.Stat(localPath); err == nil {
			return localPath
		}
	}
	// Explicit well-known paths first — GUI app bundles launched from Finder/Dock
	// do NOT inherit the shell PATH, so exec.LookPath misses Homebrew binaries.
	for _, p := range []string{
		"/opt/homebrew/bin/tailscale", // Homebrew on Apple Silicon
		"/usr/local/bin/tailscale",    // Homebrew on Intel Mac
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fall back to PATH lookup (works when launched from a terminal).
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	return ""
}
