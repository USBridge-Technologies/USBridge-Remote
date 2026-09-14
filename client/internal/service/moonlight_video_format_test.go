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
		format := moonlightVideoFormat(c.mode, false, false)
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
	if got := moonlightVideoFormat("bogus-mode", false, false); got != 0x0001 {
		t.Errorf("moonlightVideoFormat(bogus) = 0x%04X, want VIDEO_FORMAT_H264 (0x0001)", got)
	}
}

// TestMoonlightVideoFormatColor444 pins the RustShine Pro color upgrade's
// bitmask mapping: h265+color444 must request VIDEO_FORMAT_H265_REXT8_444
// (0x0400, still classified as "h265" by videoFormatCodecName's 0x0F00
// mask), and color444 must be silently ignored for every other mode since
// this project's hardware encode path has no H.264/AV1 4:4:4 profile.
func TestMoonlightVideoFormatColor444(t *testing.T) {
	if got := moonlightVideoFormat(models.VideoModeH265, true, false); got != 0x0400 {
		t.Errorf("moonlightVideoFormat(h265, color444=true) = 0x%04X, want VIDEO_FORMAT_H265_REXT8_444 (0x0400)", got)
	}
	if codec, ok := videoFormatCodecName(0x0400); !ok || codec != models.VideoModeH265 {
		t.Errorf("videoFormatCodecName(0x0400) = (%q, %v), want (%q, true)", codec, ok, models.VideoModeH265)
	}
	if got := moonlightVideoFormat(models.VideoModeH264, true, false); got != 0x0001 {
		t.Errorf("moonlightVideoFormat(h264, color444=true) = 0x%04X, want plain VIDEO_FORMAT_H264 (0x0001) -- color444 has no H264 profile", got)
	}
	if got := moonlightVideoFormat(models.VideoModeAV1, true, false); got != 0x1000 {
		t.Errorf("moonlightVideoFormat(av1, color444=true) = 0x%04X, want plain VIDEO_FORMAT_AV1_MAIN8 (0x1000) -- color444 has no AV1 profile wired up", got)
	}
}

// TestMoonlightVideoFormatHdr pins the RustShine HDR color upgrade's bitmask
// mapping -- mirrors TestMoonlightVideoFormatColor444 exactly, for
// VIDEO_FORMAT_H265_MAIN10 (0x0200) instead of REXT8_444, and is silently
// ignored outside VideoModeH265 for the same reason (no H.264/AV1 Main10
// profile wired up in this project's backends).
func TestMoonlightVideoFormatHdr(t *testing.T) {
	if got := moonlightVideoFormat(models.VideoModeH265, false, true); got != 0x0200 {
		t.Errorf("moonlightVideoFormat(h265, hdr=true) = 0x%04X, want VIDEO_FORMAT_H265_MAIN10 (0x0200)", got)
	}
	if codec, ok := videoFormatCodecName(0x0200); !ok || codec != models.VideoModeH265 {
		t.Errorf("videoFormatCodecName(0x0200) = (%q, %v), want (%q, true)", codec, ok, models.VideoModeH265)
	}
	if got := moonlightVideoFormat(models.VideoModeH264, false, true); got != 0x0001 {
		t.Errorf("moonlightVideoFormat(h264, hdr=true) = 0x%04X, want plain VIDEO_FORMAT_H264 (0x0001) -- hdr has no H264 profile", got)
	}
	if got := moonlightVideoFormat(models.VideoModeAV1, false, true); got != 0x1000 {
		t.Errorf("moonlightVideoFormat(av1, hdr=true) = 0x%04X, want plain VIDEO_FORMAT_AV1_MAIN8 (0x1000) -- hdr has no AV1 profile wired up", got)
	}
}

// TestMoonlightVideoFormatColor444AndHdrCombine pins the combined-axes
// request: both checkboxes together must request the OR of
// VIDEO_FORMAT_H265_REXT10_444 (0x0800, the ideal combined 4:4:4+10-bit
// profile) with its two single-feature fallbacks, VIDEO_FORMAT_H265_MAIN10
// (0x0200) and VIDEO_FORMAT_H265_REXT8_444 (0x0400) -- NOT 0x0800 alone.
//
// Regression pin for a real bug (2026-09-14): no backend implements the
// combined REXT10_444 profile, so a server's ServerCodecModeSupport never
// has that bit. When this function returned only 0x0800,
// RtspConnection.c's negotiation cascade ANDs supportedVideoFormats against
// each candidate bit in turn (REXT10_444, then MAIN10, then REXT8_444) --
// with only 0x0800 set, the MAIN10 and REXT8_444 fallback checks failed
// too (0x0800 & 0x0200 == 0, 0x0800 & 0x0400 == 0), so negotiation dropped
// all the way to plain VIDEO_FORMAT_H265, silently losing HDR AND 4:4:4
// both instead of degrading to whichever one the server actually supports.
// Confirmed live via serverCodecModeSupport=0x80301 (SCM_HEVC_MAIN10 |
// SCM_HEVC_REXT8_444 set, SCM_HEVC_REXT10_444 not set) negotiating down to
// 0x0100 instead of 0x0200.
func TestMoonlightVideoFormatColor444AndHdrCombine(t *testing.T) {
	want := 0x0800 | 0x0200 | 0x0400
	if got := moonlightVideoFormat(models.VideoModeH265, true, true); got != want {
		t.Errorf("moonlightVideoFormat(h265, color444=true, hdr=true) = 0x%04X, want REXT10_444|MAIN10|REXT8_444 (0x%04X)", got, want)
	}
	if codec, ok := videoFormatCodecName(int32(want)); !ok || codec != models.VideoModeH265 {
		t.Errorf("videoFormatCodecName(0x%04X) = (%q, %v), want (%q, true)", want, codec, ok, models.VideoModeH265)
	}
}
