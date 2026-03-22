//go:build android

package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"usbridge-client/internal/models"
)

func (s *userspaceWireGuardService) configureInterface(resp *models.WireGuardBootstrapResponse, mtu int, routeTargets []string) error {
	commands := [][]string{
		{"ip", "link", "set", "dev", s.ifaceName, "mtu", fmt.Sprintf("%d", mtu), "up"},
		{"ip", "address", "replace", resp.ClientAddressCIDR, "dev", s.ifaceName},
	}
	for _, routeTarget := range routeTargets {
		commands = append(commands, []string{"ip", "route", "replace", routeTarget, "dev", s.ifaceName, "src", resp.ClientAddress})
	}
	if err := runAndroidCommands(commands); err != nil {
		return fmt.Errorf("failed to configure WireGuard interface on Android (root or a dedicated VpnService integration is required): %w", err)
	}
	return nil
}

func (s *userspaceWireGuardService) cleanupInterface() error {
	if strings.TrimSpace(s.ifaceName) == "" {
		return nil
	}
	if err := runAndroidCommands([][]string{{"ip", "link", "delete", "dev", s.ifaceName}}); err != nil {
		return fmt.Errorf("failed to cleanup WireGuard interface on Android: %w", err)
	}
	return nil
}

func runAndroidCommands(commands [][]string) error {
	scriptPath, err := writeAndroidScript(commands)
	if err != nil {
		return err
	}
	defer os.Remove(scriptPath)

	helpers := [][]string{
		{"/system/bin/sh", scriptPath},
	}
	if _, err := exec.LookPath("su"); err == nil {
		helpers = append([][]string{{"su", "-c", "/system/bin/sh " + shellEscape(scriptPath)}}, helpers...)
	}

	var lastErr error
	for _, helper := range helpers {
		if err := runAndroidCommand(helper[0], helper[1:]...); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func runAndroidCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeAndroidScript(commands [][]string) (string, error) {
	f, err := os.CreateTemp("", "usbridge-wg-android-*.sh")
	if err != nil {
		return "", err
	}
	defer f.Close()

	var b strings.Builder
	b.WriteString("#!/system/bin/sh\n")
	b.WriteString("set -eu\n")
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		if len(command) >= 5 && command[0] == "ip" && command[1] == "link" && command[2] == "delete" {
			b.WriteString(shellJoin(command) + " >/dev/null 2>&1 || true\n")
			continue
		}
		b.WriteString(shellJoin(command) + "\n")
	}
	if err := os.WriteFile(f.Name(), []byte(b.String()), 0700); err != nil {
		return "", err
	}
	return f.Name(), nil
}
