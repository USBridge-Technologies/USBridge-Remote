//go:build linux

package streamhost

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// A streamer started through usbridge-streamer-launch runs from a memfd,
// so its comm isn't its name and killall misses it (confirmed live);
// orphan cleanup has to find it by argv[0].
func TestKillOrphansByArgv0_FindsProcessByArgv0(t *testing.T) {
	const name = "usbridge-orphan-test-dummy"
	cmd := exec.Command("/bin/sleep", "30")
	cmd.Args[0] = name
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	killOrphansByArgv0([]string{name})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cmd.Process.Kill()
		t.Fatal("process with matching argv[0] was not killed")
	}
}

func TestKillOrphansByArgv0_LeavesOthersAlone(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	cmd.Args[0] = "usbridge-orphan-test-bystander"
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	killOrphansByArgv0([]string{"usbridge-orphan-test-dummy"})
	time.Sleep(100 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("non-matching process was killed")
	}
}
