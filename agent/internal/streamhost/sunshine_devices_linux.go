//go:build linux

package streamhost

import (
	"bufio"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// monitorLogLineRe matches Sunshine's own "Monitor <index> is <name>: ..."
// info-level log line (see correlate_to_wayland in kmsgrab.cpp), which is
// the only place Sunshine tells us how its KMS capture output_name index
// (an arbitrary plane-enumeration order, not necessarily alphabetical by
// connector name) maps back to a real connector name like "DP-2".
var monitorLogLineRe = regexp.MustCompile(`Monitor (\d+) is ([^:]+):`)

// monitorIndexByName scans Sunshine's log for its most recent "Monitor N is
// <name>" enumeration (see monitorLogLineRe) and returns a map from
// connector name (e.g. "DP-2") to Sunshine's own output_name index for it.
// Without this, callers have no way to know Sunshine's real index-to-monitor
// mapping short of guessing (e.g. alphabetical /sys/class/drm order), which
// silently mislabels devices whenever Sunshine's internal DRM plane
// enumeration order doesn't match — see the "drm:N" device paths built in
// capture.Devices(). Returns nil if the log doesn't exist yet or the
// correlation never ran (e.g. no Wayland connection at startup).
func (b *sunshineBackend) monitorIndexByName() map[string]int {
	path := b.LogPath()
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Only the tail of the log matters (the most recent Sunshine startup's
	// enumeration); seeking near the end bounds the read on a long-lived
	// deployment's log instead of scanning it in full every call.
	const tailBytes = 1 << 20 // 1MiB is generously more than one startup's worth of log lines
	if info, err := f.Stat(); err == nil && info.Size() > tailBytes {
		_, _ = f.Seek(-tailBytes, io.SeekEnd)
	}

	result := make(map[string]int)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		m := monitorLogLineRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		idx, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		// Keep overwriting on later matches so a mid-log restart's fresher
		// enumeration wins over an older one earlier in the tail window.
		result[strings.TrimSpace(m[2])] = idx
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ListCaptureDevices reports Sunshine's real connector-name-to-output_name
// index correlation, one CaptureDevice per connector Sunshine has told us
// about via monitorIndexByName. Callers correlate by Key (the connector
// name) against their own independent display enumeration.
func (b *sunshineBackend) ListCaptureDevices() []CaptureDevice {
	byName := b.monitorIndexByName()
	if len(byName) == 0 {
		return nil
	}
	out := make([]CaptureDevice, 0, len(byName))
	for name, idx := range byName {
		out = append(out, CaptureDevice{
			Key:        name,
			OutputName: strconv.Itoa(idx),
		})
	}
	return out
}
