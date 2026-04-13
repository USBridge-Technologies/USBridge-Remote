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

func (s *Service) getTailscalePath() string {
	candidates := []string{
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
		"/opt/homebrew/bin/tailscale",
		"/usr/local/bin/tailscale",
	}

	if path, err := exec.LookPath("tailscale"); err == nil {
		candidates = append(candidates, path)
	}

	// Try to find a binary that is actually connected to a daemon
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			err := exec.CommandContext(ctx, path, "status", "--json").Run()
			cancel()
			if err == nil {
				return path
			}
		}
	}

	// Fallback: return the first one that exists
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
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
	logrus.Infof("🚀 [Tailscale/macOS] Direct start failed or daemon not responding. Attempting to start daemon...")

	// 1. Try to start the daemon depending on installation type
	if strings.Contains(tsPath, "Tailscale.app") {
		logrus.Info("📦 [Tailscale/macOS] Starting Tailscale.app...")
		_ = exec.Command("open", "-a", "Tailscale").Run()
	} else {
		logrus.Info("🍺 [Tailscale/macOS] Attempting to start Homebrew tailscale service...")
		// Use osascript to start brew service or tailscaled
		brewPath := "/opt/homebrew/bin/brew"
		if _, err := os.Stat("/usr/local/bin/brew"); err == nil {
			brewPath = "/usr/local/bin/brew"
		}
		
		script := fmt.Sprintf("do shell script \"%s services start tailscale || %s 2>&1 &\" with administrator privileges", brewPath, tsPath)
		_ = exec.Command("osascript", "-e", script).Run()
	}

	// Give it some time to wake up
	time.Sleep(3 * time.Second)

	// 2. Now try 'tailscale up' via osascript
	script := fmt.Sprintf("do shell script \"%s %s 2>&1 || true\" with administrator privileges", tsPath, strings.Join(args, " "))
	logrus.Infof("🔐 [Tailscale/macOS] Requesting privileges for login flow...")
	
	cmd := exec.Command("osascript", "-e", script)
	out, _ := cmd.CombinedOutput()
	output := string(out)
	
	for _, line := range strings.FieldsFunc(output, func(r rune) bool { return r == '\r' || r == '\n' }) {
		if url := s.extractURL(strings.TrimSpace(line)); url != "" {
			logrus.Infof("🔗 [Tailscale/macOS] Captured URL: %s", url)
			return url, nil
		}
	}
	
	if strings.Contains(output, "failed to connect") {
		logrus.Errorf("❌ [Tailscale/macOS] Still cannot connect to Tailscale daemon after start attempt.")
	} else {
		logrus.Warnf("⚠️ [Tailscale/macOS] Login flow finished. Output: %s", output)
	}
	
	return "", nil
}

func (s *Service) runLogoutCommand(tsPath string) error {
	return exec.Command(tsPath, "logout").Run()
}
