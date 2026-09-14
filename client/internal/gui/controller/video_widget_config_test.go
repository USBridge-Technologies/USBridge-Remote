package controller

import (
	"testing"

	"usbridge-client/internal/models"
)

// TestVideoDeviceConfigFromRequestPreservesColor444AndHdr is a regression
// test for a bug where the "Start Video" dialog (handleVideoStartWithParams)
// and the device-settings "Apply" dialog (ShowVideoDeviceSettings) each
// hand-built a models.VideoDeviceConfig from the submitted
// models.VideoStartRequest via their own separate struct literal, and both
// literals forgot to carry over Color444/Hdr. Since Color444/Hdr have no
// other persistence path, the save clobbered whatever was on disk with
// false, and the very next reconcile/restart read that same false back via
// VideoDeviceConfig.ToVideoStartRequest() -- so the checkboxes being checked
// in the UI had no effect on the actual stream, from the very first
// Start/Apply onward, not just on a later reconnect. See the client log's
// "[Moonlight/HDR-debug]" line: it always showed color444=false hdr=false
// even with both boxes checked.
func TestVideoDeviceConfigFromRequestPreservesColor444AndHdr(t *testing.T) {
	for _, tc := range []struct {
		name          string
		color444, hdr bool
	}{
		{"both off", false, false},
		{"444 only", true, false},
		{"hdr only", false, true},
		{"both on", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := &models.VideoStartRequest{
				VideoDevice:  "/dev/video0",
				VideoWidth:   1920,
				VideoHeight:  1080,
				VideoFPS:     60,
				VideoBitrate: "20000K",
				VideoMode:    models.VideoModeH265,
				Color444:     tc.color444,
				Hdr:          tc.hdr,
			}

			cfg := videoDeviceConfigFromRequest(request.VideoDevice, "Capture Card", request)

			if cfg.Color444 != tc.color444 {
				t.Errorf("Color444 = %v, want %v (request checkbox choice dropped on the way to VideoDeviceConfig)", cfg.Color444, tc.color444)
			}
			if cfg.Hdr != tc.hdr {
				t.Errorf("Hdr = %v, want %v (request checkbox choice dropped on the way to VideoDeviceConfig)", cfg.Hdr, tc.hdr)
			}

			// Round-trip through ToVideoStartRequest, exactly what the next
			// reconcile/restart does after loading this cfg back off disk --
			// must still agree with what the user actually checked.
			roundTripped := cfg.ToVideoStartRequest()
			if roundTripped.Color444 != tc.color444 {
				t.Errorf("round-tripped Color444 = %v, want %v", roundTripped.Color444, tc.color444)
			}
			if roundTripped.Hdr != tc.hdr {
				t.Errorf("round-tripped Hdr = %v, want %v", roundTripped.Hdr, tc.hdr)
			}
		})
	}
}
