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
	// On macOS, if it's not running or needs sudo, we use osascript.
	// We use '2>&1' to make sure all output is captured.
	script := fmt.Sprintf("do shell script \"%s %s 2>&1\" with administrator privileges", tsPath, strings.Join(args, " "))
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("osascript tailscale up: %w (output: %s)", err, string(out))
	}

	// Try to find URL in output
	output := string(out)
	for _, line := range strings.Split(output, "\r") { // osascript output can use \r
		cleanLine := strings.TrimSpace(line)
		if strings.Contains(cleanLine, "https://login.tailscale.com") {
			for _, w := range strings.Fields(cleanLine) {
				if strings.HasPrefix(w, "https://") {
					return w, nil
				}
			}
		}
	}
	return "", nil
}

func (s *Service) runLogoutCommand(tsPath string) error {
	return exec.Command(tsPath, "logout").Run()
}
