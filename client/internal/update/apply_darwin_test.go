//go:build darwin

package update

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppleScriptStringRoundTrip(t *testing.T) {
	s := `set -e; mv '/tmp/a b'\''c' "x\y"`
	out, err := exec.Command("osascript", "-e", "return "+appleScriptString(s)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSuffix(string(out), "\n"); got != s {
		t.Fatalf("got %q, want %q", got, s)
	}
}

func TestSwapScriptReplacesBundle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "it's here")
	newApp, target := makeBundles(t, dir)
	staged, backup := filepath.Join(filepath.Dir(target), ".s.app"), filepath.Join(filepath.Dir(target), ".p.app")
	if out, err := exec.Command("/bin/sh", "-c", swapScript(newApp, target, staged, backup)).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	checkSwapped(t, target)
}

func makeBundles(t *testing.T, dir string) (newApp, target string) {
	newApp = filepath.Join(dir, "new", appBundleName)
	target = filepath.Join(dir, "Applications", appBundleName)
	for p, body := range map[string]string{newApp: "new", target: "old"} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "v"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return newApp, target
}

func checkSwapped(t *testing.T, target string) {
	t.Helper()
	if b, _ := os.ReadFile(filepath.Join(target, "v")); string(b) != "new" {
		t.Fatalf("target holds %q", b)
	}
	if entries, _ := os.ReadDir(filepath.Dir(target)); len(entries) != 1 {
		t.Fatalf("leftovers next to target: %v", entries)
	}
}

func TestSwapAsUserReplacesBundle(t *testing.T) {
	newApp, target := makeBundles(t, filepath.Join(t.TempDir(), "it's here"))
	err := swapAsUser(context.Background(), newApp, target,
		filepath.Join(filepath.Dir(target), ".staged.app"), filepath.Join(filepath.Dir(target), ".prev.app"))
	if err != nil {
		t.Fatal(err)
	}
	checkSwapped(t, target)
}

func TestSwapAsUserReadOnlyIsPermissionError(t *testing.T) {
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	os.MkdirAll(ro, 0o755)
	newApp := filepath.Join(dir, appBundleName)
	os.MkdirAll(newApp, 0o755)
	os.Chmod(ro, 0o555)
	defer os.Chmod(ro, 0o755)
	err := swapAsUser(context.Background(), newApp, filepath.Join(ro, appBundleName),
		filepath.Join(ro, ".s.app"), filepath.Join(ro, ".p.app"))
	if err == nil || !isPermissionError(err) {
		t.Fatalf("want permission error, got %v", err)
	}
}
