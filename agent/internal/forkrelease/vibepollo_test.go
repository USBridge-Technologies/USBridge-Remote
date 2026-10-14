package forkrelease

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, names ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("x"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

// The portable zip's members sit under one top directory, whatever cpack or the CI step
// named it; extraction strips it so sunshine.exe lands at the root of the staged tree.
func TestZipTopDir(t *testing.T) {
	if got := zipTopDir(writeZip(t, "Vibepollo/sunshine.exe", "Vibepollo/assets/a.json")); got != "Vibepollo/" {
		t.Fatalf("single top dir: got %q", got)
	}
	if got := zipTopDir(writeZip(t, "sunshine.exe", "assets/a.json")); got != "" {
		t.Fatalf("members at the root: got %q, want no strip", got)
	}
	if got := zipTopDir(writeZip(t, "A/x", "B/y")); got != "" {
		t.Fatalf("two top dirs: got %q, want no strip", got)
	}
}

func TestVibepolloStagedPaths(t *testing.T) {
	dir := t.TempDir()
	if VibepolloStaged(dir) {
		t.Fatal("nothing staged yet")
	}
	if VibepolloStagedVersion(dir) != "" {
		t.Fatal("no version yet")
	}
	// Distinct from Sunshine's staged tree: the two never share a directory.
	if VibepolloDir(dir) == SunshineDir(dir) {
		t.Fatal("Vibepollo and Sunshine must stage in different directories")
	}
}
