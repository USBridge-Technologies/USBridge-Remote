//go:build linux

package streamhost

import (
	"os/exec"
	"syscall"
)

// configureRustshineProcess makes the kernel SIGKILL gamestream-server if
// the agent dies for any reason — same reasoning as Sunshine's
// configureProcess.
func configureRustshineProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

// rustshineAfterStart is a no-op on Linux: Pdeathsig above already gives an
// OS-enforced kill-on-parent-death guarantee before the process even starts.
func rustshineAfterStart(b *rustshineBackend, cmd *exec.Cmd) {}
