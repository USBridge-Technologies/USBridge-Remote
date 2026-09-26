//go:build !devstreamer

package streamhost

import (
	"os"
	"path/filepath"
	"testing"
)

// Release builds must never honour the dev override, whatever the env says.
func TestReleaseBuildIgnoresDevStreamerEnv(t *testing.T) {
	stateDir := t.TempDir()
	staged := filepath.Join(stateDir, "usbridge-streamer", binaryName())
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("staged"), 0o755); err != nil {
		t.Fatal(err)
	}
	dev := filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(dev, []byte("dev"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("USBRIDGE_DEV_STREAMER", dev)
	b := NewRustshine(t.TempDir(), stateDir, "").(*rustshineBackend)
	if got := b.BinaryPath(); got != staged {
		t.Fatalf("BinaryPath() = %q, want staged %q (override must be compiled out)", got, staged)
	}
}
