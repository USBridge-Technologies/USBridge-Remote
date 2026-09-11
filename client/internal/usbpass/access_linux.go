//go:build linux

package usbpass

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	usbGroupName = "usbridge-usb"
	usbRulePath  = "/etc/udev/rules.d/99-usbridge-usb.rules"
)

// Same shape as the agent's uinput rule: scoped GROUP+MODE=0660 (not world
// writable), plus TAG+=uaccess so an interactive desktop session gets an
// immediate logind ACL without waiting for the next login to pick up the
// new supplementary group.
const usbRuleContent = `SUBSYSTEM=="usb", ENV{DEVTYPE}=="usb_device", GROUP="` + usbGroupName + `", MODE="0660", TAG+="uaccess"` + "\n"

var lastUSBAccessErr string

// LastUSBAccessError is the human-readable reason the last RequestUSBAccess
// call failed, or "" if it succeeded / hasn't run.
func LastUSBAccessError() string { return lastUSBAccessErr }

func shellQuote(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	return `"` + replacer.Replace(s) + `"`
}

// buildUSBGrantScript installs the persistent udev rule, adds the user to
// usbridge-usb, applies live chgrp/chmod + setfacl on /dev/bus/usb nodes,
// and unbinds kernel drivers for the given Linux busids (e.g. "2-3") so
// libusb can claim without CAP_SYS_ADMIN USBDEVFS_DISCONNECT.
func buildUSBGrantScript(rulePath, currentUser string, busIDs []string) string {
	var unbind strings.Builder
	for _, id := range busIDs {
		id = strings.TrimSpace(id)
		// Allow Linux busids like "2-3" / "1-1.2"; reject shell metacharacters.
		if id == "" || strings.ContainsAny(id, " \t\n;'\"`$\\/|&()<>") {
			continue
		}
		// Unbind every interface under this device from its kernel driver
		// (usb-storage, hid, etc.). Safe for passthrough: we are about to
		// claim the device ourselves.
		unbind.WriteString(fmt.Sprintf(
			`for iface in /sys/bus/usb/devices/%[1]s:*; do `+
				`[ -e "$iface/driver" ] || continue; `+
				`drv=$(basename "$(readlink -f "$iface/driver")"); `+
				`echo -n "$(basename "$iface")" > "/sys/bus/usb/drivers/$drv/unbind" 2>/dev/null || true; `+
				`done; `,
			id,
		))
	}

	return fmt.Sprintf(
		"(getent group %[3]s >/dev/null || groupadd %[3]s) && usermod -aG %[3]s %[4]s && "+
			"install -m 0644 %[1]s %[2]s && "+
			"udevadm control --reload-rules && udevadm trigger --subsystem-match=usb; "+
			"STATUS=$?; "+
			"for d in /dev/bus/usb/*/*; do "+
			"[ -e \"$d\" ] || continue; "+
			"chgrp %[3]s \"$d\" 2>/dev/null || true; "+
			"chmod 0660 \"$d\" 2>/dev/null || true; "+
			"command -v setfacl >/dev/null 2>&1 && setfacl -m u:%[4]s:rw- \"$d\" 2>/dev/null || true; "+
			"done; "+
			"%[5]s"+
			"exit $STATUS",
		rulePath, usbRulePath, usbGroupName, currentUser, unbind.String(),
	)
}

func usbRuleUpToDate() bool {
	data, err := os.ReadFile(usbRulePath)
	if err != nil {
		return false
	}
	return string(data) == usbRuleContent
}

// canOpenUSBDevice reports whether this process can O_RDWR the usbfs node
// for busnum/devnum (as libusb does). Missing nodes return false.
func canOpenUSBDevice(busnum, devnum uint32) bool {
	if busnum == 0 || devnum == 0 {
		return false
	}
	path := fmt.Sprintf("/dev/bus/usb/%03d/%03d", busnum, devnum)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// USBAccessGranted is true when the persistent udev rule is installed and
// every requested busnum/devnum is openable O_RDWR. Empty list checks the
// rule only (used by probes that don't yet know which stick to mount).
func USBAccessGranted(devs []usbDevRef) bool {
	if !usbRuleUpToDate() {
		return false
	}
	if len(devs) == 0 {
		return true
	}
	for _, d := range devs {
		if !canOpenUSBDevice(d.Busnum, d.Devnum) {
			return false
		}
	}
	return true
}

type usbDevRef struct {
	BusID  string
	Busnum uint32
	Devnum uint32
}

func friendlyPkexecError(err error, out []byte) string {
	s := string(out)
	switch {
	case strings.Contains(s, "No authentication agent found"),
		strings.Contains(s, "Authentication agent refused"):
		return "no polkit authentication agent is running for this session " +
			"(pkexec needs one to prompt for the password). Log into a full desktop " +
			"session and make sure its polkit agent is running, then retry."
	case err != nil && strings.Contains(err.Error(), "exit status 126"):
		return "authentication was cancelled or dismissed. Approve the prompt and try again."
	case err != nil:
		trimmed := strings.TrimSpace(s)
		if trimmed != "" {
			return fmt.Sprintf("pkexec failed: %v (%s)", err, trimmed)
		}
		return fmt.Sprintf("pkexec failed: %v", err)
	default:
		return strings.TrimSpace(s)
	}
}

func kernelDriverBound(busID string) bool {
	busID = strings.TrimSpace(busID)
	if busID == "" || strings.ContainsAny(busID, " \t\n;'\"`$\\/|&()<>") {
		return false
	}
	matches, err := filepath.Glob(filepath.Join("/sys/bus/usb/devices", busID+":*"))
	if err != nil {
		return false
	}
	for _, iface := range matches {
		if _, err := os.Stat(filepath.Join(iface, "driver")); err == nil {
			return true
		}
	}
	return false
}

// RequestUSBAccess installs the usbridge-usb udev rule via pkexec (same
// pattern as the agent's RequestAccessibility for /dev/uinput) and unbinds
// kernel drivers for the given Linux busids so libusb can claim them.
func RequestUSBAccess(devs []usbDevRef) bool {
	lastUSBAccessErr = ""

	if _, err := exec.LookPath("pkexec"); err != nil {
		lastUSBAccessErr = "pkexec is not installed. Install it and try again:\n" +
			"  su -c 'apt install pkexec'\n" +
			"(on Debian, pkexec ships in its own package)"
		logrus.Warnf("usbpass: %s", lastUSBAccessErr)
		return false
	}

	tmp, err := os.CreateTemp("", "usbridge-usb-*.rules")
	if err != nil {
		lastUSBAccessErr = fmt.Sprintf("could not create temp udev rule: %v", err)
		return false
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(usbRuleContent); err != nil {
		_ = tmp.Close()
		lastUSBAccessErr = fmt.Sprintf("could not write temp udev rule: %v", err)
		return false
	}
	_ = tmp.Close()

	currentUser := "$(whoami)"
	if u, err := user.Current(); err == nil && u.Username != "" {
		currentUser = shellQuote(u.Username)
	}

	var busIDs []string
	for _, d := range devs {
		if d.BusID != "" {
			busIDs = append(busIDs, d.BusID)
		}
	}

	script := buildUSBGrantScript(tmp.Name(), currentUser, busIDs)
	logrus.Infof("usbpass: requesting USB device access via pkexec (rule=%s busids=%v)", usbRulePath, busIDs)
	cmd := exec.Command("pkexec", "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	logrus.Infof("usbpass: pkexec usb-access exit=%v output=%q", err, string(out))
	if err != nil {
		lastUSBAccessErr = friendlyPkexecError(err, out)
		return false
	}

	time.Sleep(300 * time.Millisecond)
	if !usbRuleUpToDate() {
		lastUSBAccessErr = "udev rule was installed but is not readable; try again or check /etc/udev/rules.d"
		return false
	}
	for _, d := range devs {
		if d.Busnum == 0 || d.Devnum == 0 {
			continue
		}
		if !canOpenUSBDevice(d.Busnum, d.Devnum) {
			lastUSBAccessErr = fmt.Sprintf(
				"udev rule installed but %s is still inaccessible; unplug/replug the device or log out and back in",
				filepath.Join(fmt.Sprintf("/dev/bus/usb/%03d/%03d", d.Busnum, d.Devnum)),
			)
			return false
		}
	}
	return true
}

// usbAccessNeeded reports whether usbfs permissions still need a privileged
// udev grant before libusb can even open the device node. Kernel-driver
// unbind is NOT required here: SetAutoDetach / a later RequestUSBAccess on
// claim failure handles that, so we don't pop pkexec on every remount while
// the rule is already installed.
func usbAccessNeeded(devs []usbDevRef) bool {
	if !usbRuleUpToDate() {
		return true
	}
	for _, d := range devs {
		if d.Busnum != 0 && d.Devnum != 0 && !canOpenUSBDevice(d.Busnum, d.Devnum) {
			return true
		}
	}
	return false
}

// EnsureUSBAccess grants Linux usbfs permissions (and unbinds kernel
// drivers) when needed. No-op when the udev rule is in place, usbfs nodes
// are openable, and no kernel driver still owns the interfaces. Call
// before TryClaimGousb.
func EnsureUSBAccess(devs []usbDevRef) error {
	if !usbAccessNeeded(devs) {
		return nil
	}
	if RequestUSBAccess(devs) {
		return nil
	}
	if lastUSBAccessErr != "" {
		return fmt.Errorf("%s", lastUSBAccessErr)
	}
	return fmt.Errorf("USB device access was not granted")
}
