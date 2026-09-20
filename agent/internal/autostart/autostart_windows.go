//go:build windows

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "USBridgeAgent"

var (
	shell32             = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	swHide                = 0
	elevatedWait          = 60 * time.Second
)

// shellExecuteInfoW mirrors Win32 SHELLEXECUTEINFOW so Enable/Disable can
// wait until the elevated --install-service / --uninstall-service helper
// actually exits (plain ShellExecuteW only reports that it launched).
type shellExecuteInfoW struct {
	cbSize         uint32
	fMask          uint32
	hwnd           uintptr
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       uintptr
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      uintptr
	dwHotKey       uint32
	hIconOrMonitor uintptr
	hProcess       uintptr
}

// IsEnabled reports whether the USBridgeAgent service is registered with
// AUTO_START. Deliberately opens the SCM/service with read-only access
// (SC_MANAGER_CONNECT / SERVICE_QUERY_CONFIG) instead of going through
// mgr.Connect()+Mgr.OpenService, which request SC_MANAGER_ALL_ACCESS and
// SERVICE_ALL_ACCESS respectively -- both of those need an elevated token
// against a LocalSystem-owned service, so calling them from the normal
// (non-elevated) tray/UI process that runs after a plain login always fails
// with "Access is denied", and the error path above made IsEnabled() return
// false unconditionally. That silently mismatched reality: the checkbox
// showed unchecked even with the service correctly registered as
// AUTO_START and running (confirmed live from a non-admin session on this
// exact box). Read-only rights need no elevation and are enough to answer
// this question.
func IsEnabled() bool {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(scm)

	namePtr, err := windows.UTF16PtrFromString(serviceName)
	if err != nil {
		return false
	}
	h, err := windows.OpenService(scm, namePtr, windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(h)

	n := uint32(4096)
	buf := make([]byte, n)
	if err := windows.QueryServiceConfig(h, (*windows.QUERY_SERVICE_CONFIG)(unsafe.Pointer(&buf[0])), n, &n); err != nil {
		return false
	}
	cfg := (*windows.QUERY_SERVICE_CONFIG)(unsafe.Pointer(&buf[0]))
	return cfg.StartType == mgr.StartAutomatic
}

// NeedsReboot is true after Autostart at Boot has been granted (the
// USBridgeAgent service is registered AUTO_START) but that grant is not
// yet in effect for this boot: the service is not running. Install no
// longer Start()s the service from a live GUI (that would spawn a second
// tray icon in the same session); SCM starts it on the next reboot, at
// which point this returns false and the UI hint disappears.
func NeedsReboot() bool {
	return IsEnabled() && !isServiceRunning()
}

func isServiceRunning() bool {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(scm)

	namePtr, err := windows.UTF16PtrFromString(serviceName)
	if err != nil {
		return false
	}
	h, err := windows.OpenService(scm, namePtr, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(h)

	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &st); err != nil {
		return false
	}
	return st.CurrentState == windows.SERVICE_RUNNING
}

func Enable() error {
	// Request UAC elevation to run `--install-service`
	exe, _, err := LaunchTarget()
	if err != nil {
		return err
	}
	if exe != "" {
		_ = os.Remove(exe + ":Zone.Identifier")
	}

	if err := runElevated(exe, "--install-service"); err != nil {
		return err
	}
	if !IsEnabled() {
		return fmt.Errorf("Windows service was not registered as AUTO_START")
	}
	return nil
}

func Disable() error {
	exe, _, err := LaunchTarget()
	if err != nil {
		return err
	}
	if err := runElevated(exe, "--uninstall-service"); err != nil {
		return err
	}
	if IsEnabled() {
		return fmt.Errorf("Windows service is still registered as AUTO_START")
	}
	return nil
}

func runElevated(exe string, args string) error {
	verbPtr, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	filePtr, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	paramsPtr, err := syscall.UTF16PtrFromString(args)
	if err != nil {
		return err
	}
	dirPtr, err := syscall.UTF16PtrFromString(filepath.Dir(exe))
	if err != nil {
		return err
	}

	info := shellExecuteInfoW{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verbPtr,
		lpFile:       filePtr,
		lpParameters: paramsPtr,
		lpDirectory:  dirPtr,
		nShow:        swHide,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))

	ret, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return fmt.Errorf("ShellExecuteExW(runas) failed: %v (the UAC prompt may have been declined)", callErr)
	}
	if info.hProcess == 0 {
		return nil
	}
	h := windows.Handle(info.hProcess)
	defer windows.CloseHandle(h)

	event, waitErr := windows.WaitForSingleObject(h, uint32(elevatedWait/time.Millisecond))
	if waitErr != nil {
		return fmt.Errorf("WaitForSingleObject: %w", waitErr)
	}
	if event == uint32(windows.WAIT_TIMEOUT) {
		return fmt.Errorf("elevated helper did not finish within %s", elevatedWait)
	}
	if event != uint32(windows.WAIT_OBJECT_0) {
		return fmt.Errorf("WaitForSingleObject: unexpected status %d", event)
	}

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err == nil && code != 0 {
		return fmt.Errorf("elevated helper exited with code %d", code)
	}
	return nil
}

// RefreshX11SessionEnv is a Linux/SDDM-only concept (see its doc comment on
// the linux build) -- no-op everywhere else.
func RefreshX11SessionEnv() {}

// EnsureDisplayActive is a Linux/X11-only concept (see its doc comment on
// the linux build) -- no-op everywhere else.
func EnsureDisplayActive() {}

// Location is what the GUI's info button shows.
func Location() string { return "Windows service: " + serviceName }
