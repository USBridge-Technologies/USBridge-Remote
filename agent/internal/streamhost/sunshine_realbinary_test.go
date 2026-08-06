package streamhost

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Manual, opt-in end-to-end check against the REAL bundled Sunshine binary
// (not run in normal `go test ./...` — set USBRIDGE_SUNSHINE_BIN to the
// extracted binary path to run it). Confirms sunshineConfigArgs() actually
// achieves full isolation against the real binary, not just against our own
// assumptions about its CLI parsing.
func TestSunshineConfigArgs_RealBinaryFullIsolation(t *testing.T) {
	bin := os.Getenv("USBRIDGE_SUNSHINE_BIN")
	if bin == "" {
		t.Skip("set USBRIDGE_SUNSHINE_BIN to run this against a real extracted Sunshine binary")
	}

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	if err := os.MkdirAll(filepath.Join(fakeHome, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}

	b := &sunshineBackend{stateDir: t.TempDir(), launchPath: bin}
	if err := os.MkdirAll(b.sunshineDataDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	args := append(b.sunshineConfigArgs(), "--creds", "realtestuser", "realtestpass123")
	cmd := exec.Command(bin, args...)
	cmd.Dir = filepath.Dir(bin)
	out, err := cmd.CombinedOutput()
	t.Logf("sunshine output:\n%s", out)
	if err != nil {
		t.Fatalf("--creds run failed: %v", err)
	}

	if _, err := os.Stat(b.credentialsFilePath()); err != nil {
		t.Errorf("expected sunshine_state.json at %s: %v", b.credentialsFilePath(), err)
	}

	legacyDir := filepath.Join(fakeHome, ".config", "sunshine")
	entries, err := os.ReadDir(legacyDir)
	if err == nil && len(entries) > 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("real Sunshine binary wrote into the legacy default dir %s despite explicit overrides: %v", legacyDir, names)
	}
}
