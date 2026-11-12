//go:build !linux

package permissions

import "errors"

// StreamerLauncherVerify is Linux-only (the KMS-capture launcher, see
// streamer_launcher_linux.go).
func (s *Service) StreamerLauncherVerify(bundleDir string) (string, error) {
	return "", errors.New("streamer launcher is Linux-only")
}

// SunshineLaunchReady is Linux-only, see streamer_launcher_linux.go.
func (s *Service) SunshineLaunchReady() bool { return false }

// VibepolloLaunchReady is Linux-only, see streamer_launcher_linux.go.
func (s *Service) VibepolloLaunchReady() bool { return false }

// EnsureTreeKMS is Linux-only, see streamer_launcher_linux.go.
func (s *Service) EnsureTreeKMS(target string) bool { return false }

// HasFileCapability is Linux-only, see streamer_launcher_linux.go.
func HasFileCapability(p string) bool { return false }

// PriorityGranted is Linux-only.
func (s *Service) PriorityGranted() bool { return true }

// RequestPriority is Linux-only.
func (s *Service) RequestPriority() bool { return true }

