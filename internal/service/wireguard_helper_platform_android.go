//go:build android

package service

import (
	"fmt"

	"usbridge-client/internal/models"
)

func platformUsesWireGuardHelper() bool {
	return false
}

func (s *userspaceWireGuardService) connectWithElevatedWireGuardHelper(_ *models.WireGuardBootstrapResponse, _ int) error {
	return fmt.Errorf("elevated WireGuard helper is not available on android")
}

func (s *userspaceWireGuardService) disconnectWithElevatedWireGuardHelper() error {
	return nil
}
