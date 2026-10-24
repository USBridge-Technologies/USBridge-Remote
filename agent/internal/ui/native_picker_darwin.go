//go:build darwin

package ui

import (
	"fmt"
	"os/exec"
	"strings"
)

// pickNativePath opens the system's own file (or folder) dialog; see the Windows file.
func pickNativePath(folder bool, title string) (path string, shown bool) {
	kind := "file"
	if folder {
		kind = "folder"
	}
	out, err := exec.Command("osascript", "-e", fmt.Sprintf(`POSIX path of (choose %s with prompt %q)`, kind, title)).Output()
	if err != nil {
		if _, cancelled := err.(*exec.ExitError); cancelled {
			return "", true
		}
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
