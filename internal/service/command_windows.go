//go:build windows

package service

import (
	"os/exec"
	"syscall"
)

func maybeHideWindow(cmd *exec.Cmd) {
	if cmd != nil {
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.HideWindow = true
		cmd.SysProcAttr.CreationFlags = 0x08000000 // CREATE_NO_WINDOW
	}
}
