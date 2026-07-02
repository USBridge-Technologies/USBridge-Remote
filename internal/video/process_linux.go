//go:build linux

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
		// Pdeathsig makes the kernel SIGKILL ffmpeg if the agent dies for any
		// reason (crash, SIGKILL, hung graceful-shutdown path) — a backstop
		// against orphaned encoder processes independent of our own cleanup code.
		Pdeathsig: syscall.SIGKILL,
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
