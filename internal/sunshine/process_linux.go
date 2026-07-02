//go:build linux

package sunshine

import (
	"os/exec"
	"syscall"
)

// configureProcess makes the kernel SIGKILL Sunshine if the agent dies for
// any reason (crash, SIGKILL, hung graceful-shutdown path) — a backstop
// against an orphaned Sunshine instance independent of our own cleanup code.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
