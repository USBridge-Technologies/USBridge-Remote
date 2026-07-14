//go:build darwin

package tailscale

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// Default socket path for tailscaled on macOS (Homebrew version)
	homebrewSocket = "/var/run/tailscaled.socket"
)

func (s *Service) getTailscalePath() string {
	candidates := []string{}

	// Bundled distribution: tailscale CLI next to the agent binary, i.e.
	// USBridgeAgent.app/Contents/MacOS/tailscale (see scripts/build_macos.sh).
	if exePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exePath), "tailscale"))
	}

	candidates = append(candidates,
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
		"/opt/homebrew/bin/tailscale",
		"/usr/local/bin/tailscale",
	)

	if path, err := exec.LookPath("tailscale"); err == nil {
		candidates = append(candidates, path)
	}

	// 1. Try to find a binary that is already connected to a daemon
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
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
	// Use --reset to force URL generation if flags changed
	args := []string{"up", "--accept-dns=false", "--reset"}

	// The socket file's presence is the authoritative signal that a
	// Homebrew-flavored tailscaled is running, regardless of which copy of
	// the CLI we're invoking (system Homebrew install or the one bundled
	// next to the agent binary — both need --socket to reach it).
	if _, err := os.Stat(homebrewSocket); err == nil {
		args = append([]string{"--socket", homebrewSocket}, args...)
	}
	return args
}

func (s *Service) configureCommand(cmd *exec.Cmd) {}

func (s *Service) prepareUpCommand(tsPath string, args []string) *exec.Cmd {
	return exec.Command(tsPath, args...)
}

func (s *Service) handleUpStartError(tsPath string, args []string, err error) (string, error) {
	logrus.Infof("🚀 [Tailscale/macOS] Attempting recovery for: %v", err)

	if !strings.Contains(tsPath, "Tailscale.app") {
		brewPath := "/opt/homebrew/bin/brew"
		if _, err := os.Stat("/usr/local/bin/brew"); err == nil {
			brewPath = "/usr/local/bin/brew"
		}
		
		logrus.Info("🍺 [Tailscale/macOS] Running 'sudo brew services restart tailscale'...")
		script := fmt.Sprintf("do shell script \"sudo %s services restart tailscale 2>&1\" with administrator privileges", brewPath)
		_ = exec.Command("osascript", "-e", script).Run()
		time.Sleep(3 * time.Second)
	} else {
		_ = exec.Command("open", "-a", "Tailscale").Run()
		time.Sleep(2 * time.Second)
	}

	// Final try via osascript
	script := fmt.Sprintf("do shell script \"sudo %s %s 2>&1 || true\" with administrator privileges", tsPath, strings.Join(args, " "))
	logrus.Infof("🔐 [Tailscale/macOS] Running 'tailscale up' via osascript...")
	
	cmd := exec.Command("osascript", "-e", script)
	out, _ := cmd.CombinedOutput()
	output := string(out)
	
	for _, line := range strings.FieldsFunc(output, func(r rune) bool { return r == '\r' || r == '\n' }) {
		if url := s.extractURL(line); url != "" {
			return url, nil
		}
	}
	
	return "", nil
}

func (s *Service) runLogoutCommand(tsPath string) error {
	args := []string{"logout"}
	if _, err := os.Stat(homebrewSocket); err == nil {
		args = append([]string{"--socket", homebrewSocket}, args...)
	}
	return exec.Command(tsPath, args...).Run()
}
