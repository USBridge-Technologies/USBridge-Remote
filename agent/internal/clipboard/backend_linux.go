//go:build linux && !android

package clipboard

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// mimePNG / mimeURIList are the clipboard targets used for images and file
// lists, matching the freedesktop.org conventions both X11 clipboard
// managers (via xclip's arbitrary -t target) and Wayland compositors (via
// wl-clipboard's --type) understand.
const (
	mimePNG     = "image/png"
	mimeURIList = "text/uri-list"
)

// clipboardTool abstracts over whichever CLI clipboard helper is installed.
// Shelling out mirrors this repo's existing style for OS integration (the
// agent's screen capture already shells out to macOS's screencapture — see
// capture/screen_darwin.go); no portable X11/Wayland clipboard protocol
// library is vendored here, and hand-rolling ICCCM selection ownership is
// far more code than this is worth.
type clipboardTool interface {
	supportsMime() bool
	// targetsHash returns a cheap fingerprint of the currently available
	// clipboard target/mime list, used purely for change detection so the
	// poll loop doesn't have to pull full clipboard content every tick.
	targetsHash(ctx context.Context) (string, error)
	getText(ctx context.Context) (string, bool, error)
	setText(ctx context.Context, text string) error
	getMime(ctx context.Context, mime string) ([]byte, bool, error)
	setMime(ctx context.Context, mime string, data []byte) error
}

// runCmd's whole clipboard-tool-hangs-forever story: xclip/wl-copy "set"
// invocations fork into the background to keep serving the selection after
// the process we exec'd exits, and inherit our Stdout pipe in the process.
// Confirmed live: with only the outer 3s context timeout below, "xclip
// -selection clipboard <text" reliably hung for 100+ seconds -- the timeout
// does kill the direct child, but Go's Cmd.Wait() still blocks forever
// waiting for the *inherited stdout pipe* to see EOF, which never happens
// as long as the daemonized grandchild (the one actually holding the X11
// selection open) keeps that fd around. Manager.Apply/Run both hold
// backendMu around exactly this call, so one hung xclip invocation froze
// clipboard sync end to end -- both directions, exactly the live symptom
// this fixes.
//
// cmd.WaitDelay (Go 1.20+) is the sanctioned fix for this: once the
// process itself has exited, Wait() only waits this much longer for the
// I/O pipes to close before force-closing them itself and returning
// exec.ErrWaitDelay -- confirmed live: with WaitDelay set, the same call
// returns in ~1s instead of hanging. Per ErrWaitDelay's own doc, it's only
// ever returned when the process "exits with a successful status code" --
// so it's not a real failure, just Go giving up on waiting for a daemon
// that was never going to close that fd; treated as success below rather
// than surfaced as a set/get failure to callers.
func runCmd(ctx context.Context, path string, args []string, stdin []byte) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, path, args...)
	cmd.WaitDelay = 1 * time.Second
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return nil, err
	}
	return out.Bytes(), nil
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// xclipTool drives xclip, available on essentially every X11 desktop and
// the only common CLI tool that supports arbitrary clipboard targets (so it
// covers text, images, and file lists).
type xclipTool struct{ path string }

func (t xclipTool) supportsMime() bool { return true }

func (t xclipTool) targetsHash(ctx context.Context) (string, error) {
	out, err := runCmd(ctx, t.path, []string{"-selection", "clipboard", "-o", "-t", "TARGETS"}, nil)
	if err != nil {
		// xclip exits non-zero when the clipboard is empty/has no owner —
		// that's a normal state, not a failure worth surfacing.
		return "", nil
	}
	return hashBytes(out), nil
}

func (t xclipTool) getText(ctx context.Context) (string, bool, error) {
	out, err := runCmd(ctx, t.path, []string{"-selection", "clipboard", "-o"}, nil)
	if err != nil {
		return "", false, nil
	}
	return string(out), len(out) > 0, nil
}

func (t xclipTool) setText(ctx context.Context, text string) error {
	_, err := runCmd(ctx, t.path, []string{"-selection", "clipboard"}, []byte(text))
	return err
}

func (t xclipTool) getMime(ctx context.Context, mime string) ([]byte, bool, error) {
	// xclip's selection owner doesn't validate the requested target at all:
	// asking a plain-text clipboard for "-t image/png" (or any other mime
	// it never actually offered) just echoes back the text bytes instead of
	// refusing -- confirmed live: "echo -n hi | xclip -selection clipboard"
	// then "xclip -o -t image/png" prints "hi". Read()'s own mime-first
	// probing order (uri-list, then png, then plain text) means that bug
	// alone made linuxBackend.Read misclassify *every* plain-text clipboard
	// as KindImage whenever xclip ended up answering the getMime(png) call
	// (dualTool's secondary, whenever wl-clipboard correctly refused
	// first) -- silently breaking clipboard sync in both directions any
	// time the actual content was text. Cross-checking TARGETS (which
	// xclip *does* populate honestly -- confirmed live too) before trusting
	// the data closes this off.
	targets, err := runCmd(ctx, t.path, []string{"-selection", "clipboard", "-o", "-t", "TARGETS"}, nil)
	if err != nil || !bytes.Contains(targets, []byte(mime)) {
		return nil, false, nil
	}
	out, err := runCmd(ctx, t.path, []string{"-selection", "clipboard", "-o", "-t", mime}, nil)
	if err != nil || len(out) == 0 {
		return nil, false, nil
	}
	return out, true, nil
}

func (t xclipTool) setMime(ctx context.Context, mime string, data []byte) error {
	_, err := runCmd(ctx, t.path, []string{"-selection", "clipboard", "-t", mime}, data)
	return err
}

// wlTool drives wl-clipboard (wl-copy/wl-paste), the standard Wayland
// clipboard CLI; like xclip it supports arbitrary MIME types.
type wlTool struct{ copyPath, pastePath string }

func (t wlTool) supportsMime() bool { return true }

func (t wlTool) targetsHash(ctx context.Context) (string, error) {
	out, err := runCmd(ctx, t.pastePath, []string{"--list-types"}, nil)
	if err != nil {
		return "", nil
	}
	return hashBytes(out), nil
}

func (t wlTool) getText(ctx context.Context) (string, bool, error) {
	out, err := runCmd(ctx, t.pastePath, []string{"--no-newline"}, nil)
	if err != nil || len(out) == 0 {
		return "", false, nil
	}
	return string(out), true, nil
}

func (t wlTool) setText(ctx context.Context, text string) error {
	_, err := runCmd(ctx, t.copyPath, nil, []byte(text))
	return err
}

func (t wlTool) getMime(ctx context.Context, mime string) ([]byte, bool, error) {
	out, err := runCmd(ctx, t.pastePath, []string{"--type", mime}, nil)
	if err != nil || len(out) == 0 {
		return nil, false, nil
	}
	return out, true, nil
}

func (t wlTool) setMime(ctx context.Context, mime string, data []byte) error {
	_, err := runCmd(ctx, t.copyPath, []string{"--type", mime}, data)
	return err
}

// xselTool is the text-only fallback when neither xclip nor wl-clipboard is
// installed: xsel has no concept of arbitrary clipboard targets, so image
// and file clipboard sync degrade gracefully to unsupported rather than
// disabling clipboard sync entirely.
type xselTool struct{ path string }

func (t xselTool) supportsMime() bool { return false }

func (t xselTool) targetsHash(ctx context.Context) (string, error) {
	text, ok, err := t.getText(ctx)
	if err != nil || !ok {
		return "", nil
	}
	return hashBytes([]byte(text)), nil
}

func (t xselTool) getText(ctx context.Context) (string, bool, error) {
	out, err := runCmd(ctx, t.path, []string{"--clipboard", "--output"}, nil)
	if err != nil {
		return "", false, nil
	}
	return string(out), len(out) > 0, nil
}

func (t xselTool) setText(ctx context.Context, text string) error {
	_, err := runCmd(ctx, t.path, []string{"--clipboard", "--input"}, []byte(text))
	return err
}

func (t xselTool) getMime(context.Context, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (t xselTool) setMime(context.Context, string, []byte) error {
	return fmt.Errorf("clipboard: xsel does not support non-text clipboard formats (install xclip or wl-clipboard)")
}

// dualTool wraps two clipboardTools -- normally wl-clipboard as primary and
// xclip as secondary, see probeTool -- writing to and reading from both.
//
// Whether a given app's paste actually observes XWayland's bridged X11
// CLIPBOARD selection, versus the native Wayland selection, depends on the
// app's toolkit and on how well the compositor bridges the two -- not
// something this project can assume works. Confirmed live: on a real
// KDE/kwin_wayland session with only xclip installed, clipboard sync (both
// text and files, both directions) stayed silently broken despite
// kwin_wayland's Xwayland integration supposedly bridging the two
// automatically. Writing to both sides on every apply, and checking both on
// every read, means a paste works regardless of which bridge (if either) is
// actually functioning on a given desktop -- at the cost of running two
// tiny CLI calls instead of one, which is negligible next to the poll
// loop's own 800ms cadence.
type dualTool struct {
	primary, secondary clipboardTool
}

func (t dualTool) supportsMime() bool { return true }

// targetsHash combines both sides' hashes so a change on *either* selection
// (ours or an external app's) is detected, even if the two have drifted out
// of sync because whatever bridge might normally reconcile them isn't
// working.
func (t dualTool) targetsHash(ctx context.Context) (string, error) {
	h1, _ := t.primary.targetsHash(ctx)
	h2, _ := t.secondary.targetsHash(ctx)
	return hashBytes([]byte(h1 + "|" + h2)), nil
}

// getText/getMime try primary first, falling back to secondary -- content
// set by an external app normally only lands on whichever selection that
// app's toolkit actually uses, not both, so unlike setText/setMime this
// isn't "do both", it's "check both, use whichever has it".
func (t dualTool) getText(ctx context.Context) (string, bool, error) {
	if text, ok, err := t.primary.getText(ctx); ok {
		return text, ok, err
	}
	return t.secondary.getText(ctx)
}

func (t dualTool) setText(ctx context.Context, text string) error {
	err1 := t.primary.setText(ctx, text)
	err2 := t.secondary.setText(ctx, text)
	if err1 != nil {
		return err1
	}
	return err2
}

func (t dualTool) getMime(ctx context.Context, mime string) ([]byte, bool, error) {
	if data, ok, err := t.primary.getMime(ctx, mime); ok {
		return data, ok, err
	}
	return t.secondary.getMime(ctx, mime)
}

func (t dualTool) setMime(ctx context.Context, mime string, data []byte) error {
	err1 := t.primary.setMime(ctx, mime, data)
	err2 := t.secondary.setMime(ctx, mime, data)
	if err1 != nil {
		return err1
	}
	return err2
}

var (
	logMu        sync.Mutex
	loggedNoTool bool
)

// detect picks the clipboard CLI tool available on this session, preferring
// a dualTool (wl-clipboard + xclip together) under Wayland when both are
// installed, a single wl-clipboard or xclip when only one is, and falling
// back to xsel (text only) -- see probeTool.
//
// Every call re-probes from scratch rather than caching a result for the
// process's life: exec.LookPath only stats PATH's directories, no
// subprocess spawn, so this stays cheap even at the clipboard poll loop's
// ~800ms cadence (see manager.go's pollInterval). This isn't just about
// picking up a tool that wasn't there before -- caching *any* successful
// result, even just "found xclip", is its own bug: confirmed live,
// permissions.RequestClipboardTool (the UI's "Install" button) installing
// wl-clipboard *in addition to* an xclip that was already found and cached
// earlier never took effect without a full agent restart, because a cached
// non-nil result short-circuited before ever reconsidering whether a better
// combination (dualTool) had since become available. Re-probing every tick
// means any change in what's installed -- newly appearing, newly gone, or
// newly able to upgrade a single tool into a dualTool -- is picked up on
// the very next poll, no restart, no explicit cache invalidation needed
// anywhere.
func detect() clipboardTool {
	tool := probeTool()

	logMu.Lock()
	defer logMu.Unlock()
	if tool == nil {
		if !loggedNoTool {
			log.Printf("[clipboard] no xclip/xsel/wl-clipboard found; clipboard sync disabled on this Linux session")
			loggedNoTool = true
		}
	} else {
		loggedNoTool = false // if it later disappears again, log that too
	}
	return tool
}

// probeTool runs the actual PATH lookups behind detect(). Split out so
// detect() only has to hold detectMu around this call, not around any
// caller's use of the returned clipboardTool.
//
// On a Wayland session with both wl-clipboard and xclip installed, wraps
// both in a dualTool instead of picking just one -- see its doc comment for
// why relying on the compositor to bridge the two isn't good enough.
func probeTool() clipboardTool {
	var wl *wlTool
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		copyPath, errC := exec.LookPath("wl-copy")
		pastePath, errP := exec.LookPath("wl-paste")
		if errC == nil && errP == nil {
			t := wlTool{copyPath: copyPath, pastePath: pastePath}
			wl = &t
		}
	}
	var xc *xclipTool
	if p, err := exec.LookPath("xclip"); err == nil {
		t := xclipTool{path: p}
		xc = &t
	}

	switch {
	case wl != nil && xc != nil:
		return dualTool{primary: *wl, secondary: *xc}
	case wl != nil:
		return *wl
	case xc != nil:
		return *xc
	}

	if p, err := exec.LookPath("xsel"); err == nil {
		log.Printf("[clipboard] only xsel found (text-only); install xclip or wl-clipboard for image/file clipboard sync")
		return xselTool{path: p}
	}
	return nil
}

type linuxBackend struct{}

// NewBackend returns the Linux clipboard backend. winHandle is unused (the
// underlying tools talk to the X11/Wayland clipboard server directly, not a
// specific window) — see the darwin backend for why the parameter exists.
func NewBackend(winHandle any) Backend { return &linuxBackend{} }

func (b *linuxBackend) ChangeStamp() (string, error) {
	tool := detect()
	if tool == nil {
		return "", nil
	}
	return tool.targetsHash(context.Background())
}

func (b *linuxBackend) Read() (Content, bool, error) {
	tool := detect()
	if tool == nil {
		return Content{}, false, nil
	}
	ctx := context.Background()

	if tool.supportsMime() {
		if data, ok, _ := tool.getMime(ctx, mimeURIList); ok {
			if files := parseURIList(data); len(files) > 0 {
				return Content{Kind: KindFile, Files: files}, true, nil
			}
		}
		if data, ok, _ := tool.getMime(ctx, mimePNG); ok && len(data) > 0 {
			return Content{Kind: KindImage, Image: data}, true, nil
		}
	}

	if text, ok, _ := tool.getText(ctx); ok && text != "" {
		return Content{Kind: KindText, Text: text}, true, nil
	}
	return Content{}, false, nil
}

// EnumerateFiles implements FileEnumerator: same text/uri-list fetch as
// Read, but stops at os.Stat — no file bytes are read, so this is cheap
// enough to call on every detected change.
func (b *linuxBackend) EnumerateFiles() ([]FileSummary, bool) {
	tool := detect()
	if tool == nil || !tool.supportsMime() {
		return nil, false
	}
	data, ok, _ := tool.getMime(context.Background(), mimeURIList)
	if !ok {
		return nil, false
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	var summaries []FileSummary
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "file" {
			continue
		}
		info, err := os.Stat(u.Path)
		if err != nil {
			continue
		}
		size := info.Size()
		if info.IsDir() {
			size = 0 // a real total would mean walking the tree — not cheap
		}
		summaries = append(summaries, FileSummary{Name: filepath.Base(u.Path), Size: size, IsDir: info.IsDir()})
	}
	if len(summaries) == 0 {
		return nil, false
	}
	return summaries, true
}

func (b *linuxBackend) Write(content Content) error {
	tool := detect()
	if tool == nil {
		return fmt.Errorf("clipboard: no clipboard tool available (install xclip or wl-clipboard)")
	}
	ctx := context.Background()
	switch content.Kind {
	case KindText:
		return tool.setText(ctx, content.Text)
	case KindImage:
		if !tool.supportsMime() {
			return fmt.Errorf("clipboard: current tool does not support image clipboard")
		}
		return tool.setMime(ctx, mimePNG, content.Image)
	case KindFile:
		if !tool.supportsMime() {
			return fmt.Errorf("clipboard: current tool does not support file clipboard")
		}
		data, err := buildURIList(content.Files)
		if err != nil {
			return err
		}
		return tool.setMime(ctx, mimeURIList, data)
	default:
		return fmt.Errorf("clipboard: unsupported kind %q", content.Kind)
	}
}

// parseURIList reads a text/uri-list payload (one file:// URI per line,
// optionally with '#'-comment lines per RFC 2483) and loads each referenced
// file's bytes for transfer to the peer.
func parseURIList(data []byte) []FileItem {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	var files []FileItem
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "file" {
			continue
		}
		info, err := os.Stat(u.Path)
		if err != nil {
			continue
		}
		if info.IsDir() {
			data, err := tarDir(u.Path)
			if err != nil {
				continue
			}
			files = append(files, FileItem{Name: filepath.Base(u.Path), Data: data, IsDir: true})
			continue
		}
		data, err := os.ReadFile(u.Path)
		if err != nil {
			continue
		}
		files = append(files, FileItem{Name: filepath.Base(u.Path), Data: data})
	}
	return files
}

// buildURIList materializes incoming files to a temp dir and returns a
// text/uri-list payload pointing at them, so a normal paste into a file
// manager works.
func buildURIList(files []FileItem) ([]byte, error) {
	dir, err := os.MkdirTemp("", "usbridge-clip-*")
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	for _, f := range files {
		path := filepath.Join(dir, sanitizeFileName(f.Name))
		if f.IsDir {
			if err := untarDir(f.Data, path); err != nil {
				return nil, err
			}
		} else if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			return nil, err
		}
		u := url.URL{Scheme: "file", Path: path}
		b.WriteString(u.String())
		b.WriteString("\r\n")
	}
	return b.Bytes(), nil
}
