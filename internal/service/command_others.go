//go:build !windows

package service

import (
	"os/exec"
)

func maybeHideWindow(cmd *exec.Cmd) {
	// No-op on non-Windows platforms
}
