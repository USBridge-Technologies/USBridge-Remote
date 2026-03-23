//go:build darwin

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
		{"ifconfig", s.ifaceName, "inet", strings.TrimSpace(resp.ClientAddress), strings.TrimSpace(resp.ServerAddress), "mtu", fmt.Sprintf("%d", mtu), "up"},
	}
	for _, routeTarget := range routeTargets {
		args, err := darwinRouteArgs("add", routeTarget, s.ifaceName)
		if err != nil {
			return err
		}
		commands = append(commands, args)
	}
	if err := runUnixCommands(commands); err != nil {
		return fmt.Errorf("failed to configure WireGuard interface on macOS (administrator privileges may be required): %w", err)
	}
	return nil
}

func (s *userspaceWireGuardService) cleanupInterface() error {
	if strings.TrimSpace(s.ifaceName) == "" {
		return nil
	}
	commands := make([][]string, 0, len(s.routeTargets)+1)
	for _, routeTarget := range s.routeTargets {
		args, err := darwinRouteArgs("delete", routeTarget, s.ifaceName)
		if err == nil {
			commands = append(commands, args)
		}
	}
	commands = append(commands, []string{"ifconfig", s.ifaceName, "down"})
	if err := runUnixCommands(commands); err != nil {
		return fmt.Errorf("failed to cleanup WireGuard interface on macOS: %w", err)
	}
	return nil
}

func darwinRouteArgs(action, routeTarget, ifaceName string) ([]string, error) {
	_, network, err := parseCIDR(routeTarget)
	if err != nil {
		return nil, fmt.Errorf("invalid WireGuard route %q: %w", routeTarget, err)
	}
	prefix := prefixLength(network)
	baseIP := network.IP.String()
	if prefix == 32 {
		return []string{"route", "-q", "-n", action, "-host", baseIP, "-interface", ifaceName}, nil
	}
	return []string{"route", "-q", "-n", action, "-net", routeTarget, "-interface", ifaceName}, nil
}

func runUnixCommands(commands [][]string) error {
	scriptPath, err := writeUnixScript(commands)
	if err != nil {
		return err
	}
	defer os.Remove(scriptPath)

	if os.Geteuid() == 0 {
		return runUnixCommand("/bin/sh", scriptPath)
	}

	helpers := [][]string{}
	if _, err := exec.LookPath("sudo"); err == nil {
		helpers = append(helpers, []string{"sudo", "/bin/sh", scriptPath})
	}
	if _, err := exec.LookPath("su"); err == nil {
		helpers = append(helpers, []string{"su", "-c", "/bin/sh " + shellEscape(scriptPath)})
	}
	if len(helpers) == 0 {
		return fmt.Errorf("administrator privileges are required and no privilege helper is available")
	}

	var lastErr error
	for _, helper := range helpers {
		if err := runUnixCommand(helper[0], helper[1:]...); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func runUnixCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeUnixScript(commands [][]string) (string, error) {
	f, err := os.CreateTemp("", "usbridge-wg-userspace-*.sh")
	if err != nil {
		return "", err
	}
	defer f.Close()

	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		b.WriteString(shellJoin(command))
		if len(command) >= 4 && command[0] == "route" && (command[3] == "delete" || command[3] == "add") {
			b.WriteString(" >/dev/null 2>&1 || true\n")
			continue
		}
		b.WriteByte('\n')
	}
	if err := os.WriteFile(f.Name(), []byte(b.String()), 0700); err != nil {
		return "", err
	}
	return f.Name(), nil
}
