//go:build !linux

package service

import (
	"fmt"

	"usbridge-client/internal/models"
)

type unsupportedWireGuardService struct{}

func newWireGuardService(config *models.AppConfig) WireGuardService {
	_ = config
	return &unsupportedWireGuardService{}
}

func (s *unsupportedWireGuardService) Connect(resp *models.WireGuardBootstrapResponse) error {
	_ = resp
	return fmt.Errorf("WireGuard runtime is not implemented on this platform yet")
}

func (s *unsupportedWireGuardService) Disconnect() error { return nil }
func (s *unsupportedWireGuardService) IsRunning() bool   { return false }
func (s *unsupportedWireGuardService) GetServerHost() string {
	return ""
}
func (s *unsupportedWireGuardService) GetClientHost() string {
	return ""
}
func (s *unsupportedWireGuardService) GeneratePublicKey() (string, error) {
	return "", fmt.Errorf("WireGuard runtime is not implemented on this platform yet")
}
func (s *unsupportedWireGuardService) GetPrivateKey() string { return "" }
