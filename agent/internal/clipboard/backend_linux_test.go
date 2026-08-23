//go:build linux && !android

package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestRunCmdDoesNotHangOnDaemonizingProcess reproduces the live bug behind
// "clipboard stopped working in both directions": xclip's "set" invocation
// forks a background process to keep serving the X11 selection, which
// inherits our Stdout pipe -- Go's Cmd.Wait() then blocks until that pipe
// sees EOF, which never happens as long as the daemonized process holds it
// open, *regardless* of the outer context's own timeout already having
// killed the directly-exec'd process. Confirmed live against the real
// xclip binary: without cmd.WaitDelay, a single "xclip -selection
// clipboard <text" call hung for 100+ seconds; Manager.Apply/Run both hold
// backendMu around runCmd, so one hung call froze clipboard sync in both
// directions. This fake script reproduces the same fd-inheritance shape
// (a backgrounded subshell that outlives the script and keeps stdout open)
// without depending on a real X11/Wayland session being available.
func TestRunCmdDoesNotHangOnDaemonizingProcess(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\ncat >/dev/null\n(sleep 5) &\nexit 0\n"
	path := filepath.Join(dir, "daemonizing-tool")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tool: %v", err)
	}

	start := time.Now()
	_, err := runCmd(context.Background(), path, nil, []byte("hello"))
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Fatalf("runCmd took %v against a daemonizing process, want well under 3s (WaitDelay should cap this)", elapsed)
	}
	if err != nil {
		t.Fatalf("runCmd returned an error for a process that exited successfully: %v (ErrWaitDelay must be treated as success)", err)
	}
}

// writeFakeXclipWithTargets writes a fake `xclip` whose "-o -t TARGETS"
// response is exactly `targets` (not derived from what was "set"), so a
// test can simulate xclip's real, confirmed-live misbehavior: answering a
// getMime(mime) request with real data even when that mime was never
// actually offered. Any other invocation (get without -t, or a set with
// stdin) echoes back / stores `content` unconditionally, matching a real
// xclip selection owner that doesn't validate the requested target at all.
func writeFakeXclipWithTargets(t *testing.T, dir, targets, content string) string {
	t.Helper()
	path := filepath.Join(dir, "xclip")
	script := fmt.Sprintf(
		"#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = \"TARGETS\" ] && { printf '%s'; exit 0; }; done\nprintf '%s'\n",
		targets, content,
	)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake xclip: %v", err)
	}
	return path
}

// TestXclipGetMimeRejectsUnofferedType reproduces the live bug behind
// plain-text clipboard content getting silently misdelivered as a bogus
// "image": xclip's selection owner doesn't validate the requested target at
// all -- confirmed live, "echo -n hi | xclip -selection clipboard" then
// "xclip -o -t image/png" prints "hi" instead of refusing. linuxBackend.Read
// checks image/png (and text/uri-list) *before* falling back to plain text,
// so without cross-checking TARGETS first, xclipTool.getMime always
// (wrongly) "succeeded" for image/png whenever xclip held plain text,
// making Read() report KindImage garbage for ordinary text -- breaking
// clipboard sync any time xclip ended up answering that call (dualTool's
// secondary, whenever wl-clipboard correctly refused first).
func TestXclipGetMimeRejectsUnofferedType(t *testing.T) {
	dir := t.TempDir()
	// TARGETS lists only UTF8_STRING (plain text) -- image/png was never
	// actually offered, even though this fake (like the real xclip binary)
	// would happily echo the text back if asked for it directly.
	path := writeFakeXclipWithTargets(t, dir, "TARGETS\\nUTF8_STRING", "just plain text")
	tool := xclipTool{path: path}

	data, ok, err := tool.getMime(context.Background(), mimePNG)
	if err != nil {
		t.Fatalf("getMime: unexpected error %v", err)
	}
	if ok {
		t.Fatalf("getMime(image/png) = %q, ok=true; want ok=false since TARGETS never offered it", data)
	}
}

// TestXclipGetMimeAcceptsOfferedType is the positive-path sibling of the
// above: a mime that genuinely *is* listed in TARGETS must still work.
func TestXclipGetMimeAcceptsOfferedType(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeXclipWithTargets(t, dir, "TARGETS\\nimage/png", "fake-png-bytes")
	tool := xclipTool{path: path}

	data, ok, err := tool.getMime(context.Background(), mimePNG)
	if err != nil || !ok {
		t.Fatalf("getMime(image/png): data=%q ok=%v err=%v, want ok=true", data, ok, err)
	}
	if string(data) != "fake-png-bytes" {
		t.Errorf("getMime(image/png) = %q, want %q", data, "fake-png-bytes")
	}
}
