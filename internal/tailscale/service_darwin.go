//go:build darwin

package tailscale

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// Default socket path for tailscaled on macOS (Homebrew version)
	homebrewSocket = "/var/run/tailscaled.socket"
)

func (s *Service) getTailscalePath() string {
	candidates := []string{
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
		"/opt/homebrew/bin/tailscale",
		"/usr/local/bin/tailscale",
	}

	if path, err := exec.LookPath("tailscale"); err == nil {
		candidates = append(candidates, path)
	}

	// 1. Try to find a binary that is already connected to a daemon
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
			// Check standard status AND with explicit homebrew socket
			err := exec.CommandContext(ctx, path, "status", "--json").Run()
			if err != nil {
				err = exec.CommandContext(ctx, path, "--socket", homebrewSocket, "status", "--json").Run()
			}
			cancel()
			if err == nil {
				return path
			}
		}
	}

	// 2. Fallback: return the first one that exists
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func (s *Service) upArgs() []string {
	// If it's homebrew, we might need to point to the socket
	args := []string{"up", "--accept-dns=false"}
	tsPath := s.getTailscalePath()
	if strings.Contains(tsPath, "homebrew") || strings.Contains(tsPath, "/usr/local/bin") {
		// Only add socket if it exists
		if _, err := os.Stat(homebrewSocket); err == nil {
			args = append([]string{"--socket", homebrewSocket}, args...)
		}
	}
	return args
}

func (s *Service) prepareUpCommand(tsPath string, args []string) *exec.Cmd {
	return exec.Command(tsPath, args...)
}

func (s *Service) handleUpStartError(tsPath string, args []string, err error) (string, error) {
	logrus.Infof("🚀 [Tailscale/macOS] Connection error: %v. Attempting to force-start daemon as root...", err)

	// 1. Try to start/restart the daemon correctly
	if strings.Contains(tsPath, "Tailscale.app") {
		logrus.Info("📦 [Tailscale/macOS] Opening Tailscale.app...")
		_ = exec.Command("open", "-a", "Tailscale").Run()
	} else {
		// Homebrew version: must be started with SUDO to work in kernel mode
		brewPath := "/opt/homebrew/bin/brew"
		if _, err := os.Stat("/usr/local/bin/brew"); err == nil {
			brewPath = "/usr/local/bin/brew"
		}
		
		logrus.Info("🍺 [Tailscale/macOS] Running 'sudo brew services restart tailscale'...")
		// Restart is safer to ensure it picks up root permissions
		script := fmt.Sprintf("do shell script \"sudo %s services restart tailscale 2>&1\" with administrator privileges", brewPath)
		out, err := exec.Command("osascript", "-e", script).CombinedOutput()
		logrus.Infof("📡 [Tailscale/macOS] Brew restart result: %s (err: %v)", string(out), err)
	}

	// Wait longer for the service to actually create the socket and initialize
	logrus.Info("⏳ [Tailscale/macOS] Waiting for daemon to initialize (5s)...")
	time.Sleep(5 * time.Second)

	// 2. Now try 'tailscale up' via osascript
	// We use the same args which might include the --socket flag
	script := fmt.Sprintf("do shell script \"sudo %s %s 2>&1 || true\" with administrator privileges", tsPath, strings.Join(args, " "))
	logrus.Infof("🔐 [Tailscale/macOS] Running 'tailscale up' as root...")
	
	cmd := exec.Command("osascript", "-e", script)
	out, _ := cmd.CombinedOutput()
	output := string(out)
	
	logrus.Infof("📝 [Tailscale/macOS] Full output from 'up':\n%s", output)

	for _, line := range strings.FieldsFunc(output, func(r rune) bool { return r == '\r' || r == '\n' }) {
		if url := s.extractURL(strings.TrimSpace(line)); url != "" {
			logrus.Infof("🔗 [Tailscale/macOS] SUCCESS! Captured URL: %s", url)
			return url, nil
		}
	}
	
	if strings.Contains(output, "failed to connect") {
		logrus.Errorf("❌ [Tailscale/macOS] Still NO CONNECTION to daemon. Check if 'tailscaled' is running manually.")
	}
	
	return "", nil
}

func (s *Service) runLogoutCommand(tsPath string) error {
	// Add socket if it's homebrew
	args := []string{"logout"}
	if strings.Contains(tsPath, "homebrew") {
		if _, err := os.Stat(homebrewSocket); err == nil {
			args = append([]string{"--socket", homebrewSocket}, args...)
		}
	}
	return exec.Command(tsPath, args...).Run()
}
