//go:build linux && !android

package clipboard

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func runCmd(ctx context.Context, path string, args []string, stdin []byte) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, path, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
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

var (
	detectOnce sync.Once
	activeTool clipboardTool
)

// detect picks the clipboard CLI tool available on this session, preferring
// wl-clipboard under Wayland and xclip under X11 (both support arbitrary
// targets), falling back to xsel (text only), and logging once if nothing
// usable is installed — clipboard sync then just no-ops instead of crashing.
func detect() clipboardTool {
	detectOnce.Do(func() {
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			copyPath, errC := exec.LookPath("wl-copy")
			pastePath, errP := exec.LookPath("wl-paste")
			if errC == nil && errP == nil {
				activeTool = wlTool{copyPath: copyPath, pastePath: pastePath}
				return
			}
		}
		if p, err := exec.LookPath("xclip"); err == nil {
			activeTool = xclipTool{path: p}
			return
		}
		if p, err := exec.LookPath("xsel"); err == nil {
			log.Printf("[clipboard] only xsel found (text-only); install xclip or wl-clipboard for image/file clipboard sync")
			activeTool = xselTool{path: p}
			return
		}
		log.Printf("[clipboard] no xclip/xsel/wl-clipboard found; clipboard sync disabled on this Linux session")
	})
	return activeTool
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
