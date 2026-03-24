//go:build windows

package service

import (
	"fmt"

	"golang.zx2c4.com/wireguard/conn"
	wgtun "golang.zx2c4.com/wireguard/tun"

	"usbridge-client/internal/models"
)

func (s *userspaceWireGuardService) createTUNDevice(_ *models.WireGuardBootstrapResponse, mtu int) (wgtun.Device, conn.Bind, string, error) {
	tunDevice, err := wgtun.CreateTUN(s.ifaceName, mtu)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to create WireGuard TUN interface: %w", err)
	}
	return tunDevice, conn.NewDefaultBind(), "", nil
}

func (s *userspaceWireGuardService) afterDeviceUp(_ conn.Bind, resp *models.WireGuardBootstrapResponse, mtu int, routeTargets []string) error {
	return s.configureInterface(resp, mtu, routeTargets)
}
