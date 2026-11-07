//go:build darwin

package gui

import (
	"fmt"
	"os/exec"
	"strings"
)

func nativeSaveAvailable() bool { return true }

// choose file name has no type filter; the caller appends the extension.
func nativeSaveFile(_ uintptr, title, defaultName string, _ saveFilter) (string, error) {
	script := fmt.Sprintf(
		// activate: the panel belongs to osascript, which otherwise opens
		// it behind the client window.
		"activate\nPOSIX path of (choose file name with prompt %q default name %q)",
		title, defaultName,
	)
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		// User cancelled: osascript exits non-zero with no path.
		if _, ok := err.(*exec.ExitError); ok {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
