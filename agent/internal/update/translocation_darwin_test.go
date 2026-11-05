//go:build darwin && !ios

package update

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRemoveQuarantine(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), appBundleName)
	exe := filepath.Join(bundle, "Contents", "MacOS", "x")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	flag := []byte("0083;6ae27f59;Chrome;")
	for _, p := range []string{bundle, exe} {
		if err := unix.Setxattr(p, quarantineAttr, flag, 0); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := removeQuarantine(bundle)
	if err != nil || !removed {
		t.Fatalf("first pass: removed=%v err=%v", removed, err)
	}
	for _, p := range []string{bundle, exe} {
		if _, err := unix.Getxattr(p, quarantineAttr, nil); err != unix.ENOATTR {
			t.Fatalf("%s still quarantined (%v)", p, err)
		}
	}
	// Nothing left to remove: no relaunch, so no loop.
	if removed, err := removeQuarantine(bundle); err != nil || removed {
		t.Fatalf("second pass: removed=%v err=%v", removed, err)
	}
}
