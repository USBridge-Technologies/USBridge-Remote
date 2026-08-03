//go:build linux

package autostart

import (
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// xrandrMode is one resolution xrandr reports for a connected output (one
// line of its plain, non-verbose --query output -- multiple refresh rates on
// that line collapse into a single entry here, since ensureDisplayActive
// only ever picks a mode by resolution name, letting the driver choose its
// own default refresh rate for it).
type xrandrMode struct {
	name string // e.g. "1600x900", or a manually-added "1920x1080_60.00"
	area int
}

type xrandrOutput struct {
	name          string
	connected     bool
	hasActiveMode bool
	modes         []xrandrMode
}

// parseXrandrOutputs parses plain `xrandr --query` output (NOT --verbose --
// that format doesn't mark the active mode with "*" per-output the same way
// and is far more expensive to generate). Split out from ensureDisplayActive
// purely so this parsing is unit-testable against captured real output
// without a live X server.
func parseXrandrOutputs(output []byte) []xrandrOutput {
	var outputs []xrandrOutput
	var cur *xrandrOutput
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				cur = nil
				continue
			}
			outputs = append(outputs, xrandrOutput{name: fields[0], connected: fields[1] == "connected"})
			cur = &outputs[len(outputs)-1]
			continue
		}
		if cur == nil || !cur.connected {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		w, h, ok := parseXrandrResolution(fields[0])
		if !ok {
			continue
		}
		if strings.Contains(line, "*") {
			cur.hasActiveMode = true
		}
		cur.modes = append(cur.modes, xrandrMode{name: fields[0], area: w * h})
	}
	return outputs
}

// parseXrandrResolution parses a mode's resolution column, e.g. "1600x900"
// or a manually --newmode'd "1920x1080_60.00" (the trailing "_60.00" is
// xrandr's own auto-generated disambiguating suffix for a custom mode name,
// not a real part of the resolution).
func parseXrandrResolution(s string) (w, h int, ok bool) {
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	heightPart := parts[1]
	if idx := strings.IndexByte(heightPart, '_'); idx >= 0 {
		heightPart = heightPart[:idx]
	}
	wi, errW := strconv.Atoi(parts[0])
	hi, errH := strconv.Atoi(heightPart)
	if errW != nil || errH != nil {
		return 0, 0, false
	}
	return wi, hi, true
}

// runXrandrFunc abstracts invoking the real xrandr binary so
// ensureDisplayActive/fixDarkOutput's decision logic can be unit tested
// without a live X server.
type runXrandrFunc func(args ...string) error

// ensureDisplayActive is EnsureDisplayActive's actual logic, parameterized on
// the xrandr invocation so it's testable directly.
func ensureDisplayActive(query func() ([]byte, error), run runXrandrFunc) {
	out, err := query()
	if err != nil {
		return
	}
	for _, o := range parseXrandrOutputs(out) {
		if !o.connected || o.hasActiveMode || len(o.modes) == 0 {
			continue
		}
		fixDarkOutput(o, run)
	}
}

// fixDarkOutput recovers a connected output that currently has no active
// mode at all -- confirmed live: a fresh SDDM login can leave a previously-
// working HDMI output completely dark (no mode active on it whatsoever)
// because the desktop environment's own display auto-configuration on
// session start doesn't reliably reapply whatever mode was working before,
// even though the physical monitor and its EDID haven't changed at all.
//
// Tries the output's own advertised modes from the largest resolution down,
// skipping straight past any driver rejection instead of giving up. Modes
// with an underscore in their name (xrandr's auto-generated disambiguator
// for one manually added via --newmode/--addmode, e.g. "1920x1080_60.00")
// are tried *after* every plain EDID-reported mode of equal or smaller size,
// even though they might nominally be larger -- confirmed live that the
// driver can simply stop accepting a custom modeline it used to accept
// (RRAddOutputMode failing BadMatch), so a real, currently-supported mode is
// a safer first choice than the highest resolution on paper. Falls back to
// letting the driver pick for itself (--auto) only if every mode in the
// list itself fails.
func fixDarkOutput(o xrandrOutput, run runXrandrFunc) {
	sorted := append([]xrandrMode(nil), o.modes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		iCustom := strings.Contains(sorted[i].name, "_")
		jCustom := strings.Contains(sorted[j].name, "_")
		if iCustom != jCustom {
			return !iCustom
		}
		return sorted[i].area > sorted[j].area
	})
	for _, m := range sorted {
		if err := run("--output", o.name, "--mode", m.name); err == nil {
			return
		}
	}
	_ = run("--output", o.name, "--auto")
}

// EnsureDisplayActive checks this agent's own active X11 session (see
// findOwnX11Session) for a connected output with no active mode at all, and
// recovers it -- see fixDarkOutput's doc comment for the exact scenario this
// exists for. No-ops entirely (including doing no work at all) if no X11
// session is found, or every connected output already has an active mode.
func EnsureDisplayActive() {
	display, xauthPath, ok := findOwnX11Session()
	if !ok {
		return
	}
	env := append(os.Environ(), "DISPLAY="+display, "XAUTHORITY="+xauthPath)
	query := func() ([]byte, error) {
		cmd := exec.Command("xrandr", "--query")
		cmd.Env = env
		return cmd.Output()
	}
	run := func(args ...string) error {
		cmd := exec.Command("xrandr", args...)
		cmd.Env = env
		return cmd.Run()
	}
	ensureDisplayActive(query, run)
}
