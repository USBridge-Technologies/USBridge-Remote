//go:build linux

package autostart

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// x11EnvDir/x11DisplayPath/x11XauthPath are the same paths xsetupHookBlock's
// root-run shell fragment writes (see its own doc comment) — this file's
// RefreshX11SessionEnv keeps them fresh for the rest of a login session, the
// window xsetupHookBlock itself can't reach (it only ever runs once, inside
// the greeter's own X session, before login).
const (
	x11EnvDir      = "/run/usbridge-agent"
	x11DisplayPath = x11EnvDir + "/greeter-display"
	x11XauthPath   = x11EnvDir + "/greeter-xauth"
)

// findOwnX11Session scans /proc for a process owned by this agent's own uid
// that has an X11 DISPLAY and a readable XAUTHORITY in its environment --
// i.e. this same user's actual logged-in desktop session, once (if) it's an
// X11 one -- *and* is the seat's actually-active session right now, per
// logind (see sessionIsActive). Every process inside a graphical session
// inherits the same DISPLAY/XAUTHORITY/XDG_SESSION_ID from its session
// leader, so the first match found is as good as any other; no need to
// specifically find kwin/plasma/etc.
//
// The active check matters for fast-user-switching / "switch user back to
// the greeter": that spawns a brand new greeter session (a different user,
// sddm) on a new VT while this user's own desktop session keeps right on
// running in the background, just no longer the one actually on screen.
// Without checking Active, this function still happily finds that
// backgrounded session (it's real, it's still this uid's, DISPLAY/
// XAUTHORITY both still resolve) and RefreshX11SessionEnv keeps streaming
// its content — confirmed live: the physical monitor was showing SDDM's
// login prompt, but the stream kept showing that *backgrounded* desktop
// session instead (frozen/blanked-by-the-VT-switch, in practice a stream of
// solid black) instead of the greeter actually on screen, which had no
// chance to end up in these files at all because a fresh RefreshX11SessionEnv
// tick kept re-overwriting the greeter's own just-written Xsetup values
// with the stale backgrounded session's every time it ran.
//
// Unlike the SDDM greeter (a different user, sddm, whose own XAUTHORITY this
// agent's user genuinely cannot read -- see xsetupHookBlock's doc comment for
// why that needs a root-run hook instead), a real logged-in X11 session's
// XAUTHORITY is normally owned by the logged-in user themselves (confirmed
// live: SDDM writes it to a user-owned file under /tmp, not the
// sddm-owned /run/sddm/xauth_* the greeter itself uses) -- so this agent,
// already running as that same user, can read it directly with no
// elevation at all.
func findOwnX11Session() (display, xauthPath string, ok bool) {
	uid := os.Getuid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return "", "", false
	}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // not a pid directory
		}
		procDir := filepath.Join("/proc", e.Name())
		info, err := os.Stat(procDir)
		if err != nil {
			continue
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(st.Uid) != uid {
			continue
		}
		data, err := os.ReadFile(filepath.Join(procDir, "environ"))
		if err != nil {
			continue // no permission (not our process after all) or it exited mid-scan
		}
		disp, xauth, sessionID := parseX11SessionEnv(data)
		if disp == "" || xauth == "" {
			continue
		}
		if _, err := os.Stat(xauth); err != nil {
			continue // stale env var pointing at an already-removed cookie file
		}
		if sessionID != "" && !sessionIsActive(sessionID) {
			continue // real session, but not the one actually on screen right now
		}
		return disp, xauth, true
	}
	return "", "", false
}

// parseX11SessionEnv pulls DISPLAY, XAUTHORITY, and XDG_SESSION_ID out of a
// raw /proc/<pid>/environ dump (NUL-separated "KEY=value" entries) -- split
// out of findOwnX11Session purely so this parsing has no /proc dependency
// and can be unit tested directly.
func parseX11SessionEnv(environ []byte) (display, xauthPath, sessionID string) {
	for _, kv := range strings.Split(string(environ), "\x00") {
		switch {
		case strings.HasPrefix(kv, "DISPLAY="):
			display = strings.TrimPrefix(kv, "DISPLAY=")
		case strings.HasPrefix(kv, "XAUTHORITY="):
			xauthPath = strings.TrimPrefix(kv, "XAUTHORITY=")
		case strings.HasPrefix(kv, "XDG_SESSION_ID="):
			sessionID = strings.TrimPrefix(kv, "XDG_SESSION_ID=")
		}
	}
	return display, xauthPath, sessionID
}

// sessionIsActive reports whether logind considers sessionID the seat's
// currently active session (i.e. actually on screen right now, not just
// running in the background — see findOwnX11Session's doc comment for why
// that distinction matters). Uses `loginctl` rather than parsing
// /run/systemd/sessions/<id> directly: that file's own header says "do not
// parse", loginctl is the supported interface to the exact same data, and
// this is called at most once per RefreshX11SessionEnv tick (see
// sunshineWatchdogInterval), so a subprocess here is not remotely hot-path.
// Treats "logind not reachable"/any error as *not* active — this is a
// permissive-by-default gate (skip the found session rather than trust it)
// specifically because acting on a wrong guess here means streaming a
// backgrounded, possibly stale/locked desktop instead of what's actually on
// screen, which is the exact bug this whole check exists to avoid.
func sessionIsActive(sessionID string) bool {
	out, err := exec.Command("loginctl", "show-session", sessionID, "-p", "Active", "--value").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "yes"
}

// RefreshX11SessionEnv re-syncs the greeter-display/greeter-xauth files (see
// x11DisplayPath/x11XauthPath) against whatever X11 session this agent's own
// user is currently running, if any. rust-shine's X11 capture fallback (see
// capture-kms's x11_fallback.rs module docs) reads these files fresh on
// every fallback attempt, but nothing used to *write* them past the one-shot
// SDDM Xsetup hook (xsetupHookBlock) -- fine for the pre-login greeter window
// that hook targets, but the moment that greeter session ends (successful
// login) its DISPLAY/cookie go stale for the rest of however long this agent
// keeps running, including across any later switch between an X11 and a
// Wayland desktop session (the greeter itself only ever runs X11, so its
// snapshot has no way to reflect that switch at all). Calling this
// periodically (see app.go's sunshineWatchdog) means the fallback always
// targets the *current* real session instead of a one-time snapshot: no-op
// once nothing has changed since the last call, so it's cheap to call
// often.
//
// Deliberately does nothing (leaves whatever is already there alone) when no
// X11 session is found for this user right now -- that's both the "still at
// the greeter, nobody's logged in yet" case (Xsetup's own write is still the
// only source and is exactly right there) and the "logged into a Wayland
// session" case (KMS capture works directly against those -- see
// run_kms_pipeline_for_session -- so the X11 fallback is simply never
// reached and stale content sitting unused is harmless).
func RefreshX11SessionEnv() {
	display, xauthPath, ok := findOwnX11Session()
	if !ok {
		return
	}
	data, err := os.ReadFile(xauthPath)
	if err != nil {
		return
	}
	writeX11SessionEnv(display, data, x11EnvDir, x11DisplayPath, x11XauthPath)
}

// writeX11SessionEnv does RefreshX11SessionEnv's actual comparison and
// writes, parameterized on the destination paths purely so this half (unlike
// findOwnX11Session's real /proc scan) can be exercised directly in a test
// against a throwaway directory instead of the real, root-owned
// /run/usbridge-agent.
func writeX11SessionEnv(display string, xauthData []byte, dir, displayPath, xauthPath string) {
	if cur, err := os.ReadFile(xauthPath); err == nil && bytes.Equal(cur, xauthData) {
		if curDisp, err := os.ReadFile(displayPath); err == nil && strings.TrimSpace(string(curDisp)) == display {
			return // already up to date -- skip the writes below
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(xauthPath, xauthData, 0o600)
	_ = os.WriteFile(displayPath, []byte(display), 0o600)
}
