//go:build windows

package service

import (
	"fmt"
	"os/exec"
	"strings"

	"usbridge-client/internal/models"
)

func (s *userspaceWireGuardService) configureInterface(resp *models.WireGuardBootstrapResponse, mtu int, routeTargets []string) error {
	ip, network, err := parseCIDR(resp.ClientAddressCIDR)
	if err != nil {
		return fmt.Errorf("failed to parse client WireGuard address: %w", err)
	}
	prefix := prefixLength(network)

	script := []string{
		"$ErrorActionPreference = 'Stop'",
		fmt.Sprintf("$alias = '%s'", psQuote(s.ifaceName)),
		fmt.Sprintf("$ip = '%s'", psQuote(ip.String())),
		fmt.Sprintf("$prefix = %d", prefix),
		"Get-NetIPAddress -InterfaceAlias $alias -AddressFamily IPv4 -ErrorAction SilentlyContinue | Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue",
		"New-NetIPAddress -InterfaceAlias $alias -IPAddress $ip -PrefixLength $prefix -Type Unicast -PolicyStore ActiveStore | Out-Null",
	}
	if mtu > 0 {
		script = append(script, fmt.Sprintf("Set-NetIPInterface -InterfaceAlias $alias -NlMtuBytes %d -ErrorAction SilentlyContinue | Out-Null", mtu))
	}
	if len(routeTargets) > 0 {
		script = append(script, fmt.Sprintf("$routes = @(%s)", psStringArray(routeTargets)))
		script = append(script,
			"foreach ($route in $routes) {",
			"  Get-NetRoute -InterfaceAlias $alias -DestinationPrefix $route -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue",
			"  New-NetRoute -InterfaceAlias $alias -DestinationPrefix $route -NextHop '0.0.0.0' -PolicyStore ActiveStore | Out-Null",
			"}",
		)
	}

	if err := runPowerShell(script...); err != nil {
		return fmt.Errorf("failed to configure WireGuard interface on Windows (administrator privileges may be required): %w", err)
	}
	return nil
}

func (s *userspaceWireGuardService) cleanupInterface() error {
	if strings.TrimSpace(s.ifaceName) == "" {
		return nil
	}
	script := []string{
		"$ErrorActionPreference = 'Stop'",
		fmt.Sprintf("$alias = '%s'", psQuote(s.ifaceName)),
	}
	if len(s.routeTargets) > 0 {
		script = append(script, fmt.Sprintf("$routes = @(%s)", psStringArray(s.routeTargets)))
		script = append(script,
			"foreach ($route in $routes) {",
			"  Get-NetRoute -InterfaceAlias $alias -DestinationPrefix $route -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue",
			"}",
		)
	}
	script = append(script,
		"Get-NetIPAddress -InterfaceAlias $alias -AddressFamily IPv4 -ErrorAction SilentlyContinue | Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue",
	)
	if err := runPowerShell(script...); err != nil {
		return fmt.Errorf("failed to cleanup WireGuard interface on Windows: %w", err)
	}
	return nil
}

func runPowerShell(lines ...string) error {
	script := strings.Join(lines, "\n")
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func psQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func psStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("'%s'", psQuote(value)))
	}
	return strings.Join(quoted, ", ")
}
