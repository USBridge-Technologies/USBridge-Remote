package streamhost

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeHungProc models a Sunshine whose Wait() blocks until released. If
// killable is false, Kill() has no effect -- like a process whose threads
// are stuck in uninterruptible kernel sleep (uinput teardown deadlock).
type fakeHungProc struct {
	killable bool
	release  chan struct{}
	once     sync.Once
	killed   int
}

func newFakeHungProc(killable bool) *fakeHungProc {
	return &fakeHungProc{killable: killable, release: make(chan struct{})}
}
func (f *fakeHungProc) Pid() int { return 4242 }
func (f *fakeHungProc) Kill() error {
	f.killed++
	if f.killable {
		f.once.Do(func() { close(f.release) })
	}
	return nil
}
func (f *fakeHungProc) Wait() error { <-f.release; return nil }

func unusedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func trackedBackend(f sunshineProcess) (*sunshineBackend, *trackedSunshineProc) {
	tp := newTrackedSunshineProc(f)
	b := &sunshineBackend{proc: tp}
	go b.watchProcessExit(tp)
	return b, tp
}

// The core bug: a process that is alive but whose admin port never answers
// used to be believed healthy forever.
func TestRecoverHung_UnreachableBeyondGraceKillsAndClears(t *testing.T) {
	f := newFakeHungProc(true)
	b, _ := trackedBackend(f)
	port := unusedPort(t)

	b.mu.Lock()
	defer b.mu.Unlock()
	if handled, err := b.recoverHungLocked(port); !handled || err != nil {
		t.Fatalf("first unhealthy sighting must only start the clock, got handled=%v err=%v", handled, err)
	}
	b.unhealthySince = time.Now().Add(-2 * sunshineHangGrace)
	handled, err := b.recoverHungLocked(port)
	if handled || err != nil {
		t.Fatalf("hung process should be killed and Start should relaunch, got handled=%v err=%v", handled, err)
	}
	if f.killed == 0 {
		t.Error("hung process was never killed")
	}
	if b.proc != nil {
		t.Error("b.proc must be cleared so a fresh Sunshine can launch")
	}
}

func TestRecoverHung_WithinGraceIsLeftAlone(t *testing.T) {
	f := newFakeHungProc(true)
	b, _ := trackedBackend(f)
	port := unusedPort(t)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.recoverHungLocked(port)
	b.unhealthySince = time.Now().Add(-sunshineHangGrace / 2)
	if handled, err := b.recoverHungLocked(port); !handled || err != nil || f.killed != 0 {
		t.Fatalf("slow cold start must not be killed: handled=%v err=%v kills=%d", handled, err, f.killed)
	}
}

func TestRecoverHung_ReachablePortResetsClock(t *testing.T) {
	f := newFakeHungProc(true)
	b, _ := trackedBackend(f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.unhealthySince = time.Now().Add(-2 * sunshineHangGrace)
	if handled, _ := b.recoverHungLocked(l.Addr().(*net.TCPAddr).Port); !handled || !b.unhealthySince.IsZero() || f.killed != 0 {
		t.Fatal("a reachable admin port means healthy: clock reset, nothing killed")
	}
}

// Uninterruptible process: never spawn a second instance on top of it.
func TestRecoverHung_UnkillableReportsStuckAndKeepsProc(t *testing.T) {
	f := newFakeHungProc(false)
	b, _ := trackedBackend(f)
	port := unusedPort(t)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.unhealthySince = time.Now().Add(-2 * sunshineHangGrace)
	handled, err := b.recoverHungLocked(port)
	if !handled || !errors.Is(err, errSunshineStuck) {
		t.Fatalf("want handled=true + errSunshineStuck, got handled=%v err=%v", handled, err)
	}
	if b.proc == nil {
		t.Error("stuck proc must stay tracked so no duplicate Sunshine is launched")
	}
}

// Stop() must ask nicely first: a process that exits on SIGTERM must never
// receive SIGKILL.
func TestStop_TerminatesGracefullyBeforeKill(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "trap 'exit 0' TERM; while :; do sleep 0.1; done")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // let the shell install its trap
	tp := newTrackedSunshineProc(sunshineExecCmdProcess{cmd})
	b := &sunshineBackend{proc: tp}
	go b.watchProcessExit(tp)

	start := time.Now()
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if d := time.Since(start); d >= sunshineStopGrace {
		t.Errorf("SIGTERM-responsive process took %s: Stop fell through to SIGKILL", d)
	}
	if !tp.exited(time.Second) {
		t.Error("process still running after Stop")
	}
}

func pidAlive(pid int) bool {
	// signal 0 succeeds for zombies too; check /proc state instead.
	b, err := exec.Command("ps", "-o", "stat=", "-p", itoa(pid)).Output()
	return err == nil && len(b) > 0 && b[0] != 'Z'
}

func itoa(i int) string { return strconvItoa(i) }

// End-to-end with REAL processes through the real Start()/Stop(): a fake
// "sunshine" that ignores SIGTERM and never opens its admin port. Start()
// must notice, SIGKILL it, and launch a replacement with a new pid.
func TestStart_RealHungProcessIsDetectedKilledAndReplaced(t *testing.T) {
	oldG, oldS := sunshineHangGrace, sunshineStopGrace
	sunshineHangGrace, sunshineStopGrace = 200*time.Millisecond, 500*time.Millisecond
	defer func() { sunshineHangGrace, sunshineStopGrace = oldG, oldS }()

	dir := t.TempDir()
	script := dir + "/fake-sunshine"
	writeExec(t, script, "#!/bin/sh\ncase \"$*\" in *--creds*) exit 0;; esac\ntrap '' TERM\nwhile :; do sleep 0.2; done\n")
	b := &sunshineBackend{launchPath: script, exeDir: dir, stateDir: dir}
	defer b.Stop()
	port := unusedPort(t)

	if err := b.Start(port); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	pid1 := b.Pid()
	if pid1 == 0 || !pidAlive(pid1) {
		t.Fatalf("fake sunshine not running (pid=%d)", pid1)
	}
	// Alive but unreachable: first tick starts the clock, no action.
	b.Start(port)
	if b.Pid() != pid1 {
		t.Fatal("process replaced before the hang grace period elapsed")
	}
	time.Sleep(sunshineHangGrace + 100*time.Millisecond)
	if err := b.Start(port); err != nil {
		t.Fatalf("recovery Start: %v", err)
	}
	pid2 := b.Pid()
	if pid2 == pid1 || pid2 == 0 {
		t.Fatalf("hung process was not replaced: pid1=%d pid2=%d", pid1, pid2)
	}
	if pidAlive(pid1) {
		t.Errorf("hung pid %d still alive after recovery", pid1)
	}
	if !pidAlive(pid2) {
		t.Errorf("replacement pid %d is not running", pid2)
	}
}

// Stop() on a process that ignores SIGTERM must escalate to SIGKILL.
func TestStop_EscalatesToKillWhenSigtermIgnored(t *testing.T) {
	oldS := sunshineStopGrace
	sunshineStopGrace = 500 * time.Millisecond
	defer func() { sunshineStopGrace = oldS }()

	cmd := exec.Command("/bin/sh", "-c", "trap '' TERM; while :; do sleep 0.2; done")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	tp := newTrackedSunshineProc(sunshineExecCmdProcess{cmd})
	b := &sunshineBackend{proc: tp}
	go b.watchProcessExit(tp)
	b.Stop()
	if !tp.exited(2 * time.Second) {
		t.Fatal("SIGTERM-ignoring process survived Stop()")
	}
}

func strconvItoa(i int) string { return strconv.Itoa(i) }

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
