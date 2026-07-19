package api

import (
	"reflect"
	"testing"
)

func TestVideoCodecModes(t *testing.T) {
	cases := []struct {
		name      string
		supported []string
		wantIDs   []string
	}{
		{"all three", []string{"h264", "h265", "av1"}, []string{"h264", "h265", "av1"}},
		{"h264 only, no AV1 hardware", []string{"h264"}, []string{"h264"}},
		{"h264+h265, no AV1 hardware", []string{"h264", "h265"}, []string{"h264", "h265"}},
		{"empty falls back to h264", nil, []string{"h264"}},
		{"order from videoCodecModeInfo, not input order", []string{"av1", "h264"}, []string{"h264", "av1"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			modes := videoCodecModes(c.supported)
			gotIDs := make([]string, len(modes))
			for i, m := range modes {
				gotIDs[i] = m["id"]
			}
			if !reflect.DeepEqual(gotIDs, c.wantIDs) {
				t.Errorf("videoCodecModes(%v) ids = %v, want %v", c.supported, gotIDs, c.wantIDs)
			}
		})
	}
}

// This is the exact bug the dynamic capability check fixes: AV1 must never
// appear in supported_modes when the host's hardware can't encode it, even
// though the client-facing list used to be a static [h264,h265,av1] array
// regardless of what Sunshine's own encoder probe found.
func TestVideoCodecModes_AV1UnavailableIsExcludedNotJustLast(t *testing.T) {
	modes := videoCodecModes([]string{"h264", "h265"})
	for _, m := range modes {
		if m["id"] == "av1" {
			t.Fatalf("videoCodecModes must not include av1 when hardware doesn't support it: %v", modes)
		}
	}
	if len(modes) != 2 {
		t.Fatalf("expected exactly 2 modes (h264, h265), got %v", modes)
	}
}
