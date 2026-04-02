//go:build ios || (!linux && !windows && !darwin && !android)

package service

import (
	"fmt"
	"runtime"

	"usbridge-client/internal/models"
)

type unsupportedWireGuardService struct{}

func newWireGuardService(config *models.AppConfig) WireGuardService {
	_ = config
	return &unsupportedWireGuardService{}
}

func (s *unsupportedWireGuardService) Connect(resp *models.WireGuardBootstrapResponse) error {
	_ = resp
	if runtime.GOOS == "ios" {
		return fmt.Errorf("WireGuard on iOS requires a dedicated Network Extension / Packet Tunnel integration, which is not present in this project yet")
	}
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
	if runtime.GOOS == "ios" {
		return "", fmt.Errorf("WireGuard on iOS requires a dedicated Network Extension / Packet Tunnel integration, which is not present in this project yet")
	}
	return "", fmt.Errorf("WireGuard runtime is not implemented on this platform yet")
}
func (s *unsupportedWireGuardService) GetPrivateKey() string { return "" }
func (s *unsupportedWireGuardService) SetPrivateKey(string) error {
	if runtime.GOOS == "ios" {
		return fmt.Errorf("WireGuard on iOS requires a dedicated Network Extension / Packet Tunnel integration, which is not present in this project yet")
	}
	return fmt.Errorf("WireGuard runtime is not implemented on this platform yet")
}
