//go:build windows

package sunshine

import (
	"os/exec"
	"syscall"
)

// configureProcess hides the console window Windows would otherwise pop up
// for Sunshine (a console-subsystem exe) when spawned from the agent, which
// has no console of its own.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
