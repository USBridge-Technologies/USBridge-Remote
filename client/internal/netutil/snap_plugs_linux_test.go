//go:build linux && !android

package netutil

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeSnapctl puts a snapctl on PATH that says camera is connected, can't read
// alsa, and has every other plug disconnected.
func fakeSnapctl(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$2" in
camera) exit 0 ;;
alsa) echo "error: cannot read" >&2; exit 1 ;;
*) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "snapctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestSnapMissingPlugs(t *testing.T) {
	fakeSnapctl(t)
	t.Setenv("SNAP", "/snap/usbridge-client/x1")
	want := []string{"audio-record", "joystick", "raw-usb", "network-manager", "network-observe", "hardware-observe", "removable-media"}
	if got := SnapMissingPlugs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SnapMissingPlugs() = %v, want %v", got, want)
	}
}

func TestSnapMissingPlugsOutsideSnap(t *testing.T) {
	fakeSnapctl(t)
	t.Setenv("SNAP", "")
	if got := SnapMissingPlugs(); got != nil {
		t.Fatalf("SnapMissingPlugs() = %v outside a snap", got)
	}
}

func TestSnapMissingPlugsWithoutSnapctl(t *testing.T) {
	t.Setenv("SNAP", "/snap/usbridge-client/x1")
	t.Setenv("PATH", t.TempDir())
	if got := SnapMissingPlugs(); got != nil {
		t.Fatalf("SnapMissingPlugs() = %v without snapctl", got)
	}
}

func TestSnapConnectCommandIsOneLine(t *testing.T) {
	got := SnapConnectCommand([]string{"camera", "alsa"})
	want := "sudo snap connect usbridge-client:camera && sudo snap connect usbridge-client:alsa"
	if got != want {
		t.Fatalf("SnapConnectCommand = %q, want %q", got, want)
	}
}

// The store description lists the same commands, and every plug must exist in the snap.
func TestSnapManualPlugsMatchSnapcraft(t *testing.T) {
	data, err := os.ReadFile("../../snap/snapcraft.yaml")
	if err != nil {
		t.Fatal(err)
	}
	yaml := string(data)
	for _, plug := range snapManualPlugs {
		if !strings.Contains(yaml, "      - "+plug+"\n") {
			t.Errorf("%s isn't a plug of the snap", plug)
		}
		if !strings.Contains(yaml, "sudo snap connect usbridge-client:"+plug+" ") {
			t.Errorf("the description doesn't tell how to connect %s", plug)
		}
	}
}
