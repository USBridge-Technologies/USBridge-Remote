//go:build !windows && !darwin && !linux

package video

import "os/exec"

func configureFFmpegCommand(cmd *exec.Cmd) {}

func terminateFFmpegProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
