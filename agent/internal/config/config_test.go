package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirIsUsable_EmptyIsFalse(t *testing.T) {
	if DirIsUsable("") || DirIsUsable("   ") {
		t.Fatal("empty dir must not be usable")
	}
}

func TestDirIsUsable_WritableTemp(t *testing.T) {
	dir := t.TempDir()
	if !DirIsUsable(dir) {
		t.Fatalf("temp dir %s should be usable", dir)
	}
	if !DirIsUsable(filepath.Join(dir, "nested", "state")) {
		t.Fatal("MkdirAll of a nested writable path should succeed")
	}
}

func TestDirIsUsable_BlockedByFile(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if DirIsUsable(blocked) {
		t.Fatal("a regular file must not count as a usable state dir")
	}
}
