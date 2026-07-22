//go:build linux

// Package display detects the host's actual maximum display resolution so
// the agent can advertise it to clients instead of a hardcoded guess.
package display

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// MaxResolution returns the highest resolution among all connected DRM/KMS
// outputs, read directly from /sys/class/drm — the same source Sunshine's
// own KMS capture backend uses to find its connector (see the "Found
// connector ID" log line), so this reflects whatever the host is actually
// capable of displaying/capturing, not a fixed default. ok is false if no
// connected output with a readable mode list was found (headless box,
// permissions, non-DRM setup), so the caller can fall back to a default.
func MaxResolution() (width, height int, ok bool) {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return 0, 0, false
	}
	for _, e := range entries {
		name := e.Name()
		// Connector directories look like "card1-DP-2", "card0-HDMI-A-1" —
		// skip the bare "card0" render-node entries.
		if !strings.Contains(name, "-") {
			continue
		}
		dir := filepath.Join("/sys/class/drm", name)
		status, err := os.ReadFile(filepath.Join(dir, "status"))
		if err != nil || strings.TrimSpace(string(status)) != "connected" {
			continue
		}
		f, err := os.Open(filepath.Join(dir, "modes"))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			// Each connector's modes file is sorted with the preferred
			// (typically highest) mode first, e.g. "1920x1080". Take the
			// first line for this connector, then compare by area across
			// all connected connectors to find the overall max.
			w, h, parsed := parseModeLine(sc.Text())
			if parsed && w*h > width*height {
				width, height, ok = w, h, true
			}
			break
		}
		f.Close()
	}
	return width, height, ok
}

// Resolution is a single width x height mode.
type Resolution struct {
	Width  int
	Height int
}

// ConnectorResolutions returns every distinct resolution advertised by each
// connected DRM/KMS output, one slice per connector, ordered by connector
// name (e.g. "card1-DP-1", "card1-DP-2", ...) — the same order os.ReadDir
// gives, which lines up with how the agent otherwise indexes displays.
// Within a connector, modes keep the order the kernel reports them in
// (preferred/highest first, see MaxResolution's comment), deduplicated by
// width/height since the same resolution is repeated once per refresh rate.
// Returns nil if /sys/class/drm can't be read or no connector has modes.
func ConnectorResolutions() [][]Resolution {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return nil
	}
	var connectors [][]Resolution
	for _, e := range entries {
		name := e.Name()
		if !strings.Contains(name, "-") {
			continue
		}
		dir := filepath.Join("/sys/class/drm", name)
		status, err := os.ReadFile(filepath.Join(dir, "status"))
		if err != nil || strings.TrimSpace(string(status)) != "connected" {
			continue
		}
		f, err := os.Open(filepath.Join(dir, "modes"))
		if err != nil {
			continue
		}
		seen := make(map[Resolution]bool)
		var modes []Resolution
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			w, h, parsed := parseModeLine(sc.Text())
			if !parsed {
				continue
			}
			r := Resolution{Width: w, Height: h}
			if seen[r] {
				continue
			}
			seen[r] = true
			modes = append(modes, r)
		}
		f.Close()
		if len(modes) > 0 {
			connectors = append(connectors, modes)
		}
	}
	return connectors
}

// Connector is a single connected DRM/KMS output, in the same enumeration
// order as ConnectorResolutions (os.ReadDir order over /sys/class/drm).
// Sunshine's own KMS capture backend enumerates connected outputs in the
// same connected-only order and refers to them by that position (its
// "output_name"/monitor index), so Index here is meant to line up with it.
type Connector struct {
	Name  string // e.g. "card1-DP-2"
	Index int    // position among connected connectors, 0-based
	Modes []Resolution
}

// Connectors returns every connected DRM/KMS output with its name and
// supported modes, in connected-connector order — the value to persist as
// Sunshine's output_name (as Index, stringified) to pin a specific monitor
// for KMS capture. Returns nil if /sys/class/drm can't be read.
func Connectors() []Connector {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return nil
	}
	var connectors []Connector
	for _, e := range entries {
		name := e.Name()
		if !strings.Contains(name, "-") {
			continue
		}
		dir := filepath.Join("/sys/class/drm", name)
		status, err := os.ReadFile(filepath.Join(dir, "status"))
		if err != nil || strings.TrimSpace(string(status)) != "connected" {
			continue
		}
		f, err := os.Open(filepath.Join(dir, "modes"))
		if err != nil {
			continue
		}
		seen := make(map[Resolution]bool)
		var modes []Resolution
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			w, h, parsed := parseModeLine(sc.Text())
			if !parsed {
				continue
			}
			r := Resolution{Width: w, Height: h}
			if seen[r] {
				continue
			}
			seen[r] = true
			modes = append(modes, r)
		}
		f.Close()
		if len(modes) == 0 {
			continue
		}
		connectors = append(connectors, Connector{
			Name:  name,
			Index: len(connectors),
			Modes: modes,
		})
	}
	return connectors
}

func parseModeLine(line string) (w, h int, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(line), "x", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}
