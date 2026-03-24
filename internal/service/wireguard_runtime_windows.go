//go:build windows

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ensureWireGuardRuntimeAvailable() error {
	candidates := wireGuardRuntimeCandidates()
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return nil
		}
	}

	return fmt.Errorf("missing WireGuard runtime dependency: wintun.dll was not found in any supported location (%s)", strings.Join(candidates, ", "))
}

func wireGuardRuntimeCandidates() []string {
	exePath, err := os.Executable()
	if err != nil {
		return nil
	}

	exeDir := filepath.Dir(exePath)
	candidates := []string{
		filepath.Join(exeDir, "wintun.dll"),
	}

	windir := strings.TrimSpace(os.Getenv("WINDIR"))
	if windir != "" {
		candidates = append(candidates, filepath.Join(windir, "System32", "wintun.dll"))
	}
	return candidates
}
