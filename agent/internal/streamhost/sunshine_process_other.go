//go:build !linux && !windows

package streamhost

import (
	"fmt"
	"log"
	"os"
	"os/exec"
)

// configureProcess is a no-op on macOS — Pdeathsig has no equivalent there.
// The kill-on-parent-death guarantee is implemented in afterStart instead.
func configureProcess(cmd *exec.Cmd) {}

// afterStart spawns a tiny detached shell watchdog that polls the agent's
// own PID and kills Sunshine the moment the agent process is gone — the
// closest portable equivalent to Linux's Pdeathsig or Windows' kill-on-job-
// close Job Object, neither of which macOS has. Crucially this is a genuinely
// separate OS process, not a goroutine: it keeps running and can still act
// even if the agent itself is killed with SIGKILL and never gets a chance to
// run its own cleanup code.
func afterStart(b *sunshineBackend, cmd *exec.Cmd) {
	agentPID := os.Getpid()
	sunshinePID := cmd.Process.Pid
	script := fmt.Sprintf(
		`while kill -0 %d 2>/dev/null; do sleep 1; done; kill -9 %d 2>/dev/null`,
		agentPID, sunshinePID,
	)
	watchdog := exec.Command("/bin/sh", "-c", script)
	if err := watchdog.Start(); err != nil {
		log.Printf("[sunshine] failed to start death-watchdog, Sunshine may survive an agent crash: %v", err)
		return
	}
	b.watchdog = watchdog
	go func() { _ = watchdog.Wait() }()
}
