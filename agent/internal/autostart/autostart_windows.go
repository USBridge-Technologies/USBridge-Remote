//go:build windows

package autostart

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const runValueName = "USBridgeAgent"

func openRunKey(access uint32) (registry.Key, error) {
	return registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, access)
}

func commandLine() (string, error) {
	exe, args, err := LaunchTarget()
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, fmt.Sprintf("\"%s\"", exe))
	for _, a := range args {
		if strings.ContainsRune(a, ' ') {
			parts = append(parts, fmt.Sprintf("\"%s\"", a))
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " "), nil
}

func IsEnabled() bool {
	key, err := openRunKey(registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	_, _, err = key.GetStringValue(runValueName)
	return err == nil
}

func Enable() error {
	cmd, err := commandLine()
	if err != nil {
		return err
	}

	// Unblock the file (remove Mark of the Web) so Windows doesn't show a security
	// confirmation prompt during automatic launch at login.
	exe, _, err := LaunchTarget()
	if err == nil && exe != "" {
		_ = os.Remove(exe + ":Zone.Identifier")
	}

	key, err := openRunKey(registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(runValueName, cmd)
}

func Disable() error {
	key, err := openRunKey(registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	err = key.DeleteValue(runValueName)
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}

// RefreshX11SessionEnv is a Linux/SDDM-only concept (see its doc comment on
// the linux build) -- no-op everywhere else.
func RefreshX11SessionEnv() {}

// EnsureDisplayActive is a Linux/X11-only concept (see its doc comment on
// the linux build) -- no-op everywhere else.
func EnsureDisplayActive() {}
