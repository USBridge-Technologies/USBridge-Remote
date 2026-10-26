//go:build linux && !android

// Package kwinplanes is the Linux "one plane for the stream" tune for KWin.
//
// KWin on a multi-plane GPU scans windows out directly and puts the desktop
// on an overlay above them (an "underlay"), and shows video on overlay
// planes. Capturing a screen that is split over several KMS planes means
// putting them back together, and while a window is animated (snapped to a
// screen edge, dragged back) KWin flips between those layouts every few
// frames, so the picture changes as a whole and the stream flickers. With
// direct scanout and overlays off KWin hands out one composited buffer (plus
// the cursor), the same as GNOME/mutter, and the capture has nothing to put
// together.
//
// Plasma sources ~/.config/plasma-workspace/env/*.sh at login, so a small
// script there is all it takes: no administrator rights, in effect from the
// next login.
package kwinplanes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const scriptName = "usbridge-kwin-single-plane.sh"

const scriptBody = `# USBridge: KWin composites everything into one buffer (no direct scanout, no
# overlay planes), so a stream of this screen is a single picture instead of
# several KMS planes that flip while windows animate. Delete this file and
# log in again to undo.
export KWIN_DRM_NO_DIRECT_SCANOUT=1
export KWIN_USE_OVERLAYS=0
`

func scriptPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "plasma-workspace", "env", scriptName), nil
}

// kwinEnviron is the environment of the running kwin_wayland, nil when KWin
// isn't the compositor of this machine.
func kwinEnviron() (env []string, running bool) {
	entries, _ := filepath.Glob("/proc/[0-9]*/comm")
	for _, comm := range entries {
		b, err := os.ReadFile(comm)
		if err != nil || strings.TrimSpace(string(b)) != "kwin_wayland" {
			continue
		}
		running = true
		if raw, err := os.ReadFile(filepath.Join(filepath.Dir(comm), "environ")); err == nil {
			env = strings.Split(string(raw), "\x00")
		}
		return env, true
	}
	return nil, false
}

func hasBoth(lines []string) bool {
	var direct, overlays bool
	for _, l := range lines {
		l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "export "))
		switch l {
		case "KWIN_DRM_NO_DIRECT_SCANOUT=1":
			direct = true
		case "KWIN_USE_OVERLAYS=0":
			overlays = true
		}
	}
	return direct && overlays
}

// Status reports whether this machine runs KWin on Wayland, the compositor
// that splits the screen over several planes (applicable), and whether the
// tune is already in place (done: in the login script, or already in the
// running KWin's environment).
func Status() (applicable, done bool) {
	env, running := kwinEnviron()
	if !running {
		return false, false
	}
	if hasBoth(env) {
		return true, true
	}
	p, err := scriptPath()
	if err != nil {
		return true, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return true, false
	}
	return true, hasBoth(strings.Split(string(b), "\n"))
}

// Active reports whether the running KWin already has the tune, as opposed
// to it being written and waiting for the next login.
func Active() bool {
	env, running := kwinEnviron()
	return running && hasBoth(env)
}

// Apply writes the login script. It takes effect at the next Plasma login.
func Apply() error {
	p, err := scriptPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(scriptBody), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", p, err)
	}
	return nil
}
