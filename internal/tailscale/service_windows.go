//go:build windows

package tailscale

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func (s *Service) getTailscalePath() string {
	// 1. Program Files
	progFiles := os.Getenv("ProgramFiles")
	if progFiles != "" {
		p := filepath.Join(progFiles, "Tailscale", "tailscale.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// 2. Program Files (x86)
	progFilesX86 := os.Getenv("ProgramFiles(x86)")
	if progFilesX86 != "" {
		p := filepath.Join(progFilesX86, "Tailscale", "tailscale.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// 3. Local AppData (some users install there)
	localAppData := os.Getenv("LocalAppData")
	if localAppData != "" {
		p := filepath.Join(localAppData, "Tailscale", "tailscale.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// 4. PATH
	if path, err := exec.LookPath("tailscale.exe"); err == nil {
		return path
	}
	return ""
}

func (s *Service) upArgs() []string {
	return []string{"up", "--accept-dns=false"}
}

func (s *Service) prepareUpCommand(tsPath string, args []string) *exec.Cmd {
	return exec.Command(tsPath, args...)
}

func (s *Service) handleUpStartError(tsPath string, args []string, err error) (string, error) {
	return "", fmt.Errorf("windows tailscale up failed: %w", err)
}

func (s *Service) runLogoutCommand(tsPath string) error {
	return exec.Command(tsPath, "logout").Run()
}
