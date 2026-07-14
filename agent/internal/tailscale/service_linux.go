//go:build linux

package tailscale

import (
	"fmt"
	"os/exec"
)

func (s *Service) getTailscalePath() string {
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path
	}
	return "/usr/bin/tailscale"
}

func (s *Service) upArgs() []string {
	return []string{"up", "--accept-dns=false", "--reset"}
}

func (s *Service) configureCommand(cmd *exec.Cmd) {}

func (s *Service) prepareUpCommand(tsPath string, args []string) *exec.Cmd {
	// Linux usually needs pkexec for up
	if _, err := exec.LookPath("pkexec"); err == nil {
		newArgs := append([]string{tsPath}, args...)
		return exec.Command("pkexec", newArgs...)
	}
	return exec.Command(tsPath, args...)
}

func (s *Service) handleUpStartError(tsPath string, args []string, err error) (string, error) {
	return "", fmt.Errorf("linux tailscale up failed: %w", err)
}

func (s *Service) runLogoutCommand(tsPath string) error {
	if _, err := exec.LookPath("pkexec"); err == nil {
		return exec.Command("pkexec", tsPath, "logout").Run()
	}
	return exec.Command(tsPath, "logout").Run()
}
