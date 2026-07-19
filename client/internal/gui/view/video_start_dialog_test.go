package view

import (
	"os"
	"testing"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2/test"
)

func TestMain(m *testing.M) {
	i18n.Init("en")
	os.Exit(m.Run())
}

func supportedModesFixture() []models.VideoTransportMode {
	return []models.VideoTransportMode{
		{ID: models.VideoModeH264, Name: "H.264", Transport: "rtp", Encoding: "h264"},
		{ID: models.VideoModeH265, Name: "H.265", Transport: "rtp", Encoding: "h265"},
		{ID: models.VideoModeAV1, Name: "AV1", Transport: "rtp", Encoding: "av1"},
	}
}

// This is the exact regression: the agent's videoInfo API sets info.Mode to a
// transport label ("moonlight"), not a codec, while the real codec hint lives
// in info.Encoding. Configure() used to read info.Mode, which matched no
// button, silently falling back to the first entry in the mode list — always
// H264, regardless of what info.Encoding said or what the user had picked.
func TestConfigure_UsesEncodingNotMode_H265(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	vsd := NewVideoStartDialog(win)

	info := &models.VideoInfoData{
		VideoStatus: models.VideoStatus{
			Mode:           "moonlight", // transport label, NOT a codec
			Encoding:       "h265",      // the actual agent codec hint
			SupportedModes: supportedModesFixture(),
		},
	}

	vsd.Configure(info, 1920, 1080, 60, "20M")

	if got := vsd.selectedModeID(); got != models.VideoModeH265 {
		t.Fatalf("selectedModeID() = %q, want %q (info.Mode=%q must NOT be used as the codec)", got, models.VideoModeH265, info.Mode)
	}
	btn, ok := vsd.modeButtons[models.VideoModeH265]
	if !ok || !btn.active {
		t.Fatalf("H265 button should be active after Configure(); modeButtons=%v", vsd.modeButtons)
	}
	if h264Btn, ok := vsd.modeButtons[models.VideoModeH264]; ok && h264Btn.active {
		t.Fatalf("H264 button should NOT be active — this is the exact 'always shows H264' bug")
	}
}

func TestConfigure_UsesEncodingNotMode_AV1(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	vsd := NewVideoStartDialog(win)

	info := &models.VideoInfoData{
		VideoStatus: models.VideoStatus{
			Mode:           "moonlight",
			Encoding:       "av1",
			SupportedModes: supportedModesFixture(),
		},
	}

	vsd.Configure(info, 1920, 1080, 60, "20M")

	if got := vsd.selectedModeID(); got != models.VideoModeAV1 {
		t.Fatalf("selectedModeID() = %q, want %q", got, models.VideoModeAV1)
	}
}

// The live negotiated codec (from the running Moonlight session) is
// authoritative and must win over the agent's pre-connection guess in
// info.Encoding whenever a session is actually active.
func TestConfigure_LiveCodecProviderOverridesAgentHint(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	vsd := NewVideoStartDialog(win)
	vsd.SetLiveCodecProvider(func() (string, bool) { return models.VideoModeAV1, true })

	info := &models.VideoInfoData{
		VideoStatus: models.VideoStatus{
			Mode:           "moonlight",
			Encoding:       "h264", // agent's stale/pre-connect guess
			SupportedModes: supportedModesFixture(),
		},
	}

	vsd.Configure(info, 1920, 1080, 60, "20M")

	if got := vsd.selectedModeID(); got != models.VideoModeAV1 {
		t.Fatalf("selectedModeID() = %q, want %q (live-negotiated codec must win over agent hint)", got, models.VideoModeAV1)
	}
}

// When the live provider reports no active session (ok=false), Configure()
// must fall back to the agent's info.Encoding hint rather than treating the
// zero value as a real codec.
func TestConfigure_LiveCodecProviderInactiveFallsBackToAgentHint(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	vsd := NewVideoStartDialog(win)
	vsd.SetLiveCodecProvider(func() (string, bool) { return "", false })

	info := &models.VideoInfoData{
		VideoStatus: models.VideoStatus{
			Mode:           "moonlight",
			Encoding:       "h265",
			SupportedModes: supportedModesFixture(),
		},
	}

	vsd.Configure(info, 1920, 1080, 60, "20M")

	if got := vsd.selectedModeID(); got != models.VideoModeH265 {
		t.Fatalf("selectedModeID() = %q, want %q", got, models.VideoModeH265)
	}
}

func TestConfigure_NoInfoDefaultsToH264(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	vsd := NewVideoStartDialog(win)

	vsd.Configure(nil, 1920, 1080, 60, "20M")

	if got := vsd.selectedModeID(); got != models.VideoModeH264 {
		t.Fatalf("selectedModeID() = %q, want %q", got, models.VideoModeH264)
	}
}

// If the device only advertises H264 (e.g. AV1 unsupported on this GPU) but
// the agent hint/live codec says AV1 anyway, setSelectedModeID must fall back
// deterministically instead of silently accepting an offer the device can't
// actually satisfy.
func TestConfigure_CodecNotOfferedByDeviceFallsBackToFirstAvailable(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	vsd := NewVideoStartDialog(win)

	info := &models.VideoInfoData{
		VideoStatus: models.VideoStatus{
			Mode:     "moonlight",
			Encoding: "av1",
			SupportedModes: []models.VideoTransportMode{
				{ID: models.VideoModeH264, Name: "H.264", Transport: "rtp", Encoding: "h264"},
			},
		},
	}

	vsd.Configure(info, 1920, 1080, 60, "20M")

	if got := vsd.selectedModeID(); got != models.VideoModeH264 {
		t.Fatalf("selectedModeID() = %q, want %q (only mode the device offers)", got, models.VideoModeH264)
	}
}
