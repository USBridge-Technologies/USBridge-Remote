//go:build windows

package streamhost

import (
	"encoding/json"
	"os"
	"strings"
)

// windowsDisplayDevicesMarker precedes the JSON array Sunshine's Windows
// display_device backend logs once at every startup (see sunshine.log:
// "Info: Currently available display devices:" followed by a pretty-printed
// JSON array on the following lines).
const windowsDisplayDevicesMarker = "Currently available display devices:"

// windowsDisplayDevice is one entry from Sunshine's Windows-only display
// device JSON dump.
type windowsDisplayDevice struct {
	DeviceID     string `json:"device_id"`
	DisplayName  string `json:"display_name"`
	FriendlyName string `json:"friendly_name"`
	Info         struct {
		Primary    bool `json:"primary"`
		Resolution struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"resolution"`
	} `json:"info"`
}

// windowsDisplayDevices parses Sunshine's own "Currently available display
// devices" JSON block from its log. This is the only place the real,
// stable device_id string that Sunshine's Windows output_name config key
// expects is ever exposed: unlike Linux/KMS, where output_name is a small
// connected-output index, Sunshine's Windows display_device backend
// identifies each monitor by an id derived from its EDID + instance path
// (e.g. "{26932b0f-6861-553f-b009-2caec1fc240f}"), which the agent has no
// way to compute or predict — it can only be read back from what Sunshine
// itself already determined at startup. Returns nil if the log doesn't
// exist yet or no such block has been logged this session.
func (b *sunshineBackend) windowsDisplayDevices() []windowsDisplayDevice {
	path := b.LogPath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(data)

	var result []windowsDisplayDevice
	searchFrom := 0
	for {
		rel := strings.Index(text[searchFrom:], windowsDisplayDevicesMarker)
		if rel < 0 {
			break
		}
		afterMarker := searchFrom + rel + len(windowsDisplayDevicesMarker)
		relStart := strings.IndexByte(text[afterMarker:], '[')
		if relStart < 0 {
			break
		}
		start := afterMarker + relStart
		end := jsonArrayEnd(text, start)
		if end < 0 {
			break
		}
		var devices []windowsDisplayDevice
		if err := json.Unmarshal([]byte(text[start:end+1]), &devices); err == nil {
			// Keep overwriting so a later restart's fresher enumeration wins
			// over an older one earlier in the log (same "last wins" logic
			// as monitorIndexByName on Linux).
			result = devices
		}
		searchFrom = end + 1
	}
	return result
}

// jsonArrayEnd returns the index of the ']' that closes the JSON array
// starting at s[start] (which must be '['), tracking bracket depth only —
// it doesn't need to be string-aware since none of the fields Sunshine logs
// here (paths, GUIDs, names) contain literal '[' or ']' characters. Returns
// -1 if the array is never closed (e.g. log was truncated mid-write).
func jsonArrayEnd(s string, start int) int {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// ListCaptureDevices reports Sunshine's Windows display_device enumeration,
// one CaptureDevice per monitor. Key is left "" since Windows identifies
// devices directly by device_id (via OutputName), not by connector-name
// correlation.
func (b *sunshineBackend) ListCaptureDevices() []CaptureDevice {
	devices := b.windowsDisplayDevices()
	if len(devices) == 0 {
		return nil
	}
	out := make([]CaptureDevice, 0, len(devices))
	for _, d := range devices {
		name := d.FriendlyName
		if name == "" {
			name = d.DisplayName
		}
		out = append(out, CaptureDevice{
			OutputName:  d.DeviceID,
			DisplayName: name,
			Primary:     d.Info.Primary,
			Width:       d.Info.Resolution.Width,
			Height:      d.Info.Resolution.Height,
		})
	}
	return out
}
