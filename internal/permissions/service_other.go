//go:build !darwin && !linux

package permissions

type Service struct{}

func New() *Service                           { return &Service{} }
func (s *Service) AccessibilityGranted() bool { return true }
func (s *Service) ScreenRecordingGranted() bool {
	return true
}
func (s *Service) RequestAccessibility() bool { return true }
func (s *Service) RequestScreenRecording() bool {
	return true
}
func (s *Service) RequestMissing()                       {}
func (s *Service) OpenPrivacySettings() error            { return nil }
func (s *Service) KMSCaptureGranted(binPath string) bool { return false }
func (s *Service) RequestKMSCapture(binPath string) bool { return false }
