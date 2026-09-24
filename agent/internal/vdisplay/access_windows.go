//go:build windows

package vdisplay

import (
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// On Windows the virtual monitor comes from VirtualDrivers' MttVDD driver
// (https://github.com/VirtualDrivers/Virtual-Display-Driver), which the
// rust-shine streamer attaches/detaches itself -- no elevation needed at
// stream time, only once to install the driver. So here "access" means "the
// driver is installed", and granting it runs the install (one UAC prompt).

// mttvddHardwareID is the hardware id install-mttvdd.ps1 creates the device
// node with, and what EnumDisplayDevicesW reports as its sources' DeviceID.
const mttvddHardwareID = `Root\MttVDD`

// installScript is rust-shine's scripts/install-mttvdd.ps1 (keep the two
// copies identical): downloads the pinned MttVDD release, checks its
// SignPath signature, trusts that one publisher, writes vdd_settings.xml,
// creates the device node -- and, if the device already exists but is
// stopped, just restarts it.
//
//go:embed install-mttvdd.ps1
var installScript string

// installPollTimeout: how long InstallDriver waits, after the elevated
// script exits 0, for the new monitor to show up in EnumDisplayDevicesW.
const installPollTimeout = 15 * time.Second

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procEnumDisplayDevicesW = user32.NewProc("EnumDisplayDevicesW")
)

type displayDeviceW struct {
	cb           uint32
	deviceName   [32]uint16
	deviceString [128]uint16
	stateFlags   uint32
	deviceID     [128]uint16
	deviceKey    [128]uint16
}

// displayDeviceIDs lists every GDI display source's DeviceID (the adapter's
// first hardware id, e.g. `Root\MttVDD` or `PCI\VEN_10DE&...`).
func displayDeviceIDs() []string {
	var ids []string
	for i := uint32(0); ; i++ {
		dd := displayDeviceW{}
		dd.cb = uint32(unsafe.Sizeof(dd))
		ok, _, _ := procEnumDisplayDevicesW.Call(0, uintptr(i), uintptr(unsafe.Pointer(&dd)), 0)
		if ok == 0 {
			return ids
		}
		ids = append(ids, syscall.UTF16ToString(dd.deviceID[:]))
	}
}

// hasMttVDD reports whether any of ids is MttVDD's adapter. A device that
// is installed but stopped has no GDI sources, so reads as absent -- which
// is right: the install button's script restarts it.
func hasMttVDD(ids []string) bool {
	for _, id := range ids {
		if strings.EqualFold(id, mttvddHardwareID) {
			return true
		}
	}
	return false
}

// DriverInstalled reports whether MttVDD's adapter is present and running.
func DriverInstalled() bool { return hasMttVDD(displayDeviceIDs()) }

// AccessGranted: nothing to grant beyond having the driver.
func AccessGranted() bool { return DriverInstalled() }

// GrantAccess installs (or repairs) the driver.
func GrantAccess() error { return InstallDriver() }

// Unload/Revive are Linux's vkms connector juggling. On Windows the
// streamer detaches its monitor itself on the way out, and the agent runs
// its --teardown-virtual-display helper after a hard kill (see
// streamhost.virtualDisplayTeardownNeeded).
func Unload() error { return nil }
func Revive() error { return nil }

// psSingleQuoted quotes s as a PowerShell single-quoted string literal.
func psSingleQuoted(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// installCommandArgs builds the powershell.exe arguments that run scriptPath
// (with -LogPath logPath) in a new powershell.exe -- elevated through UAC
// when elevate is set -- wait for it, and exit with its exit code. The
// inner command line is what Start-Process hands the new process verbatim,
// so its paths are double-quoted for that process's own argument parsing;
// the whole of it is then a single-quoted literal for the outer -Command.
// elevate is false only in tests, which can't answer a UAC prompt.
func installCommandArgs(scriptPath, logPath string, elevate bool) []string {
	inner := fmt.Sprintf(`-NoProfile -ExecutionPolicy Bypass -File "%s" -LogPath "%s"`, scriptPath, logPath)
	verb := ""
	if elevate {
		verb = " -Verb RunAs"
	}
	cmd := fmt.Sprintf(
		"$p = Start-Process -FilePath powershell.exe%s -Wait -PassThru -WindowStyle Hidden -ArgumentList %s; exit $p.ExitCode",
		verb, psSingleQuoted(inner),
	)
	return []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", cmd}
}

// errInstallCancelled is returned when the UAC prompt was declined.
var errInstallCancelled = errors.New("virtual display driver install was cancelled at the administrator prompt")

// interpretInstallResult turns the outer powershell's outcome into an error.
// Start-Process throws "...canceled by the user" when the UAC prompt is
// declined; any other failure carries the tail of the elevated script's own
// transcript (logTail), which is the only place its error message lands.
func interpretInstallResult(runErr error, output, logTail string) error {
	if runErr == nil {
		return nil
	}
	if strings.Contains(output, "canceled by the user") || strings.Contains(output, "cancelled by the user") {
		return errInstallCancelled
	}
	detail := strings.TrimSpace(logTail)
	if detail == "" {
		detail = strings.TrimSpace(output)
	}
	return fmt.Errorf("virtual display driver install failed: %v: %s", runErr, detail)
}

// lastLines returns up to n trailing non-empty lines of s.
func lastLines(s string, n int) string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// InstallDriver runs the embedded install script elevated (one UAC
// prompt), then waits for the monitor to appear.
func InstallDriver() error {
	if DriverInstalled() {
		return nil
	}
	dir, err := os.MkdirTemp("", "usbridge-mttvdd-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	scriptPath := filepath.Join(dir, "install-mttvdd.ps1")
	logPath := filepath.Join(dir, "install.log")
	// UTF-8 BOM: Windows PowerShell 5.1 reads a BOM-less .ps1 as ANSI.
	if err := os.WriteFile(scriptPath, append([]byte{0xEF, 0xBB, 0xBF}, installScript...), 0o600); err != nil {
		return err
	}

	cmd := exec.Command("powershell.exe", installCommandArgs(scriptPath, logPath, true)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, runErr := cmd.CombinedOutput()
	logData, _ := os.ReadFile(logPath)
	log.Printf("[vdisplay] MttVDD install exit=%v output=%q log=%q", runErr, string(out), lastLines(string(logData), 20))
	if err := interpretInstallResult(runErr, string(out), lastLines(string(logData), 6)); err != nil {
		return err
	}

	deadline := time.Now().Add(installPollTimeout)
	for !DriverInstalled() {
		if time.Now().After(deadline) {
			return fmt.Errorf("the virtual display driver installed, but its monitor didn't appear within %s -- a reboot may be needed", installPollTimeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil
}
