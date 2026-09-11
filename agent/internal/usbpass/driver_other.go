//go:build !windows

package usbpass

import "fmt"

func (s *Service) InstallDrivers() error {
	return fmt.Errorf("USB passthrough drivers are Windows-only")
}
