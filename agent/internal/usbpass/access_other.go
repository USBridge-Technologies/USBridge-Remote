//go:build !linux

package usbpass

// Linux-only (polkit rule for usbip; see access_linux.go). Elsewhere nothing
// needs granting.
func AttachAccessGranted() bool             { return true }
func (s *Service) GrantAttachAccess() error { return nil }
