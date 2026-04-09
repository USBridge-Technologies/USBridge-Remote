//go:build darwin || linux

package video

import (
	"os/exec"
	"syscall"
)

func configureFFmpegCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

func terminateFFmpegProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil && pgid > 0 {
		return syscall.Kill(-pgid, syscall.SIGTERM)
	}
	return cmd.Process.Kill()
}
