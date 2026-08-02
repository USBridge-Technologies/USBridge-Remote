//go:build linux

package streamhost

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

// afterStart is a no-op on Linux: Pdeathsig above already gives an
// OS-enforced kill-on-parent-death guarantee before Sunshine even starts.
func afterStart(b *sunshineBackend, cmd *exec.Cmd) {}
