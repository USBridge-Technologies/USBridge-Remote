//go:build windows

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "USBridgeAgent"

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

func IsEnabled() bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return false
	}
	defer s.Close()

	cfg, err := s.Config()
	if err != nil {
		return false
	}
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
