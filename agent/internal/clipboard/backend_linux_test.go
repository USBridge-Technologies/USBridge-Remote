//go:build linux && !android

package clipboard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// resetDetectState clears detect()'s one piece of persistent state (the
// "already logged 'not found' once" flag) so each subtest starts from a
// clean slate -- it would otherwise leak between tests (and between a real
// process's own calls) by design, see detect()'s doc comment. detect()
// itself no longer caches the *tool* it finds -- every call re-probes.
func resetDetectState(t *testing.T) {
	t.Helper()
	logMu.Lock()
	loggedNoTool = false
	logMu.Unlock()
	t.Cleanup(func() {
		logMu.Lock()
		loggedNoTool = false
		logMu.Unlock()
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

// TestDetectUpgradesFromSingleToolToDualWithoutRestart reproduces the live
// bug report this replaced sync.Once-style caching entirely to fix: xclip
// alone gets found and (with any positive caching at all) locked in: later
// installing wl-clipboard too -- via the UI's "Install" button, while the
// agent is still running -- must upgrade the very next poll tick to a
// dualTool that actually writes both selections, not keep silently using
// just the xclip it found first.
func TestDetectUpgradesFromSingleToolToDualWithoutRestart(t *testing.T) {
	resetDetectState(t)
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	dir := t.TempDir()
	writeFakeExecutable(t, dir, "xclip")
	t.Setenv("PATH", dir)

	first := detect()
	if _, ok := first.(xclipTool); !ok {
		t.Fatalf("detect() with only xclip on PATH = %#v, want xclipTool", first)
	}

	writeFakeExecutable(t, dir, "wl-copy")
	writeFakeExecutable(t, dir, "wl-paste")

	second := detect()
	if _, ok := second.(dualTool); !ok {
		t.Fatalf("detect() after wl-clipboard also appeared = %#v, want dualTool", second)
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

// writeFakeXclip writes a fake `xclip` that reproduces real xclip's exact
// targetsHash-breaking behavior: `-o -t TARGETS` always prints the same
// fixed type-name list regardless of what's actually on the clipboard, `-o`
// alone prints whatever was last "set" (stored in a state file next to the
// script), and no `-o` (stdin present) overwrites that state file --
// mirroring xclip -selection clipboard's real set/get split. Confirmed live
// against the real binary: two different plain-text copies produce
// byte-identical `-o -t TARGETS` output, which is exactly the bug
// xclipTool.targetsHash's extra getText() call exists to work around.
func writeFakeXclip(t *testing.T, dir string) {
	t.Helper()
	state := filepath.Join(dir, "xclip.state")
	// Two-pass arg scan, not a single loop with early exit: the real
	// targetsHash call's args (-selection clipboard -o -t TARGETS) contain
	// *both* "-o" and "TARGETS", and TARGETS must win -- checking "-o" first
	// would misclassify that call as a plain getText.
	script := "#!/bin/sh\n" +
		"has_targets=0; has_o=0\n" +
		"for a in \"$@\"; do\n" +
		"  [ \"$a\" = \"TARGETS\" ] && has_targets=1\n" +
		"  [ \"$a\" = \"-o\" ] && has_o=1\n" +
		"done\n" +
		"if [ \"$has_targets\" = 1 ]; then printf 'TARGETS\\nSTRING\\nUTF8_STRING'; exit 0; fi\n" +
		"if [ \"$has_o\" = 1 ]; then cat '" + state + "' 2>/dev/null; exit 0; fi\n" +
		"cat > '" + state + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "xclip"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake xclip: %v", err)
	}
}

// TestXclipTargetsHashDiffersAcrossDifferentText reproduces the live bug
// report behind "copying text on the host sends nothing to the client a
// second time": xclip's `-o -t TARGETS` output only lists offered MIME type
// *names*, which stay identical across any two plain-text copies, so a
// targetsHash built from that alone never changes between them and the
// poll loop's stamp!=lastStamp check silently never fires for the second
// (or any later) same-kind text copy.
func TestXclipTargetsHashDiffersAcrossDifferentText(t *testing.T) {
	dir := t.TempDir()
	writeFakeXclip(t, dir)
	tool := xclipTool{path: filepath.Join(dir, "xclip")}
	ctx := context.Background()

	if err := tool.setText(ctx, "AAAA"); err != nil {
		t.Fatalf("setText: %v", err)
	}
	hash1, err := tool.targetsHash(ctx)
	if err != nil {
		t.Fatalf("targetsHash: %v", err)
	}

	if err := tool.setText(ctx, "BBBBBBBB-different"); err != nil {
		t.Fatalf("setText: %v", err)
	}
	hash2, err := tool.targetsHash(ctx)
	if err != nil {
		t.Fatalf("targetsHash: %v", err)
	}

	if hash1 == hash2 {
		t.Fatalf("targetsHash unchanged across different text copies: both %q", hash1)
	}
}
