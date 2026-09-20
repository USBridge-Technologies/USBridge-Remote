package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestAcquireEngineLock_ExclusiveAcrossHandles is the actual bug this file
// exists to prevent: two independent processes (simulated here by two
// separate *os.File handles on the same lock path, since flock is keyed by
// the open file description, not the process) must never both believe they
// own the engine at once. This is exactly what let a headless LaunchAgent
// and a manually-launched GUI both bind their own streamhost.Backend to the
// same GameStream ports at the same time -- see enginelock.go's doc
// comment.
func TestAcquireEngineLock_ExclusiveAcrossHandles(t *testing.T) {
	dir := t.TempDir()

	f1, holderPID1, ok1, err := acquireEngineLock(dir)
	if err != nil || !ok1 {
		t.Fatalf("first acquire: ok=%v err=%v (want ok=true)", ok1, err)
	}
	defer f1.Close()
	if holderPID1 != 0 {
		t.Errorf("first acquire reported a holder pid=%d, want 0 (we just took it)", holderPID1)
	}

	f2, holderPID2, ok2, err := acquireEngineLock(dir)
	if err != nil {
		t.Fatalf("second acquire returned an error: %v", err)
	}
	if ok2 {
		f2.Close()
		t.Fatal("second acquire succeeded while the first handle is still open -- the lock is not actually exclusive")
	}
	if holderPID2 != os.Getpid() {
		t.Errorf("second acquire reported holder pid=%d, want this test process's own pid=%d (the first acquire's PID stamp)", holderPID2, os.Getpid())
	}
}

// TestAcquireEngineLock_ReleasedOnClose confirms the actual guarantee this
// mechanism is built on: closing the fd (which is all a process death ever
// does, whether via clean exit, panic, or SIGKILL) releases the lock
// immediately, with no explicit unlock call required.
func TestAcquireEngineLock_ReleasedOnClose(t *testing.T) {
	dir := t.TempDir()

	f1, _, ok1, err := acquireEngineLock(dir)
	if err != nil || !ok1 {
		t.Fatalf("first acquire: ok=%v err=%v", ok1, err)
	}
	if err := f1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	f2, _, ok2, err := acquireEngineLock(dir)
	if err != nil || !ok2 {
		t.Fatalf("re-acquire after close: ok=%v err=%v (want ok=true -- the OS should have released the lock when the fd closed)", ok2, err)
	}
	defer f2.Close()
}

// TestKillPID_TerminatesRealProcess exercises the actual OS call
// evictEngineLockHolder falls back to when a graceful relinquish isn't
// possible.
func TestKillPID_TerminatesRealProcess(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start test process: %v", err)
	}
	pid := cmd.Process.Pid

	if !killPID(pid) {
		t.Fatal("killPID reported no live process, but we just started one")
	}

	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("process did not exit after killPID")
	}

	if killPID(pid) {
		t.Error("killPID reported a live process for a pid that's already dead (or, worse, killed something else after the pid was reused)")
	}
}

// TestEvictEngineLockHolder_FallsBackToKillWhenUnresponsive is the scenario
// that actually happened live: the recorded holder PID is a real, running
// process, but nothing is listening on the socket path it supposedly owns
// (a crash that never got to clean up, or -- as in the original bug -- a
// lock file whose holder simply isn't cooperating). evictEngineLockHolder
// must fall back to a hard kill rather than give up.
func TestEvictEngineLockHolder_FallsBackToKillWhenUnresponsive(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start decoy process: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	// No admin socket server is listening at this path -- dialAdminSocket
	// inside evictEngineLockHolder must fail fast and fall through to
	// killPID instead of hanging or silently giving up.
	unreachableSocket := filepath.Join(dir, "admin.sock")

	if !evictEngineLockHolder(dir, unreachableSocket, pid) {
		t.Fatal("evictEngineLockHolder reported failure against an unresponsive-but-real process")
	}

	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("decoy process was not killed by evictEngineLockHolder's fallback")
	}
}

// TestEvictEngineLockHolder_NoOpWithoutAPID mirrors what happens when a
// lock file exists but is empty or corrupt (e.g. this process raced the
// holder's own PID write) -- there is nothing to kill, and
// evictEngineLockHolder must say so rather than pretend success by killing
// pid 0 or a negative number.
func TestEvictEngineLockHolder_NoOpWithoutAPID(t *testing.T) {
	dir := t.TempDir()
	if !evictEngineLockHolder(dir, filepath.Join(dir, "admin.sock"), 0) {
		t.Error("evictEngineLockHolder with holderPID=0 should report true (nothing to evict, caller should just retry acquire) not false")
	}
}
