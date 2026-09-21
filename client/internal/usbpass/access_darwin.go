//go:build darwin && !ios

package usbpass

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

static bool usbpassPreflightInputMonitoring(void) {
    return CGPreflightListenEventAccess();
}

static bool usbpassRequestInputMonitoring(void) {
    return CGRequestListenEventAccess();
}
*/
import "C"

import (
	"os/exec"
	"sync"
)

var (
	lastAccessErrMu  sync.Mutex
	lastAccessErrMsg string
)

// LastUSBAccessError surfaces the most recent macOS access-denial detail
// (mirrors Linux's pkexec/udev message) so the UI can render it alongside
// the "grant access" action.
func LastUSBAccessError() string {
	lastAccessErrMu.Lock()
	defer lastAccessErrMu.Unlock()
	return lastAccessErrMsg
}

func setLastAccessError(msg string) {
	lastAccessErrMu.Lock()
	lastAccessErrMsg = msg
	lastAccessErrMu.Unlock()
}

// InputMonitoringGranted reports whether this app currently holds macOS's
// Input Monitoring TCC permission. hidbridge_darwin.go's IOHIDDeviceOpen
// (even non-exclusive, kIOHIDOptionsTypeNone) fails against any external
// keyboard/mouse HID device without it -- confirmed live against a Razer
// Viper Ultimate dongle (1532:007b, log: "hidbridge: failed to open HID
// device 1532:007b").
func InputMonitoringGranted() bool {
	return bool(C.usbpassPreflightInputMonitoring())
}

// RequestInputMonitoringAccess triggers macOS's one-time Input Monitoring
// consent prompt (CGRequestListenEventAccess) and reports whether access is
// granted. Once the user has answered that prompt once (either way), macOS
// never shows it again for this app -- OpenInputMonitoringSettingsPane is
// the only way back in after a denial.
func RequestInputMonitoringAccess() bool {
	return bool(C.usbpassRequestInputMonitoring())
}

// OpenInputMonitoringSettingsPane opens System Settings directly to the
// Input Monitoring pane so the user can flip the switch by hand when
// RequestInputMonitoringAccess can no longer (re-)prompt.
func OpenInputMonitoringSettingsPane() {
	_ = exec.Command("open", "x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent").Start()
}

type usbDevRef struct {
	BusID  string
	Busnum uint32
	Devnum uint32
}

// USBAccessGranted is always true on macOS -- there is no udev-style group
// gate here, only the per-device Input Monitoring TCC check above.
func USBAccessGranted(devs []usbDevRef) bool { return true }

func usbAccessNeeded(devs []usbDevRef) bool { return false }

// EnsureUSBAccess is a no-op on macOS.
func EnsureUSBAccess(devs []usbDevRef) error { return nil }

// RequestUSBAccess is claimDevice's post-failure hook (see session.go). On
// macOS the only access class a claim failure can actually need is Input
// Monitoring (hidbridge_darwin.go's IOHIDDeviceOpen) -- this preflights and,
// if undecided, prompts for it, recording a message for LastUSBAccessError
// when it's still missing afterward so the caller's wrapped error carries an
// actionable hint.
func RequestUSBAccess(devs []usbDevRef) bool {
	if InputMonitoringGranted() {
		setLastAccessError("")
		return true
	}
	if RequestInputMonitoringAccess() {
		setLastAccessError("")
		return true
	}
	setLastAccessError("Input Monitoring access is required for USB passthrough of keyboards/mice. Grant it in System Settings → Privacy & Security → Input Monitoring, then restart USBridge Client and try again.")
	return false
}

// powerCycleUSBPort is Linux-only (sysfs authorized toggle); macOS never
// wedges a claim the same way, so this is a no-op.
func powerCycleUSBPort(busID string) error { return nil }
