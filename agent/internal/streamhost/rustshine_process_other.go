//go:build !linux && !windows

package streamhost

import (
	"fmt"
	"log"
	"os"
	"os/exec"
)

// configureRustshineProcess is a no-op on macOS — Pdeathsig has no
// equivalent there. See rustshineAfterStart.
func configureRustshineProcess(cmd *exec.Cmd) {}

// rustshineAfterStart spawns the same kind of detached shell watchdog
// sunshineBackend's afterStart uses (process_other.go) — a genuinely
// separate OS process polling the agent's PID, since macOS has no
// Pdeathsig/Job-Object equivalent.
func rustshineAfterStart(b *rustshineBackend, cmd *exec.Cmd) {
	agentPID := os.Getpid()
	childPID := cmd.Process.Pid
	script := fmt.Sprintf(
		`while kill -0 %d 2>/dev/null; do sleep 1; done; kill -9 %d 2>/dev/null`,
		agentPID, childPID,
	)
	watchdog := exec.Command("/bin/sh", "-c", script)
	if err := watchdog.Start(); err != nil {
		log.Printf("[rustshine] failed to start death-watchdog, gamestream-server may survive an agent crash: %v", err)
		return
	}
	b.watchdog = watchdog
	go func() { _ = watchdog.Wait() }()
}
