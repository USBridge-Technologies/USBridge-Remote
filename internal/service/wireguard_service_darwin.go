//go:build darwin

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"usbridge-client/internal/models"
)

type darwinWireGuardService struct {
	config       *models.AppConfig
	ifaceName    string
	serverHost   string
	clientHost   string
	serverKey    string
	keepaliveSec int
	privateKey   string
	running      bool
	routeTargets []string
}

func newWireGuardService(config *models.AppConfig) WireGuardService {
	return &darwinWireGuardService{
		config:    config,
		ifaceName: userspaceDefaultInterfaceName(config.WireGuardInterfaceName),
	}
}

func (s *darwinWireGuardService) GeneratePublicKey() (string, error) {
	if s.privateKey == "" {
		key, err := GenerateWireGuardPrivateKey()
		if err != nil {
			return "", err
		}
		s.privateKey = key
	}
	return WireGuardPublicKeyFromPrivate(s.privateKey)
}

func (s *darwinWireGuardService) GetPrivateKey() string {
	return s.privateKey
}

func (s *darwinWireGuardService) SetPrivateKey(privateKey string) error {
	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" {
		return fmt.Errorf("WireGuard private key is empty")
	}
	if _, err := WireGuardPublicKeyFromPrivate(privateKey); err != nil {
		return err
	}
	s.privateKey = privateKey
	return nil
}

func (s *darwinWireGuardService) Connect(resp *models.WireGuardBootstrapResponse) error {
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

	if s.running {
		_ = s.Disconnect()
	}

	s.ifaceName = userspaceDefaultInterfaceName(firstNonEmpty(resp.InterfaceName, s.config.WireGuardInterfaceName, s.ifaceName))
	s.routeTargets = normalizeAllowedRoutes(resp)

	client := newDarwinWireGuardHelperClient()
	if err := client.ensureRunning(); err != nil {
		return err
	}

	request := darwinWireGuardHelperRequest{
		Command: "up",
		Payload: &darwinWireGuardUpPayload{
			Bootstrap:  resp,
			PrivateKey: s.privateKey,
			ListenPort: s.config.WireGuardListenPort,
			IfaceName:  s.ifaceName,
		},
	}
	response, err := client.call(request)
	if err != nil {
		return err
	}

	if strings.TrimSpace(response.IfaceName) != "" {
		s.ifaceName = strings.TrimSpace(response.IfaceName)
	}
	s.serverHost = resp.ServerAddress
	s.clientHost = resp.ClientAddress
	s.serverKey = strings.TrimSpace(resp.ServerPublicKey)
	s.keepaliveSec = resp.PersistentKeepalive
	s.running = true
	logrus.Infof("🔐 [WireGuard darwin] helper backend started iface=%s client=%s server=%s", s.ifaceName, s.clientHost, s.serverHost)
	return nil
}

func (s *darwinWireGuardService) Disconnect() error {
	client := newDarwinWireGuardHelperClient()
	_, err := client.call(darwinWireGuardHelperRequest{Command: "down"})
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such file or directory") {
		return err
	}

	s.running = false
	s.serverHost = ""
	s.clientHost = ""
	s.serverKey = ""
	s.keepaliveSec = 0
	s.routeTargets = nil
	return nil
}

func (s *darwinWireGuardService) IsRunning() bool {
	return s.running
}

func (s *darwinWireGuardService) GetServerHost() string {
	return s.serverHost
}

func (s *darwinWireGuardService) GetClientHost() string {
	return s.clientHost
}

func (s *darwinWireGuardService) GetPeerStatus() (*WireGuardPeerStatus, error) {
	response, err := newDarwinWireGuardHelperClient().call(darwinWireGuardHelperRequest{Command: "status"})
	if err != nil {
		return nil, err
	}
	return response.PeerStatus(s.serverKey, s.keepaliveSec), nil
}

type darwinWireGuardHelperClient struct {
	socketPath string
	logPath    string
	helperPath string
}

func newDarwinWireGuardHelperClient() *darwinWireGuardHelperClient {
	uid := os.Getuid()
	return &darwinWireGuardHelperClient{
		socketPath: filepath.Join("/tmp", fmt.Sprintf("usbridge-wg-helper-%d.sock", uid)),
		logPath:    filepath.Join("/tmp", fmt.Sprintf("usbridge-wg-helper-%d.log", uid)),
	}
}

func (c *darwinWireGuardHelperClient) ensureRunning() error {
	if _, err := c.call(darwinWireGuardHelperRequest{Command: "ping"}); err == nil {
		return nil
	}

	helperPath, err := c.resolveHelperPath()
	if err != nil {
		return err
	}
	c.helperPath = helperPath

	command := shellJoin([]string{
		helperPath,
		"serve",
		"--socket", c.socketPath,
		"--log", c.logPath,
	})
	command += " >/dev/null 2>&1 &"
	applescript := fmt.Sprintf("do shell script %s with administrator privileges", strconv.Quote(command))

	if _, err := runDarwinHelperCommand("osascript", "-e", applescript); err != nil {
		return fmt.Errorf("failed to start WireGuard helper with administrator privileges: %w", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.call(darwinWireGuardHelperRequest{Command: "ping"}); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("WireGuard helper did not start in time; check %s", c.logPath)
}

func (c *darwinWireGuardHelperClient) resolveHelperPath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve application executable path: %w", err)
	}

	exeDir := filepath.Dir(exePath)
	candidates := []string{
		filepath.Join(exeDir, "..", "Resources", "USBridgeWireGuardHelper"),
		filepath.Join(exeDir, "USBridgeWireGuardHelper"),
	}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("WireGuard helper binary was not found next to the macOS app bundle; rebuild with scripts/build_macos.sh")
}
