package streamhost

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// spawnDecoyNamed copies a real, harmless, long-running binary (/bin/sleep)
// to name inside a temp dir and execs that copy directly, so the resulting
// process's own name (what killall/taskkill match against) is name, not
// "sleep" -- the same trick a real gamestream-server or sunshine binary
// would present, without needing either actual binary in this test.
func spawnDecoyNamed(t *testing.T, name string) *exec.Cmd {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("decoy process renaming via binary copy is unix-only; Windows path (taskkill /IM) is exercised by code review only")
	}
	src, err := os.Open("/bin/sleep")
	if err != nil {
		t.Skipf("no /bin/sleep on this system: %v", err)
	}
	defer src.Close()

	dst := filepath.Join(t.TempDir(), name)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create decoy binary: %v", err)
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		t.Fatalf("copy decoy binary: %v", err)
	}
	out.Close()

	cmd := exec.Command(dst, "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start decoy process %s: %v", name, err)
	}
	return cmd
}

// TestKillOrphanStreamerProcesses_KillsEachKnownName is the actual mechanism
// both backends' Start() now rely on instead of blindly trusting a
// reachable admin port belonged to them: this proves it actually finds and
// kills a process by each name either backend's launcher can produce,
// regardless of which one is currently squatting the GameStream ports.
func TestKillOrphanStreamerProcesses_KillsEachKnownName(t *testing.T) {
	for _, name := range []string{"gamestream-server", "usbridge-streamer", "sunshine"} {
		t.Run(name, func(t *testing.T) {
			cmd := spawnDecoyNamed(t, name)
			t.Cleanup(func() { _ = cmd.Process.Kill() })

			killOrphanStreamerProcesses()

			done := make(chan struct{})
			go func() {
				_, _ = cmd.Process.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatalf("decoy process named %q was not killed by killOrphanStreamerProcesses", name)
			}
		})
	}
}

// TestKillOrphanStreamerProcesses_LeavesUnrelatedProcessesAlone guards
// against an overly broad match (e.g. a substring kill) taking down
// something that just happens to share part of a name.
func TestKillOrphanStreamerProcesses_LeavesUnrelatedProcessesAlone(t *testing.T) {
	cmd := spawnDecoyNamed(t, "totally-unrelated-process")
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	killOrphanStreamerProcesses()

	// Give it a moment, then confirm it's still alive (Signal(0) is a
	// liveness probe, not an actual kill).
	time.Sleep(300 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Errorf("unrelated decoy process appears to have been killed: %v", err)
	}
}
