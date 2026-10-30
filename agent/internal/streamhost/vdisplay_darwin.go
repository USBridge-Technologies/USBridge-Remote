//go:build darwin

package streamhost

import (
	"os"
	"os/exec"
	"syscall"

	"usbridge_agent/internal/displaypower"
)

func init() {
	// CGVirtualDisplay needs no driver: the virtual monitor is there to pick.
	virtualDriverInstalled = func() bool { return true }
	// It lives as long as the process that made it, so the display helper runs
	// in the agent itself (displaypower_darwin.go) -- all but `watch`, below.
	runDisplayHelper = func(args ...string) (displaypower.HelperResult, error) {
		return displaypower.RunInProcess(args), nil
	}
	sessionDisplayHelperStart = startDetachedHelper
}

// startDetachedHelper starts the display watcher in a session of its own, so it
// outlives the agent (and whatever takes the agent's process group down with
// it) to put the monitors back.
func startDetachedHelper(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, devnull, devnull
		defer devnull.Close()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
