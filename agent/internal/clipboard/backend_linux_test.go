//go:build linux && !android

package clipboard

import (
	"context"
	"errors"
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

// TestProbeToolPrefersDualOnWayland reproduces the second half of the same
// live bug report: with only xclip installed, an XWayland app's clipboard
// writes didn't reliably reach native Wayland apps' paste on a real
// KDE/kwin_wayland session. Once both wl-clipboard and xclip are on PATH
// under a Wayland session, probeTool must wrap both in a dualTool instead of
// picking just one, so both selections get written on every apply.
func TestProbeToolPrefersDualOnWayland(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "wl-copy")
	writeFakeExecutable(t, dir, "wl-paste")
	writeFakeExecutable(t, dir, "xclip")
	t.Setenv("PATH", dir)

	got := probeTool()
	if _, ok := got.(dualTool); !ok {
		t.Fatalf("probeTool() with wl-clipboard+xclip on a Wayland PATH = %#v, want dualTool", got)
	}
}

// fakeClipboardTool is a minimal in-memory clipboardTool for exercising
// dualTool's fan-out logic without shelling out to real CLI tools.
type fakeClipboardTool struct {
	text    string
	hasText bool
	setErr  error
}

func (f *fakeClipboardTool) supportsMime() bool                          { return true }
func (f *fakeClipboardTool) targetsHash(context.Context) (string, error) { return f.text, nil }
func (f *fakeClipboardTool) getMime(context.Context, string) ([]byte, bool, error) {
	return nil, false, nil
}
func (f *fakeClipboardTool) setMime(context.Context, string, []byte) error { return nil }

func (f *fakeClipboardTool) getText(context.Context) (string, bool, error) {
	return f.text, f.hasText, nil
}

func (f *fakeClipboardTool) setText(_ context.Context, text string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.text, f.hasText = text, true
	return nil
}

// TestDualToolSetTextWritesBothSides is the core guarantee dualTool exists
// for: an apply must land on both the primary and secondary selection, not
// just whichever one happened to already have content -- otherwise an app
// reading the other selection never sees it, exactly the failure mode
// reported live (xclip alone left native-Wayland-app pastes empty).
func TestDualToolSetTextWritesBothSides(t *testing.T) {
	primary := &fakeClipboardTool{}
	secondary := &fakeClipboardTool{}
	dt := dualTool{primary: primary, secondary: secondary}

	if err := dt.setText(context.Background(), "hello"); err != nil {
		t.Fatalf("setText: %v", err)
	}
	if primary.text != "hello" {
		t.Errorf("primary.text = %q, want %q", primary.text, "hello")
	}
	if secondary.text != "hello" {
		t.Errorf("secondary.text = %q, want %q", secondary.text, "hello")
	}
}

// TestDualToolSetTextReturnsPrimaryErrorButStillWritesSecondary ensures one
// side failing (e.g. wl-copy erroring because no compositor is attached to
// this particular headless test) doesn't silently drop the write on the
// other, still-working side.
func TestDualToolSetTextReturnsPrimaryErrorButStillWritesSecondary(t *testing.T) {
	primary := &fakeClipboardTool{setErr: errors.New("boom")}
	secondary := &fakeClipboardTool{}
	dt := dualTool{primary: primary, secondary: secondary}

	if err := dt.setText(context.Background(), "hello"); err == nil {
		t.Fatal("setText: want error surfaced from primary, got nil")
	}
	if secondary.text != "hello" {
		t.Errorf("secondary.text = %q, want %q (secondary write must not be skipped)", secondary.text, "hello")
	}
}

// TestDualToolGetTextFallsBackToSecondary covers content that an external
// app only wrote to one side (the realistic case: a native Wayland app sets
// the Wayland selection only, an XWayland/X11 app sets the X11 selection
// only) -- dualTool must still surface it instead of only ever looking at
// primary.
func TestDualToolGetTextFallsBackToSecondary(t *testing.T) {
	primary := &fakeClipboardTool{}
	secondary := &fakeClipboardTool{text: "only-on-secondary", hasText: true}
	dt := dualTool{primary: primary, secondary: secondary}

	text, ok, err := dt.getText(context.Background())
	if err != nil || !ok {
		t.Fatalf("getText: text=%q ok=%v err=%v", text, ok, err)
	}
	if text != "only-on-secondary" {
		t.Errorf("getText() = %q, want %q", text, "only-on-secondary")
	}
}
