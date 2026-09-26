//go:build windows

package usbpass

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InstallDrivers launches the staged usbip-win2 (USBip) installer elevated,
// then (same elevation, no second UAC prompt) reserves the broker's own
// ports at the TCP/IP stack level so nothing else on the machine can ever
// steal them out from under it again -- see reservePortsScript's own doc
// comment for why this exists at all.
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
	// One elevated child runs the installer, then the port reservations, so
	// this is still exactly one UAC prompt total -- a second elevate-only-
	// for-netsh call later (once unprivileged) would just silently fail
	// (Access is denied) instead, invisibly, and that's exactly the kind of
	// "why didn't this take effect" this whole change exists to avoid.
	ps := fmt.Sprintf(
		"Start-Process -FilePath '%s' -Verb RunAs -Wait; %s",
		setup, reservePortsScript(s.urbPort, s.controlAddr),
	)
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("USBip setup: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// reservePortsScript returns the PowerShell fragment InstallDrivers appends
// (same elevated Start-Process invocation, no extra UAC prompt) to reserve
// the broker's URB and control-plane ports as excluded port ranges --
// confirmed live: on a Windows test machine, WsToastNotification.exe (an
// unrelated system process) raced the broker for its hardcoded URB port
// (8090) on *every single boot*, winning often enough that the broker's own
// bind() failed and it exited immediately, silently, for over an hour, with
// nothing between "does the client have a license" and "is the OS routing
// USB traffic right" anywhere in that chain even hinting that the broker
// itself was simply never running. `netsh add excludedportrange` reserves a
// port at the TCP/IP stack level -- nothing else, including a process that
// starts before this one at the next boot, can bind it afterward -- which
// is the only fix that actually prevents this race rather than just
// recovering from it faster (see usbBrokerWatchdog in agent/internal/app
// for that half). Idempotent: `netsh ... add ...` on an already-reserved
// range fails harmlessly ("The exclusion range is already present" /
// similar), and its exit code is deliberately ignored here for exactly that
// reason -- this is a best-effort hardening step, not something worth
// failing the whole driver install over.
func reservePortsScript(urbPort int, controlAddr string) string {
	controlPort := controlAddr
	if i := strings.LastIndex(controlAddr, ":"); i >= 0 {
		controlPort = controlAddr[i+1:]
	}
	return fmt.Sprintf(
		"netsh int ipv4 add excludedportrange protocol=tcp startport=%d numberofports=1 store=persistent | Out-Null; "+
			"netsh int ipv4 add excludedportrange protocol=tcp startport=%s numberofports=1 store=persistent | Out-Null",
		urbPort, controlPort,
	)
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
