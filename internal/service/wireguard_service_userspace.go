//go:build windows || android

package service

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"
	wgdevice "golang.zx2c4.com/wireguard/device"
	wgtun "golang.zx2c4.com/wireguard/tun"

	"usbridge-client/internal/models"
)

type userspaceWireGuardService struct {
	config       *models.AppConfig
	ifaceName    string
	serverHost   string
	clientHost   string
	privateKey   string
	running      bool
	tunDevice    wgtun.Device
	device       *wgdevice.Device
	routeTargets []string
}

func newWireGuardService(config *models.AppConfig) WireGuardService {
	return &userspaceWireGuardService{
		config:    config,
		ifaceName: userspaceDefaultInterfaceName(config.WireGuardInterfaceName),
	}
}

func (s *userspaceWireGuardService) GeneratePublicKey() (string, error) {
	if s.privateKey == "" {
		key, err := GenerateWireGuardPrivateKey()
		if err != nil {
			return "", err
		}
		s.privateKey = key
	}
	return WireGuardPublicKeyFromPrivate(s.privateKey)
}

func (s *userspaceWireGuardService) GetPrivateKey() string {
	return s.privateKey
}

func (s *userspaceWireGuardService) Connect(resp *models.WireGuardBootstrapResponse) error {
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

	if s.running || s.device != nil || s.tunDevice != nil {
		_ = s.Disconnect()
	}

	mtu := resp.MTU
	if mtu <= 0 {
		mtu = 1360
	}
	s.ifaceName = userspaceDefaultInterfaceName(firstNonEmpty(resp.InterfaceName, s.config.WireGuardInterfaceName, s.ifaceName))
	s.routeTargets = normalizeAllowedRoutes(resp)

	if err := ensureWireGuardRuntimeAvailable(); err != nil {
		return err
	}

	tunDevice, bind, ifaceName, err := s.createTUNDevice(resp, mtu)
	if err != nil {
		return err
	}
	if strings.TrimSpace(ifaceName) != "" {
		s.ifaceName = ifaceName
	} else {
		realIfaceName, err := tunDevice.Name()
		if err == nil && strings.TrimSpace(realIfaceName) != "" {
			s.ifaceName = realIfaceName
		}
	}

	logger := &wgdevice.Logger{
		Verbosef: func(format string, args ...any) {
			logrus.Debugf("[WireGuard %s] "+format, append([]any{runtime.GOOS}, args...)...)
		},
		Errorf: func(format string, args ...any) {
			logrus.Errorf("[WireGuard %s] "+format, append([]any{runtime.GOOS}, args...)...)
		},
	}

	dev := wgdevice.NewDevice(tunDevice, bind, logger)
	ipcConfig, err := wireGuardIPCConfig(resp, s.privateKey, s.config.WireGuardListenPort)
	if err != nil {
		bind.Close()
		tunDevice.Close()
		return err
	}
	if err := dev.IpcSet(ipcConfig); err != nil {
		dev.Close()
		tunDevice.Close()
		return fmt.Errorf("failed to configure userspace WireGuard device: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		tunDevice.Close()
		return fmt.Errorf("failed to start userspace WireGuard device: %w", err)
	}
	if err := s.afterDeviceUp(bind, resp, mtu, s.routeTargets); err != nil {
		dev.Close()
		tunDevice.Close()
		return err
	}

	s.tunDevice = tunDevice
	s.device = dev
	s.serverHost = resp.ServerAddress
	s.clientHost = resp.ClientAddress
	s.running = true

	logrus.Infof("🔐 [WireGuard %s] userspace backend started iface=%s client=%s server=%s", runtime.GOOS, s.ifaceName, s.clientHost, s.serverHost)
	return nil
}

func (s *userspaceWireGuardService) Disconnect() error {
	var errs []string

	if err := s.cleanupInterface(); err != nil {
		errs = append(errs, err.Error())
	}
	if s.device != nil {
		s.device.Close()
		s.device = nil
	}
	if s.tunDevice != nil {
		if err := s.tunDevice.Close(); err != nil && err != os.ErrClosed {
			errs = append(errs, err.Error())
		}
		s.tunDevice = nil
	}

	s.running = false
	s.serverHost = ""
	s.clientHost = ""
	s.routeTargets = nil

	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}

func (s *userspaceWireGuardService) IsRunning() bool {
	return s.running
}

func (s *userspaceWireGuardService) GetServerHost() string {
	return s.serverHost
}

func (s *userspaceWireGuardService) GetClientHost() string {
	return s.clientHost
}
