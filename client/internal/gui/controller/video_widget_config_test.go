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
// literals forgot to carry over Color444/Hdr/EnableVSync. Since none of
// those have any other persistence path, the save clobbered whatever was on
// disk with false, and the very next reconcile/restart read that same false
// back via VideoDeviceConfig.ToVideoStartRequest() -- so the checkboxes being
// checked in the UI had no effect on the actual stream, from the very first
// Start/Apply onward, not just on a later reconnect. See the client log's
// "[Moonlight/HDR-debug]" line: it always showed color444=false hdr=false
// even with both boxes checked. EnableVSync hit the exact same bug: the
// Vulkan overlay's swapchain present mode reverted to IMMEDIATE (tearing)
// on the first reconcile after start, even with VSync checked.
func TestVideoDeviceConfigFromRequestPreservesColor444AndHdr(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		color444, hdr, vsync bool
	}{
		{"all off", false, false, false},
		{"444 only", true, false, false},
		{"hdr only", false, true, false},
		{"vsync only", false, false, true},
		{"all on", true, true, true},
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
				EnableVSync:  tc.vsync,
			}

			cfg := videoDeviceConfigFromRequest(request.VideoDevice, "Capture Card", request)

			if cfg.Color444 != tc.color444 {
				t.Errorf("Color444 = %v, want %v (request checkbox choice dropped on the way to VideoDeviceConfig)", cfg.Color444, tc.color444)
			}
			if cfg.Hdr != tc.hdr {
				t.Errorf("Hdr = %v, want %v (request checkbox choice dropped on the way to VideoDeviceConfig)", cfg.Hdr, tc.hdr)
			}
			if cfg.EnableVSync != tc.vsync {
				t.Errorf("EnableVSync = %v, want %v (request checkbox choice dropped on the way to VideoDeviceConfig)", cfg.EnableVSync, tc.vsync)
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
			if roundTripped.EnableVSync != tc.vsync {
				t.Errorf("round-tripped EnableVSync = %v, want %v", roundTripped.EnableVSync, tc.vsync)
			}
		})
	}
}

// TestMergeVideoConfigWithInfo_UsesEncodingNotTransportMode is the
// regression test for the actual codec-switch bug (found live via
// [CODEC-TRACE] logging, 2026-09-18): mergeVideoConfigWithInfo used to copy
// info.Mode into cfg.VideoMode. info.Mode (VideoStatus.Mode, JSON "mode") is
// the TRANSPORT label the agent's /api/video/info handler hardcodes to the
// literal string "moonlight" (server.go: `"mode": "moonlight"`) -- it is
// never a codec name. cfg.VideoMode is the CODEC selector
// (models.VideoModeH264/H265/AV1) that flows straight into
// MoonlightService.SetVideoMode -> moonlightVideoFormat.
//
// This merge only fires when the target device has no saved config yet
// (resolvePreferredVideoConfig's `!hasSavedVideoDeviceConfig` guard) --
// which is exactly what happens when a reconcile's device-list lookup falls
// back to a different device than the one just configured (see
// resolvePreferredVideoConfig's "NOT FOUND in current device list"
// [CODEC-TRACE] warning). Confirmed live: after selecting H265 and hitting
// Apply, the log showed
//
//	resolvePreferredVideoConfig: no saved prefs yet, merged server info.Mode="moonlight" -> VideoMode="moonlight"
//	...
//	MoonlightService.SetVideoMode: "moonlight" -> "moonlight"
//
// moonlightVideoFormat doesn't recognize "moonlight" as a mode string and
// silently defaults to VIDEO_FORMAT_H264 (its documented fallback for any
// unrecognized mode) -- so the stream restarted in H264 regardless of what
// the user picked, with every step up to this merge showing the correct
// "h265" in the logs. info.Encoding (VideoStatus.Encoding, JSON "encoding")
// is the field that actually carries the codec the server is running right
// now -- see the agent's Application.CurrentVideoCodec.
func TestMergeVideoConfigWithInfo_UsesEncodingNotTransportMode(t *testing.T) {
	cfg := models.VideoDeviceConfig{VideoMode: "", VideoWidth: 1280, VideoHeight: 720}
	info := &models.VideoInfoData{
		VideoStatus: models.VideoStatus{
			Mode:     "moonlight", // transport label -- always this literal string, never a codec
			Encoding: "h265",      // the actual negotiated/active codec
			Width:    1920,
			Height:   1080,
		},
	}

	merged := mergeVideoConfigWithInfo(cfg, info)

	if merged.VideoMode == "moonlight" {
		t.Fatalf("mergeVideoConfigWithInfo copied the transport label info.Mode=%q into VideoMode -- this silently forces H264 (moonlightVideoFormat's fallback for any unrecognized mode string) regardless of what the user selected", info.Mode)
	}
	if merged.VideoMode != models.VideoModeH265 {
		t.Errorf("VideoMode = %q, want %q (from info.Encoding, not info.Mode=%q)", merged.VideoMode, models.VideoModeH265, info.Mode)
	}
	// Width/height/fps/bitrate merging is unrelated to this bug and must
	// keep working exactly as before.
	if merged.VideoWidth != 1920 || merged.VideoHeight != 1080 {
		t.Errorf("resolution = %dx%d, want 1920x1080 (from info)", merged.VideoWidth, merged.VideoHeight)
	}
}

// TestMergeVideoConfigWithInfo_EmptyEncodingLeavesVideoModeUntouched pins
// the "nothing to merge" side: an agent response with no encoding reported
// yet must not clobber whatever VideoMode the caller already had (mirrors
// the existing empty-string guards for Bitrate/Quality/etc. in this
// function).
func TestMergeVideoConfigWithInfo_EmptyEncodingLeavesVideoModeUntouched(t *testing.T) {
	cfg := models.VideoDeviceConfig{VideoMode: models.VideoModeAV1}
	info := &models.VideoInfoData{VideoStatus: models.VideoStatus{Mode: "moonlight", Encoding: ""}}

	merged := mergeVideoConfigWithInfo(cfg, info)

	if merged.VideoMode != models.VideoModeAV1 {
		t.Errorf("VideoMode = %q, want %q (untouched -- info.Encoding was empty)", merged.VideoMode, models.VideoModeAV1)
	}
}

// TestMergeVideoConfigWithInfo_NeverAutoAdoptsAV1 is the regression test for
// a real perf incident confirmed via [CODEC-TRACE] + the frame-smoothing
// telemetry (app.log, 2026-09-19 ~14:42:36): a mid-stream "stuck-no-frame"
// reconnect landed on a device list whose winid paths had changed shape
// (plain "winid:0" -> GUID-keyed "winid:{...}", i.e. the host re-enumerated
// its outputs), so resolvePreferredVideoConfig's saved-device lookup missed
// and fell back to devices[0]. That device had no saved config, so this
// merge ran -- and because the agent's /api/video/info happened to report
// "av1" as currently running at that exact instant, the client silently
// adopted av1 for that device path and never switched back.
//
// That's specifically bad on this client (unlike h264/h265): AV1 has no
// Vulkan Video zero-copy decode here (moonlight_cgo_windows.go's decoder
// setup falls straight through to D3D11VA + a CPU sws_scale+overlay path for
// AV1), which cost ~25-45ms/frame at 2560x1600 instead of a few ms --
// visible afterward as "SLOW win_deliver_frame" on nearly every frame and a
// NetGraph DEC bar stuck at 25-30ms, sustained for the rest of that session
// purely because the device had no prior config to fall back to instead.
func TestMergeVideoConfigWithInfo_NeverAutoAdoptsAV1(t *testing.T) {
	cfg := models.VideoDeviceConfig{VideoMode: models.VideoModeH264, VideoWidth: 1280, VideoHeight: 720}
	info := &models.VideoInfoData{
		VideoStatus: models.VideoStatus{Mode: "moonlight", Encoding: "av1", Width: 2560, Height: 1600},
	}

	merged := mergeVideoConfigWithInfo(cfg, info)

	if merged.VideoMode == models.VideoModeAV1 {
		t.Fatalf("mergeVideoConfigWithInfo auto-adopted info.Encoding=%q for a never-configured device -- AV1 has no zero-copy decode on this client and must only be selected explicitly by the user, not inherited from whatever the server happens to be running", info.Encoding)
	}
	if merged.VideoMode != models.VideoModeH264 {
		t.Errorf("VideoMode = %q, want %q (left untouched since av1 must not be auto-adopted)", merged.VideoMode, models.VideoModeH264)
	}
	// Resolution merging is unrelated to this guard and must keep working.
	if merged.VideoWidth != 2560 || merged.VideoHeight != 1600 {
		t.Errorf("resolution = %dx%d, want 2560x1600 (from info)", merged.VideoWidth, merged.VideoHeight)
	}
}
