//go:build linux

package ui

import (
	"os/exec"
	"strings"
)

// pickNativePath opens the system's own file (or folder) dialog (zenity, else
// kdialog); see the Windows file.
func pickNativePath(folder bool, title string) (path string, shown bool) {
	zenity := []string{"--file-selection", "--title=" + title}
	kdialog := []string{"--getopenfilename", ".", "--title", title}
	if folder {
		zenity = append(zenity, "--directory")
		kdialog = []string{"--getexistingdirectory", ".", "--title", title}
	}
	for _, c := range [][]string{append([]string{"zenity"}, zenity...), append([]string{"kdialog"}, kdialog...)} {
		out, err := exec.Command(c[0], c[1:]...).Output()
		if err == nil {
			return strings.TrimSpace(string(out)), true
		}
		if _, cancelled := err.(*exec.ExitError); cancelled {
			return "", true
		}
	}
	return "", false
}
