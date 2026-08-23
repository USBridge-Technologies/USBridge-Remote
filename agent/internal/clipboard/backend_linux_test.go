//go:build linux && !android

package clipboard

import (
	"os"
	"path/filepath"
	"testing"
)

// resetDetectState clears detect()'s package-level cache so each subtest
// starts from a clean slate -- these fields would otherwise leak state
// between tests (and between a real process's own calls) by design, see
// detect()'s doc comment.
func resetDetectState(t *testing.T) {
	t.Helper()
	detectMu.Lock()
	activeTool = nil
	loggedNoTool = false
	detectMu.Unlock()
	t.Cleanup(func() {
		detectMu.Lock()
		activeTool = nil
		loggedNoTool = false
		detectMu.Unlock()
	})
}

// writeFakeExecutable creates an executable file named `name` inside dir so
// exec.LookPath(name) succeeds against a PATH containing dir, without
// depending on whatever clipboard tools happen to be installed on the
// machine running `go test`.
func writeFakeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

// TestDetectDoesNotCacheNotFoundForever reproduces the bug behind clipboard
// sync staying broken even after xclip is installed while the agent is
// already running (via the UI's "Install" button, or manually in a
// terminal): detect() used to cache its result -- including a *negative*
// one -- for the whole process lifetime via sync.Once, so a tool installed
// after the first (inevitably early, near-startup) detect() call was never
// picked up without a full agent restart. This verifies a later call, once
// a tool actually appears on PATH, finds it instead of returning the
// earlier "not found".
func TestDetectDoesNotCacheNotFoundForever(t *testing.T) {
	resetDetectState(t)
	t.Setenv("WAYLAND_DISPLAY", "")

	empty := t.TempDir()
	t.Setenv("PATH", empty)
	if got := detect(); got != nil {
		t.Fatalf("detect() with nothing on PATH = %#v, want nil", got)
	}

	withXclip := t.TempDir()
	writeFakeExecutable(t, withXclip, "xclip")
	t.Setenv("PATH", withXclip)

	got := detect()
	if _, ok := got.(xclipTool); !ok {
		t.Fatalf("detect() after xclip appeared on PATH = %#v, want xclipTool", got)
	}
}

// TestDetectCachesOnceFound ensures a successful detection *is* still
// cached (not re-probed on every call) -- detect() is called on every
// clipboard poll tick (~800ms, see manager.go's pollInterval), and a found
// binary doesn't uninstall itself mid-session, so re-running exec.LookPath
// forever once satisfied would just be wasted work.
func TestDetectCachesOnceFound(t *testing.T) {
	resetDetectState(t)
	t.Setenv("WAYLAND_DISPLAY", "")

	dir := t.TempDir()
	writeFakeExecutable(t, dir, "xclip")
	t.Setenv("PATH", dir)

	first := detect()
	if _, ok := first.(xclipTool); !ok {
		t.Fatalf("detect() = %#v, want xclipTool", first)
	}

	// Empty the PATH entirely -- if detect() were re-probing instead of
	// returning its cache, it would now (wrongly) report nothing found.
	t.Setenv("PATH", t.TempDir())
	second := detect()
	if _, ok := second.(xclipTool); !ok {
		t.Fatalf("detect() after PATH changed = %#v, want cached xclipTool", second)
	}
}
