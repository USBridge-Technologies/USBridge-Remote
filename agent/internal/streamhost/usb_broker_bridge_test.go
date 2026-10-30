//go:build !windows

package streamhost

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A patched host answers the probe; a stock one fails it. The answer is
// re-asked when the binary is replaced.
func TestStreamerHasUSBBridge(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "punktfunk-host")
	write := func(script string, mod time.Time) {
		t.Helper()
		if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(bin, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	t0 := time.Now().Add(-time.Hour).Truncate(time.Second)

	write(`echo "unknown command '$1' (try --help)" >&2; exit 1`, t0)
	if streamerHasUSBBridge(bin, "usbridge-bridge", nil) {
		t.Error("a stock punktfunk-host must not report the bridge")
	}

	write(`[ "$1" = usbridge-bridge ] && echo "usbridge-bridge 1"`, t0.Add(time.Minute))
	if !streamerHasUSBBridge(bin, "usbridge-bridge", nil) {
		t.Error("a patched punktfunk-host must report the bridge")
	}
	if runtime.GOOS == "linux" {
		// Punktfunk is bundled on Linux (and Windows) only: elsewhere it has no
		// binary to ask (punktfunkBinaryPath).
		b := &punktfunkBackend{}
		t.Setenv("PATH", t.TempDir())
		usePunktfunkStage(t, bin)
		if !b.RawHIDSupported() {
			t.Error("RawHIDSupported should follow the probe")
		}
	}

	// Sunshine is asked with its own switch.
	sunshine := filepath.Join(t.TempDir(), "sunshine")
	if err := os.WriteFile(sunshine, []byte("#!/bin/sh\n[ \"$1\" = --usbridge-bridge ] && echo \"usbridge-bridge 1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !(&sunshineBackend{launchPath: sunshine}).RawHIDSupported() {
		t.Error("a patched Sunshine must report the bridge")
	}
	if (&sunshineBackend{}).RawHIDSupported() {
		t.Error("no Sunshine binary, no bridge")
	}

	if streamerHasUSBBridge("", "usbridge-bridge", nil) || streamerHasUSBBridge(filepath.Join(t.TempDir(), "missing"), "usbridge-bridge", nil) {
		t.Error("no binary, no bridge")
	}
}
