package controller

import (
	"testing"

	"usbridge-client/internal/models"

	"fyne.io/fyne/v2/test"
)

// TestCorrectSelectedVideoDevicePath_SelfHealsStaleSelection is the
// regression test for the second half of the "picked H265, still streams
// H264" bug (found live via [CODEC-TRACE] logging, 2026-09-18): once
// prefs.SelectedDevice points at a device no longer present in the live
// device list (e.g. a virtual display whose agent has since restarted and
// forgotten it -- the agent only keeps virtual displays in memory, see
// server.go's virtualDisplayCreate), selectedVideoDevicePath() used to keep
// returning that phantom path forever. Every ShowCurrentVideoSettings
// (header/status-bar gear) would then open the settings popup for a device
// that doesn't exist; any codec change the user made there got saved under
// that same phantom path and never reached the device actually streaming --
// confirmed live: applying H265 to "virtual:1920x1080@240" while only
// "winid:{...}" was actually enumerated left the real device on its own
// stale "h264" config, forever, with no way for the client to notice.
//
// correctSelectedVideoDevicePath (called from resolvePreferredVideoConfig's
// device-list-fallback branch) fixes this by repointing SelectedDevice at
// whatever device the fallback actually picked, without touching either
// device's saved per-device config.
func TestCorrectSelectedVideoDevicePath_SelfHealsStaleSelection(t *testing.T) {
	test.NewApp()

	phantom := "virtual:1920x1080@240"
	real := "winid:{4698b7a1-33b8-5803-bf10-fccb17c020de}"

	// User applied H265 to the (since-vanished) virtual device -- this is
	// what ShowVideoDeviceSettings' onApply -> applyVideoDeviceConfig ->
	// saveVideoDeviceConfig does.
	saveVideoDeviceConfig(models.VideoDeviceConfig{DevicePath: phantom, VideoMode: models.VideoModeH265})
	// The real device has its own, separately-saved config.
	saveVideoDeviceConfig(models.VideoDeviceConfig{DevicePath: real, VideoMode: models.VideoModeH264})

	if got := selectedVideoDevicePath(); got != real {
		t.Fatalf("selectedVideoDevicePath() = %q after saving %q last, want %q (sanity check on saveVideoDeviceConfig itself)", got, real, real)
	}

	// Re-select the phantom device (mirrors what actually happened live:
	// SelectedDevice pointed at the virtual device, saved most recently).
	saveVideoDeviceConfig(models.VideoDeviceConfig{DevicePath: phantom, VideoMode: models.VideoModeH265})
	if got := selectedVideoDevicePath(); got != phantom {
		t.Fatalf("selectedVideoDevicePath() = %q, want %q before simulating the device-list-fallback", got, phantom)
	}

	// This is resolvePreferredVideoConfig's fallback path: phantom isn't in
	// the current device list, so it falls back to `real` and must correct
	// the stale pointer.
	correctSelectedVideoDevicePath(real)

	if got := selectedVideoDevicePath(); got != real {
		t.Errorf("selectedVideoDevicePath() = %q after correctSelectedVideoDevicePath(%q), want %q -- stale selection did not self-heal, every future popup open would keep targeting the phantom device", got, real, real)
	}

	// The phantom device's own saved config (VideoMode=h265) must survive
	// untouched -- if the virtual device ever comes back (agent restarted
	// again, re-created), its H265 choice should still be there.
	phantomCfg := loadSavedVideoDeviceConfig(phantom, "")
	if phantomCfg.VideoMode != models.VideoModeH265 {
		t.Errorf("phantom device's saved VideoMode = %q, want %q (correctSelectedVideoDevicePath must not touch per-device configs)", phantomCfg.VideoMode, models.VideoModeH265)
	}
	realCfg := loadSavedVideoDeviceConfig(real, "")
	if realCfg.VideoMode != models.VideoModeH264 {
		t.Errorf("real device's saved VideoMode = %q, want %q (unaffected by the correction)", realCfg.VideoMode, models.VideoModeH264)
	}
}

// TestCorrectSelectedVideoDevicePath_NoopWhenAlreadyCorrect guards against a
// pointless preferences write (and the log spam that would come with it) on
// every single reconcile once the pointer is already healed.
func TestCorrectSelectedVideoDevicePath_NoopWhenAlreadyCorrect(t *testing.T) {
	test.NewApp()

	path := "winid:{4698b7a1-33b8-5803-bf10-fccb17c020de}"
	saveVideoDeviceConfig(models.VideoDeviceConfig{DevicePath: path, VideoMode: models.VideoModeH264})

	correctSelectedVideoDevicePath(path)

	if got := selectedVideoDevicePath(); got != path {
		t.Errorf("selectedVideoDevicePath() = %q, want %q", got, path)
	}
}

// TestCorrectSelectedVideoDevicePath_IgnoresEmptyPath guards against ever
// clearing SelectedDevice outright -- callers only pass a real device.Path
// from the live device list, but an empty path must be a no-op rather than
// wiping the user's selection.
func TestCorrectSelectedVideoDevicePath_IgnoresEmptyPath(t *testing.T) {
	test.NewApp()

	path := "winid:{4698b7a1-33b8-5803-bf10-fccb17c020de}"
	saveVideoDeviceConfig(models.VideoDeviceConfig{DevicePath: path, VideoMode: models.VideoModeH264})

	correctSelectedVideoDevicePath("")

	if got := selectedVideoDevicePath(); got != path {
		t.Errorf("selectedVideoDevicePath() = %q after correctSelectedVideoDevicePath(\"\"), want unchanged %q", got, path)
	}
}
