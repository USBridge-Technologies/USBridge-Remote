//go:build !windows

package ui

import (
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

// relaunchAfterEngineUpdate waits for the engine to come back from its
// update, then re-execs this GUI from the (replaced) binary on disk.
func relaunchAfterEngineUpdate(r remoteAgentUpdater) {
	wentDown := false
	for deadline := time.Now().Add(3 * time.Minute); time.Now().Before(deadline); {
		time.Sleep(time.Second)
		if _, err := r.AgentUpdateStatus(); err != nil {
			wentDown = true
		} else if wentDown {
			break
		}
	}
	time.Sleep(2 * time.Second)
	exe, err := os.Executable()
	if err == nil {
		if res, e := filepath.EvalSymlinks(exe); e == nil {
			exe = res
		}
		err = syscall.Exec(exe, os.Args, os.Environ())
	}
	logrus.WithError(err).Warn("relaunch after update failed -- restart the agent window to use the new version")
}
