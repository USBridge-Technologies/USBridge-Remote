package streamhost

import (
	"os"
	"runtime"
	"testing"
)

type fakeRustshineProcess struct{ killed int }

func (p *fakeRustshineProcess) Pid() int    { return 4242 }
func (p *fakeRustshineProcess) Kill() error { p.killed++; return nil }
func (p *fakeRustshineProcess) Wait() error { return nil }

// stubTeardown swaps runVirtualDisplayTeardown for a recorder for the rest
// of the test.
func stubTeardown(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	orig := runVirtualDisplayTeardown
	runVirtualDisplayTeardown = func(bin string) error {
		calls = append(calls, bin)
		return nil
	}
	t.Cleanup(func() { runVirtualDisplayTeardown = orig })
	return &calls
}

func TestVirtualDisplayTeardownNeeded(t *testing.T) {
	cases := []struct {
		goos     string
		launched bool
		bin      string
		want     bool
	}{
		{"windows", true, `C:\x\usbridge-streamer.exe`, true},
		{"windows", false, `C:\x\usbridge-streamer.exe`, false},
		{"windows", true, "", false}, // orphan-kill path: nothing known to run
		{"linux", true, "/opt/usbridge-streamer", false},
		{"darwin", true, "/opt/usbridge-streamer", false},
	}
	for _, c := range cases {
		if got := virtualDisplayTeardownNeeded(c.goos, c.launched, c.bin); got != c.want {
			t.Errorf("virtualDisplayTeardownNeeded(%q, %v, %q) = %v, want %v", c.goos, c.launched, c.bin, got, c.want)
		}
	}
}

func TestRustshineStop_TearsDownVirtualDisplayOnce(t *testing.T) {
	calls := stubTeardown(t)
	proc := &fakeRustshineProcess{}
	b := &rustshineBackend{proc: proc, launchPath: `C:\x\usbridge-streamer.exe`, launchedWithVirtualDisplay: true}

	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if proc.killed != 1 {
		t.Fatalf("process killed %d times, want 1", proc.killed)
	}
	wantCalls := 0
	if runtime.GOOS == "windows" {
		wantCalls = 1
	}
	if len(*calls) != wantCalls {
		t.Fatalf("teardown ran %d times, want %d (GOOS=%s)", len(*calls), wantCalls, runtime.GOOS)
	}
	if wantCalls == 1 && (*calls)[0] != `C:\x\usbridge-streamer.exe` {
		t.Fatalf("teardown ran against %q, want the binary Stop just killed", (*calls)[0])
	}
	// Cleared so a later Stop() -- e.g. a restart that relaunched without a
	// virtual display -- doesn't tear down again. (Not exercised with a
	// second real Stop(): with no proc it takes the orphan path, which
	// taskkills every usbridge-streamer.exe on the machine.)
	if b.launchedWithVirtualDisplay {
		t.Fatal("Stop must clear launchedWithVirtualDisplay")
	}
}

func TestRustshineStop_NoTeardownWithoutVirtualDisplay(t *testing.T) {
	calls := stubTeardown(t)
	b := &rustshineBackend{proc: &fakeRustshineProcess{}, launchPath: `C:\x\usbridge-streamer.exe`}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("teardown ran for a launch without a virtual display: %v", *calls)
	}
}

// TestDefaultVirtualDisplayTeardown_RealBinary runs the real one-shot helper
// against a built streamer: USBRIDGE_STREAMER_BIN=<path to
// usbridge-streamer.exe> go test ./internal/streamhost -run RealBinary
func TestDefaultVirtualDisplayTeardown_RealBinary(t *testing.T) {
	bin := os.Getenv("USBRIDGE_STREAMER_BIN")
	if bin == "" {
		t.Skip("set USBRIDGE_STREAMER_BIN to run against a real streamer binary")
	}
	if err := defaultVirtualDisplayTeardown(bin); err != nil {
		t.Fatalf("--teardown-virtual-display: %v", err)
	}
}
