//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"

	"usbridge-client/internal/models"
)

type linuxWireGuardService struct {
	config     *models.AppConfig
	ifaceName  string
	serverHost string
	clientHost string
	privateKey string
	running    bool
}

func newWireGuardService(config *models.AppConfig) WireGuardService {
	return &linuxWireGuardService{
		config:    config,
		ifaceName: config.WireGuardInterfaceName,
	}
}

func (s *linuxWireGuardService) GeneratePublicKey() (string, error) {
	if s.privateKey == "" {
		key, err := GenerateWireGuardPrivateKey()
		if err != nil {
			return "", err
		}
		s.privateKey = key
	}
	return WireGuardPublicKeyFromPrivate(s.privateKey)
}

func (s *linuxWireGuardService) GetPrivateKey() string {
	return s.privateKey
}

func (s *linuxWireGuardService) Connect(resp *models.WireGuardBootstrapResponse) error {
	if resp == nil {
		return fmt.Errorf("WireGuard bootstrap response is nil")
	}
	if s.privateKey == "" && strings.TrimSpace(resp.ClientPrivateKey) != "" {
		s.privateKey = strings.TrimSpace(resp.ClientPrivateKey)
	}
	if s.privateKey == "" {
		return fmt.Errorf("WireGuard private key is not generated")
	}
	if strings.TrimSpace(resp.ServerEndpointHost) == "" || resp.ServerEndpointPort <= 0 {
		return fmt.Errorf("WireGuard bootstrap returned invalid endpoint host=%q port=%d", resp.ServerEndpointHost, resp.ServerEndpointPort)
	}
	if s.ifaceName == "" {
		s.ifaceName = "usbwg0"
	}

	_ = s.Disconnect()

	mtu := resp.MTU
	if mtu <= 0 {
		mtu = 1360
	}
	routeTargets := normalizeAllowedRoutes(resp)
	commands := [][]string{
		{"ip", "link", "delete", "dev", s.ifaceName},
		{"ip", "link", "add", "dev", s.ifaceName, "type", "wireguard"},
		{"ip", "address", "replace", resp.ClientAddressCIDR, "dev", s.ifaceName},
	}
	args := []string{
		"wg", "set", s.ifaceName,
		"listen-port", strconv.Itoa(s.config.WireGuardListenPort),
		"private-key", "/dev/stdin",
		"peer", resp.ServerPublicKey,
		"endpoint", fmt.Sprintf("%s:%d", resp.ServerEndpointHost, resp.ServerEndpointPort),
		"persistent-keepalive", strconv.Itoa(resp.PersistentKeepalive),
	}
	for _, allowedIP := range resp.AllowedIPs {
		args = append(args, "allowed-ips", allowedIP)
	}
	commands = append(commands, args)
	commands = append(commands, []string{"ip", "link", "set", "dev", s.ifaceName, "mtu", strconv.Itoa(mtu), "up"})
	for _, routeTarget := range routeTargets {
		commands = append(commands, []string{"ip", "route", "replace", routeTarget, "dev", s.ifaceName, "src", resp.ClientAddress})
	}

	logrus.Infof("🔐 [WireGuard Linux] backend=%s euid=%d iface=%s", runtime.GOOS, os.Geteuid(), s.ifaceName)
	if err := s.runBatch(commands); err != nil {
		_ = s.Disconnect()
		return err
	}

	s.serverHost = resp.ServerAddress
	s.clientHost = resp.ClientAddress
	s.running = true
	return nil
}

func (s *linuxWireGuardService) Disconnect() error {
	if s.ifaceName != "" {
		if os.Geteuid() == 0 {
			_ = s.runBatch([][]string{{"ip", "link", "delete", "dev", s.ifaceName}})
		} else {
			_ = exec.Command("ip", "link", "delete", "dev", s.ifaceName).Run()
		}
	}
	s.running = false
	s.serverHost = ""
	s.clientHost = ""
	return nil
}

func (s *linuxWireGuardService) IsRunning() bool {
	return s.running
}

func (s *linuxWireGuardService) GetServerHost() string {
	return s.serverHost
}

func (s *linuxWireGuardService) GetClientHost() string {
	return s.clientHost
}

func (s *linuxWireGuardService) run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	logrus.Infof("✅ [WireGuard] %s %s", name, strings.Join(args, " "))
	return nil
}

func (s *linuxWireGuardService) runBatch(commands [][]string) error {
	scriptPath, err := s.writePrivilegedScript(commands)
	if err != nil {
		return err
	}
	defer os.Remove(scriptPath)

	if os.Geteuid() == 0 {
		logrus.Info("🔐 [WireGuard Linux] running backend script directly as root")
		cmd := exec.Command("/bin/sh", scriptPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("/bin/sh %s: %v: %s", scriptPath, err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	logrus.Info("🔐 [WireGuard Linux] elevated privileges required, attempting helper backend")
	helpers := [][]string{}
	if _, err := exec.LookPath("pkexec"); err == nil {
		helpers = append(helpers, []string{"pkexec", "/bin/sh", scriptPath})
	}
	if _, err := exec.LookPath("sudo"); err == nil {
		helpers = append(helpers, []string{"sudo", "/bin/sh", scriptPath})
	}
	if len(helpers) == 0 {
		return fmt.Errorf("wireguard on Linux requires elevated privileges; pkexec or sudo is not available")
	}

	var lastErr error
	for _, helper := range helpers {
		logrus.Infof("🔐 [WireGuard Linux] trying privileged helper: %s", strings.Join(helper, " "))
		cmd := exec.Command(helper[0], helper[1:]...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			logrus.Infof("✅ [WireGuard Linux] privileged helper succeeded: %s", helper[0])
			return nil
		}
		lastErr = fmt.Errorf("%s: %v: %s", strings.Join(helper, " "), err, strings.TrimSpace(string(out)))
		logrus.Warnf("⚠️ [WireGuard Linux] helper failed: %v", lastErr)
	}
	return lastErr
}

func (s *linuxWireGuardService) writePrivilegedScript(commands [][]string) (string, error) {
	f, err := os.CreateTemp("", "usbridge-wg-helper-*.sh")
	if err != nil {
		return "", err
	}
	defer f.Close()

	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("set -eu\n")
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		if command[0] == "ip" && len(command) >= 5 && command[1] == "link" && command[2] == "delete" {
			b.WriteString(shellJoin(command) + " >/dev/null 2>&1 || true\n")
			continue
		}
		if command[0] == "wg" && len(command) >= 3 && command[1] == "set" {
			b.WriteString("printf '%s\\n' ")
			b.WriteString(shellEscape(s.privateKey))
			b.WriteString(" | ")
			b.WriteString(shellJoin(command))
			b.WriteString("\n")
			continue
		}
		b.WriteString(shellJoin(command) + "\n")
	}
	if err := os.WriteFile(f.Name(), []byte(b.String()), 0700); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func shellJoin(command []string) string {
	parts := make([]string, 0, len(command))
	for _, arg := range command {
		parts = append(parts, shellEscape(arg))
	}
	return strings.Join(parts, " ")
}

func shellEscape(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
