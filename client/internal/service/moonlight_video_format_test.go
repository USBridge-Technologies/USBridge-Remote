//go:build (darwin || ios || linux) && !android && cgo

package service

import (
	"testing"

	"usbridge-client/internal/models"
)

// TestVideoFormatRoundTrip pins the VIDEO_FORMAT_* bitmask mapping used on
// both sides of the negotiated-codec pipeline: moonlightVideoFormat() picks
// the bitmask the client requests when starting a connection, and
// videoFormatCodecName() decodes the bitmask moonlight-common-c reports back
// via dr_setup's NegotiatedVideoFormat. If these ever drift out of sync, the
// client would ask for one codec but report a different one as "negotiated".
func TestVideoFormatRoundTrip(t *testing.T) {
	cases := []struct {
		mode  string
		codec string
	}{
		{models.VideoModeH264, "h264"},
		{models.VideoModeH265, "h265"},
		{models.VideoModeAV1, "av1"},
	}

	for _, c := range cases {
		format := moonlightVideoFormat(c.mode)
		gotCodec, ok := videoFormatCodecName(int32(format))
		if !ok {
			t.Errorf("videoFormatCodecName(moonlightVideoFormat(%q)=0x%04X) reported no codec", c.mode, format)
			continue
		}
		if gotCodec != c.codec {
			t.Errorf("mode %q -> format 0x%04X -> codec %q, want %q", c.mode, format, gotCodec, c.codec)
		}
	}
}

func TestVideoFormatCodecNameUnknownAndUnset(t *testing.T) {
	if _, ok := videoFormatCodecName(-1); ok {
		t.Error("videoFormatCodecName(-1) should report no codec (sentinel for \"no session yet\")")
	}
	if _, ok := videoFormatCodecName(0); ok {
		t.Error("videoFormatCodecName(0) should report no codec (no format bits set)")
	}
}

func TestMoonlightVideoFormatDefaultsToH264(t *testing.T) {
	if got := moonlightVideoFormat("bogus-mode"); got != 0x0001 {
		t.Errorf("moonlightVideoFormat(bogus) = 0x%04X, want VIDEO_FORMAT_H264 (0x0001)", got)
	}
}
