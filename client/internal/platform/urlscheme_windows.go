//go:build windows

package platform

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const schemeKey = `Software\Classes\` + URLScheme

func schemeCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return `"` + exe + `" "%1"`, nil
}

// IsURLSchemeRegistered reports whether usbridge:// links open this app. The
// installer registers it per machine or per user; either counts when the
// command points at the program that is running.
func IsURLSchemeRegistered() bool {
	want, err := schemeCommand()
	if err != nil {
		return false
	}
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, schemeKey+`\shell\open\command`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		got, _, err := k.GetStringValue("")
		k.Close()
		if err == nil && strings.EqualFold(got, want) {
			return true
		}
	}
	return false
}

// RegisterURLScheme makes usbridge:// links open this app for the current user.
func RegisterURLScheme() error {
	cmd, err := schemeCommand()
	if err != nil {
		return err
	}
	root, _, err := registry.CreateKey(registry.CURRENT_USER, schemeKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.SetStringValue("", "URL:USBridge"); err != nil {
		return err
	}
	if err := root.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}
	c, _, err := registry.CreateKey(root, `shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.SetStringValue("", cmd)
}
