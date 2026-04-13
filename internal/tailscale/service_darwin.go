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

	// Also check PATH
	if path, err := exec.LookPath("tailscale"); err == nil {
		candidates = append(candidates, path)
	}

	// 1. Try to find a binary that is actually connected to a daemon
	for _, path := range candidates {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		cmd := exec.CommandContext(ctx, path, "status", "--json")
		err := cmd.Run()
		cancel()
		
		if err == nil {
			return path // This one works!
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
	return []string{"up", "--accept-dns=false"}
}

func (s *Service) prepareUpCommand(tsPath string, args []string) *exec.Cmd {
	return exec.Command(tsPath, args...)
}

func (s *Service) handleUpStartError(tsPath string, args []string, err error) (string, error) {
	// On macOS, if direct start fails, it might be due to lack of daemon permissions.
	// Use osascript to run with admin privileges. 
	script := fmt.Sprintf("do shell script \"%s %s 2>&1 || true\" with administrator privileges", tsPath, strings.Join(args, " "))
	logrus.Infof("🚀 [Tailscale/macOS] Requesting admin privileges via osascript...")
	
	cmd := exec.Command("osascript", "-e", script)
	out, _ := cmd.CombinedOutput()
	output := string(out)
	
	for _, line := range strings.FieldsFunc(output, func(r rune) bool { return r == '\r' || r == '\n' }) {
		if url := s.extractURL(strings.TrimSpace(line)); url != "" {
			logrus.Infof("🔗 [Tailscale/macOS] Captured URL from osascript: %s", url)
			return url, nil
		}
	}
	
	if strings.Contains(output, "failed to connect") {
		logrus.Errorf("❌ [Tailscale/macOS] Tailscale daemon is not running. Please start Tailscale.app or 'sudo brew services start tailscale'")
	} else {
		logrus.Warnf("⚠️ [Tailscale/macOS] osascript finished. Output: %s", output)
	}
	
	return "", nil
}

func (s *Service) runLogoutCommand(tsPath string) error {
	return exec.Command(tsPath, "logout").Run()
}
