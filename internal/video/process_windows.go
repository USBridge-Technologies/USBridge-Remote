//go:build windows

package video

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureFFmpegCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

func terminateFFmpegProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
