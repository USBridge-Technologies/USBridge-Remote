//go:build linux

package autostart

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseX11SessionEnv(t *testing.T) {
	environ := "HOME=/home/amir\x00DISPLAY=:0\x00XAUTHORITY=/tmp/xauth_abc123\x00XDG_SESSION_ID=21\x00SHELL=/bin/bash\x00"
	disp, xauth, sessionID := parseX11SessionEnv([]byte(environ))
	if disp != ":0" {
		t.Errorf("display = %q, want %q", disp, ":0")
	}
	if xauth != "/tmp/xauth_abc123" {
		t.Errorf("xauth = %q, want %q", xauth, "/tmp/xauth_abc123")
	}
	if sessionID != "21" {
		t.Errorf("sessionID = %q, want %q", sessionID, "21")
	}
}

func TestParseX11SessionEnvMissingKeys(t *testing.T) {
	disp, xauth, sessionID := parseX11SessionEnv([]byte("HOME=/home/amir\x00SHELL=/bin/bash\x00"))
	if disp != "" || xauth != "" || sessionID != "" {
		t.Errorf("expected all empty for environ with none of the keys, got display=%q xauth=%q sessionID=%q", disp, xauth, sessionID)
	}
}

func TestParseX11SessionEnvWaylandSessionHasNoXauthority(t *testing.T) {
	// A pure-Wayland session process typically has no XAUTHORITY at all --
	// findOwnX11Session's caller must treat this as "not an X11 session",
	// not accidentally use an empty XAUTHORITY.
	disp, xauth, _ := parseX11SessionEnv([]byte("WAYLAND_DISPLAY=wayland-0\x00HOME=/home/amir\x00XDG_SESSION_ID=19\x00"))
	if disp != "" || xauth != "" {
		t.Errorf("expected both empty for a Wayland-only environ, got display=%q xauth=%q", disp, xauth)
	}
}

// A nonexistent session ID must be treated as "not active" (the permissive-
// by-default-skip gate findOwnX11Session's doc comment describes), not
// panic or wrongly report active just because loginctl errored out.
func TestSessionIsActiveFalseForNonexistentSession(t *testing.T) {
	if _, err := os.Stat("/run/systemd/seats"); err != nil {
		t.Skip("not running under systemd-logind in this environment")
	}
	if sessionIsActive("this-session-id-does-not-exist-12345") {
		t.Error("expected a nonexistent session to report inactive, got active")
	}
}

// Regression test for writeX11SessionEnv's "skip the write when nothing
// changed" fast path: writing greeter-xauth/greeter-display on every single
// watchdog tick (see app.go's sunshineWatchdog) once a session has settled
// would be pure waste. Calls the real function against a throwaway
// directory (not the root-owned /run/usbridge-agent RefreshX11SessionEnv
// itself targets).
func TestWriteX11SessionEnvSkipsRewriteWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	displayPath := filepath.Join(dir, "display")
	xauthPath := filepath.Join(dir, "xauth")
	if err := os.WriteFile(xauthPath, []byte("cookie-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(displayPath, []byte(":0"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(xauthPath)
	if err != nil {
		t.Fatal(err)
	}

	writeX11SessionEnv(":0", []byte("cookie-bytes"), dir, displayPath, xauthPath)

	after, err := os.Stat(xauthPath)
	if err != nil {
		t.Fatal(err)
	}
	if before.ModTime() != after.ModTime() {
		t.Error("file was rewritten even though display and xauth content were both unchanged")
	}
}

// Companion test: a genuinely new session (different cookie bytes and/or
// display) must actually get written, not just detected.
func TestWriteX11SessionEnvWritesOnChange(t *testing.T) {
	dir := t.TempDir()
	displayPath := filepath.Join(dir, "display")
	xauthPath := filepath.Join(dir, "xauth")
	if err := os.WriteFile(xauthPath, []byte("old-cookie"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(displayPath, []byte(":1"), 0o600); err != nil {
		t.Fatal(err)
	}

	writeX11SessionEnv(":0", []byte("new-cookie"), dir, displayPath, xauthPath)

	gotXauth, err := os.ReadFile(xauthPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotXauth) != "new-cookie" {
		t.Errorf("xauth content = %q, want %q", gotXauth, "new-cookie")
	}
	gotDisplay, err := os.ReadFile(displayPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotDisplay) != ":0" {
		t.Errorf("display content = %q, want %q", gotDisplay, ":0")
	}
}

// A stale display file with a fresh-looking cookie (or vice versa) must
// still trigger a rewrite of *both* -- checking only one of the two fields
// would let a session that changed display but reused the same cookie bytes
// (or the reverse) go undetected.
func TestWriteX11SessionEnvWritesWhenOnlyDisplayChanged(t *testing.T) {
	dir := t.TempDir()
	displayPath := filepath.Join(dir, "display")
	xauthPath := filepath.Join(dir, "xauth")
	if err := os.WriteFile(xauthPath, []byte("same-cookie"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(displayPath, []byte(":1"), 0o600); err != nil {
		t.Fatal(err)
	}

	writeX11SessionEnv(":0", []byte("same-cookie"), dir, displayPath, xauthPath)

	gotDisplay, err := os.ReadFile(displayPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotDisplay) != ":0" {
		t.Errorf("display content = %q, want %q (should have been rewritten despite unchanged xauth)", gotDisplay, ":0")
	}
}

// First-ever call: the destination files don't exist yet at all (fresh
// /run/usbridge-agent tmpfs after a reboot). Must create them, not bail out
// because the "unchanged" read failed.
func TestWriteX11SessionEnvCreatesDirAndFilesWhenMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "usbridge-agent") // deliberately not yet created
	displayPath := filepath.Join(dir, "greeter-display")
	xauthPath := filepath.Join(dir, "greeter-xauth")

	writeX11SessionEnv(":0", []byte("cookie-bytes"), dir, displayPath, xauthPath)

	gotXauth, err := os.ReadFile(xauthPath)
	if err != nil {
		t.Fatalf("greeter-xauth not written: %v", err)
	}
	if string(gotXauth) != "cookie-bytes" {
		t.Errorf("xauth content = %q, want %q", gotXauth, "cookie-bytes")
	}
	gotDisplay, err := os.ReadFile(displayPath)
	if err != nil {
		t.Fatalf("greeter-display not written: %v", err)
	}
	if string(gotDisplay) != ":0" {
		t.Errorf("display content = %q, want %q", gotDisplay, ":0")
	}
}
