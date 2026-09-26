//go:build devstreamer

package streamhost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDevStreamerOverrideWinsOverStagedBinary(t *testing.T) {
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

	b := NewRustshine(t.TempDir(), stateDir, "").(*rustshineBackend)
	t.Setenv(devStreamerEnv, dev)
	if got := b.BinaryPath(); got != dev {
		t.Fatalf("BinaryPath() = %q, want dev override %q", got, dev)
	}

	t.Setenv(devStreamerEnv, filepath.Join(t.TempDir(), "missing"))
	if got := b.BinaryPath(); got != staged {
		t.Fatalf("BinaryPath() with a missing override = %q, want staged %q", got, staged)
	}
}
