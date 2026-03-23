//go:build android

package service

import (
	"fmt"

	"usbridge-client/internal/models"
)

func (s *userspaceWireGuardService) configureInterface(resp *models.WireGuardBootstrapResponse, mtu int, routeTargets []string) error {
	_ = resp
	_ = mtu
	_ = routeTargets
	return nil
}

func (s *userspaceWireGuardService) cleanupInterface() error {
	if err := stopAndroidVPNService(); err != nil {
		return fmt.Errorf("failed to stop Android VPN service: %w", err)
	}
	return nil
}
