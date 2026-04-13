//go:build darwin

package tailscale

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (s *Service) getTailscalePath() string {
	// 1. Homebrew paths (Apple Silicon)
	if _, err := os.Stat("/opt/homebrew/bin/tailscale"); err == nil {
		return "/opt/homebrew/bin/tailscale"
	}
	// 2. Homebrew paths (Intel)
	if _, err := os.Stat("/usr/local/bin/tailscale"); err == nil {
		return "/usr/local/bin/tailscale"
	}
	// 3. App Store path
	macosPath := "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
	if _, err := os.Stat(macosPath); err == nil {
		return macosPath
	}
	// 4. PATH
	if path, err := exec.LookPath("tailscale"); err == nil {
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
	// On macOS, if direct start fails, it might be due to lack of daemon permissions.
	// Use osascript to run with admin privileges. 
	// We append '2>&1' to catch all output from tailscale into the osascript result.
	script := fmt.Sprintf("do shell script \"%s %s 2>&1\" with administrator privileges", tsPath, strings.Join(args, " "))
	logrus.Infof("🚀 [Tailscale/macOS] Requesting admin privileges via osascript...")
	
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	output := string(out)
	
	if err != nil {
		logrus.Errorf("❌ [Tailscale/macOS] osascript failed: %v, output: %s", err, output)
		return "", fmt.Errorf("osascript tailscale up: %w", err)
	}

	// osascript output can be messy (different line endings \r, \n)
	// We scan it for the login URL
	for _, line := range strings.FieldsFunc(output, func(r rune) bool { return r == '\r' || r == '\n' }) {
		if url := s.extractURL(strings.TrimSpace(line)); url != "" {
			logrus.Infof("🔗 [Tailscale/macOS] Captured URL from osascript: %s", url)
			return url, nil
		}
	}
	
	logrus.Warnf("⚠️ [Tailscale/macOS] osascript finished but no URL found. Output: %s", output)
	return "", nil
}

func (s *Service) runLogoutCommand(tsPath string) error {
	return exec.Command(tsPath, "logout").Run()
}
