package usbpass

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPromoteBrokerSidecar(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "usbridge-usb-broker.exe")
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			return ""
		}
		return string(b)
	}

	write(exe, "old")
	if got := promoteBrokerSidecar(exe); got != exe || read(exe) != "old" {
		t.Fatalf("no update: got %q, content %q", got, read(exe))
	}

	write(exe+".new", "new")
	write(exe+".old", "stale")
	if got := promoteBrokerSidecar(exe); got != exe {
		t.Fatalf("got %q, want %q", got, exe)
	}
	if read(exe) != "new" {
		t.Fatalf("content %q, want the update", read(exe))
	}
	for _, p := range []string{exe + ".new", exe + ".old"} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s left behind", filepath.Base(p))
		}
	}
}
