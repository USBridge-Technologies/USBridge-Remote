//go:build windows

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "USBridgeAgent"

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

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

func Enable() error {
	// Request UAC elevation to run `--install-service`
	exe, _, err := LaunchTarget()
	if err != nil {
		return err
	}
	if exe != "" {
		_ = os.Remove(exe + ":Zone.Identifier")
	}

	return runElevated(exe, "--install-service")
}

func Disable() error {
	exe, _, err := LaunchTarget()
	if err != nil {
		return err
	}
	return runElevated(exe, "--uninstall-service")
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

	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		uintptr(unsafe.Pointer(paramsPtr)),
		uintptr(unsafe.Pointer(dirPtr)),
		0, // SW_HIDE
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW(runas) failed with code %d (the UAC prompt may have been declined)", ret)
	}
	return nil
}

// RefreshX11SessionEnv is a Linux/SDDM-only concept (see its doc comment on
// the linux build) -- no-op everywhere else.
func RefreshX11SessionEnv() {}

// EnsureDisplayActive is a Linux/X11-only concept (see its doc comment on
// the linux build) -- no-op everywhere else.
func EnsureDisplayActive() {}
