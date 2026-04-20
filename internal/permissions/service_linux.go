//go:build linux

package permissions

import (
	"github.com/sirupsen/logrus"
	"usbridge_agent/internal/capture"
)

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) AccessibilityGranted() bool {
	return true 
}

func (s *Service) ScreenRecordingGranted() bool {
	if capture.GetLinuxEnv() == "Wayland" {
		return capture.GetPortalSession() != ""
	}
	return true
}

func (s *Service) RequestAccessibility() bool {
	return true
}

func (s *Service) RequestScreenRecording() bool {
	if capture.GetLinuxEnv() == "Wayland" {
		err := capture.InitPortalSession()
		if err != nil {
			logrus.Errorf("Failed to initiate Wayland portal: %v", err)
		}
		return true
	}
	return true
}

func (s *Service) RequestMissing()            {}
func (s *Service) OpenPrivacySettings() error { return nil }
