package api

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveLocalUIPathPrefersBundleThenFlatThenFallback pins the
// priority order InitLocalUIParseFromConfig relies on to find a packaged
// build's bundled ONNX models/runtime lib without any user setup: a
// macOS-.app-relative candidate first, then a flat-next-to-the-executable
// candidate, and only the ~/.usbridge/localui fallback if neither exists on
// disk (see build_macos.sh + fetch_onnxruntime.sh for how those get there).
func TestResolveLocalUIPathPrefersBundleThenFlatThenFallback(t *testing.T) {
	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable(): %v", err)
	}
	execDir := filepath.Dir(execPath)

	fallback := filepath.Join(t.TempDir(), "fallback-file")
	if err := os.WriteFile(fallback, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Neither candidate exists yet -- must fall through to fallback.
	bundleRel := "usbridge_test_bundle_candidate_" + t.Name()
	flatRel := "usbridge_test_flat_candidate_" + t.Name()
	if got := resolveLocalUIPath(bundleRel, flatRel, fallback); got != fallback {
		t.Fatalf("resolveLocalUIPath() = %q, want fallback %q when nothing is bundled", got, fallback)
	}

	// Create the flat candidate next to the test binary -- must now win over fallback.
	flatPath := filepath.Join(execDir, flatRel)
	if err := os.WriteFile(flatPath, []byte("x"), 0o644); err != nil {
		t.Skipf("cannot write next to test binary at %s: %v (sandboxed/read-only test dir)", execDir, err)
	}
	defer os.Remove(flatPath)
	if got := resolveLocalUIPath(bundleRel, flatRel, fallback); got != flatPath {
		t.Errorf("resolveLocalUIPath() = %q, want flat candidate %q once it exists", got, flatPath)
	}

	// Create the bundle candidate too -- must now win over the flat one (checked first).
	bundlePath := filepath.Join(execDir, bundleRel)
	if err := os.WriteFile(bundlePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write bundle candidate: %v", err)
	}
	defer os.Remove(bundlePath)
	if got := resolveLocalUIPath(bundleRel, flatRel, fallback); got != bundlePath {
		t.Errorf("resolveLocalUIPath() = %q, want bundle candidate %q to take priority over the flat one", got, bundlePath)
	}
}
