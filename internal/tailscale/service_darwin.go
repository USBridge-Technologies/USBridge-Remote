//go:build darwin

package tailscale

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
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
	// We append '2>&1 || true' to catch all output and ignore exit code errors in AppleScript.
	script := fmt.Sprintf("do shell script \"%s %s 2>&1 || true\" with administrator privileges", tsPath, strings.Join(args, " "))
	logrus.Infof("🚀 [Tailscale/macOS] Requesting admin privileges via osascript...")
	
	cmd := exec.Command("osascript", "-e", script)
	out, _ := cmd.CombinedOutput() // We ignore err here because we added || true in the script
	output := string(out)
	
	// osascript output can be messy (different line endings \r, \n)
	// We scan it for the login URL
	for _, line := range strings.FieldsFunc(output, func(r rune) bool { return r == '\r' || r == '\n' }) {
		if url := s.extractURL(strings.TrimSpace(line)); url != "" {
			logrus.Infof("🔗 [Tailscale/macOS] Captured URL from osascript: %s", url)
			return url, nil
		}
	}
	
	logrus.Warnf("⚠️ [Tailscale/macOS] osascript finished but no URL found. Output: %s", output)
	// We return empty string instead of error to let StartLogin continue its own checks
	return "", nil
}

func (s *Service) runLogoutCommand(tsPath string) error {
	return exec.Command(tsPath, "logout").Run()
}
