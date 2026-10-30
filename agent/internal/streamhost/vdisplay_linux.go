//go:build linux

package streamhost

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
)

func init() {
	// vkms is in-tree: the virtual monitor is there to pick everywhere; without
	// the agent's grant a session on it says so (vdisplay.Prepare).
	virtualDriverInstalled = func() bool { return true }
	sessionDisplayHelperStart = startDetachedHelper
}

// startDetachedHelper starts the display helper so it outlives the agent: in a
// systemd user unit of its own when there is a user manager (the agent's own
// unit takes every process in it down with it), else in a session of its own.
// It gets the desktop's environment, so kscreen-doctor reaches the compositor.
func startDetachedHelper(exe string, args []string) error {
	if path, err := exec.LookPath("systemd-run"); err == nil {
		full := []string{"--user", "--collect", "--quiet", fmt.Sprintf("--unit=usbridge-display-watch-%d", os.Getpid())}
		for _, k := range []string{"WAYLAND_DISPLAY", "DISPLAY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE", "XAUTHORITY"} {
			if v, ok := os.LookupEnv(k); ok {
				full = append(full, "--setenv="+k+"="+v)
			}
		}
		full = append(append(full, exe), args...)
		if out, err := exec.Command(path, full...).CombinedOutput(); err == nil {
			return nil
		} else {
			log.Printf("[vdisplay] systemd-run for the display watcher: %v (%s) -- starting it detached instead", err, out)
		}
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
