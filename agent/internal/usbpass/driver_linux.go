//go:build linux

package usbpass

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// linuxDriverStatus reports whether this machine can accept a virtual USB
// device via usbip's vhci-hcd: the vhci-hcd kernel module must be loaded
// (built-in on some kernels, a loadable module on most distro kernels) and
// the "usbip" userspace tool (Debian/Ubuntu package "usbip") must be on
// PATH -- the broker shells out to it the same way the Windows build shells
// out to usbip-win2's own client tools.
func (s *Service) linuxDriverStatus() (vhciPresent bool, hint string) {
	if _, err := exec.LookPath("usbip"); err != nil {
		return false, "install usbip + vhci-hcd"
	}
	if !vhciHcdLoaded() {
		return false, "install usbip + vhci-hcd"
	}
	return true, ""
}

// vhciHcdLoaded checks /sys rather than shelling out to `lsmod` -- vhci-hcd
// registers a platform driver under /sys/bus/platform/drivers regardless of
// whether it's built into the kernel or loaded as a module, so this catches
// both cases without depending on kmod/module-init-tools being installed
// just to answer this one question.
func vhciHcdLoaded() bool {
	_, err := os.Stat("/sys/bus/platform/drivers/vhci_hcd")
	return err == nil
}

// vhciModulesLoadPath is where InstallDrivers pins vhci-hcd to load on
// every future boot (see its own doc comment for why this is needed at
// all) -- same /etc/modules-load.d mechanism, and same "usbridge-" prefix
// convention, as internal/permissions' uinputModulesLoadPath.
const vhciModulesLoadPath = "/etc/modules-load.d/usbridge-vhci-hcd.conf"

// InstallDrivers installs the "usbip" package (which also pulls in the
// vhci-hcd kernel module on Debian/Ubuntu kernels that ship it as a
// loadable module rather than built-in), loads vhci-hcd immediately so
// Status() can reflect success without waiting for a reboot, and pins it
// to load on every future boot via vhciModulesLoadPath.
//
// That last step is the fix for a real bug: without it, a successful
// install only loads vhci-hcd for the current boot session (modprobe alone
// doesn't persist across a reboot) -- systemd-modules-load.service never
// knew to reload it, so vhciHcdLoaded() went back to false the next time
// the machine started, and refreshUSBPassthroughUI (see internal/ui) kept
// showing "Install USB Driver" forever after every reboot even though the
// usbip package itself (and the user's original click) was still very
// much there.
//
// Uses pkexec for the same reason the Windows path elevates via UAC
// (Start-Process -Verb RunAs, see driver_windows.go): installing a
// package, loading a kernel module, and writing to /etc both require root,
// and pkexec is what actually pops a graphical prompt on a Linux desktop
// session (unlike bare sudo, which needs a terminal).
func (s *Service) InstallDrivers() error {
	script := fmt.Sprintf(
		"apt-get install -y usbip && modprobe vhci-hcd && echo vhci-hcd > %s",
		vhciModulesLoadPath,
	)
	cmd := exec.Command("pkexec", "sh", "-c", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("usbip install: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
