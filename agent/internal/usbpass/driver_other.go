//go:build !windows && !linux

package usbpass

import "fmt"

func (s *Service) InstallDrivers() error {
	return fmt.Errorf("USB passthrough drivers are Windows/Linux-only")
}

// linuxDriverStatus is only meaningful on Linux (see driver_linux.go) --
// stubbed here so service.go's Status() can call it unconditionally
// without a build-tag switch of its own. Never actually invoked on this
// platform: Status() gates it behind runtime.GOOS == "linux".
func (s *Service) linuxDriverStatus() (vhciPresent bool, hint string) {
	return false, ""
}
