//go:build windows

package usbpass

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InstallDrivers launches the staged usbip-win2 (USBip) installer elevated.
// We do not pnputil cezanne v1 infs and we never ship/link GPL libusbip into
// the closed broker — only the attested usbip2_ude from the official setup.
func (s *Service) InstallDrivers() error {
	dir := s.driverDir()
	setup := filepath.Join(dir, "USBip-setup.exe")
	if st, err := os.Stat(setup); err != nil || st.IsDir() {
		// Fall back to any USBip-*.exe staged next to the broker.
		matches, _ := filepath.Glob(filepath.Join(dir, "USBip*.exe"))
		if len(matches) == 0 {
			return fmt.Errorf("no USBip installer in %s — run rust-shine/vendor/usbip-win/fetch_usbip_win.ps1", dir)
		}
		setup = matches[0]
	}
	ps := fmt.Sprintf(
		"Start-Process -FilePath '%s' -Verb RunAs -Wait",
		setup,
	)
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("USBip setup: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// linuxDriverStatus is only meaningful on Linux (see driver_linux.go) --
// stubbed here so service.go's Status() can call it unconditionally
// without a build-tag switch of its own. Windows gets VhciDriver from the
// broker's own "status" control reply instead (resp["vhci_driver"]).
func (s *Service) linuxDriverStatus() (vhciPresent bool, hint string) {
	return false, ""
}

func (s *Service) driverDir() string {
	candidates := []string{
		filepath.Join(s.BrokerDir(), "usbip-win"),
		filepath.Join(s.exeDir, "usbip-win"),
		filepath.Join(s.stateDir, "usb-broker", "usbip-win"),
	}
	if env := os.Getenv("USBRIDGE_USBIP_WIN"); env != "" {
		return env
	}
	if env := os.Getenv("USBRIDGE_USBIP_WIN2"); env != "" {
		return env
	}
	for _, d := range candidates {
		if matches, _ := filepath.Glob(filepath.Join(d, "USBip*.exe")); len(matches) > 0 {
			return d
		}
		if matches, _ := filepath.Glob(filepath.Join(d, "*.inf")); len(matches) > 0 {
			return d
		}
	}
	return candidates[0]
}
