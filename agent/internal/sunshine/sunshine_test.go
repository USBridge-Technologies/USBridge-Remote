package sunshine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realSessionPreamble is the capability-probe phase Sunshine (itsme228/Sunshine
// fork, macOS/VideoToolbox backend) logs before every negotiation: it tries
// every codec it might use, regardless of what the client actually asked
// for or what ends up streaming. Trimmed verbatim from a real
// ~/.config/sunshine/sunshine.log capture on this machine, with only the
// per-line properties (color coding/depth/range, warnings) removed for
// brevity — the codec-identifying lines are unchanged.
const realSessionPreamble = `[2026-07-19 09:28:19.827]: Info: CLIENT DISCONNECTED
[2026-07-19 09:28:20.926]: Info: Display device configuration is disabled. Reverting any active display device configuration.
[2026-07-19 09:28:20.929]: Info: // Testing for available encoders, this may generate errors. You can safely ignore those errors. //
[2026-07-19 09:28:20.929]: Info: Trying encoder [videotoolbox]
[2026-07-19 09:28:20.930]: Info: Configuring selected display (1) to stream
[2026-07-19 09:28:20.931]: Info: Creating encoder [h264_videotoolbox]
[2026-07-19 09:28:21.172]: Info: Creating encoder [hevc_videotoolbox]
[2026-07-19 09:28:21.343]: Info: Creating encoder [av1_videotoolbox]
[2026-07-19 09:28:21.344]: Error: Couldn't open [av1_videotoolbox]
[2026-07-19 09:28:21.345]: Info: Configuring selected display (1) to stream
[2026-07-19 09:28:21.347]: Info: Creating encoder [hevc_videotoolbox]
[2026-07-19 09:28:21.529]: Info: Found H.264 encoder: h264_videotoolbox [videotoolbox]
[2026-07-19 09:28:21.529]: Info: Found HEVC encoder: hevc_videotoolbox [videotoolbox]
[2026-07-19 09:28:21.529]: Info: Executing [Desktop]`

func realSession(codecEncoderLine string) string {
	return realSessionPreamble + "\n" +
		`[2026-07-19 09:28:21.636]: Info: New streaming session started [active sessions: 1]
[2026-07-19 09:28:21.662]: Info: CLIENT CONNECTED
[2026-07-19 09:28:21.708]: Info: Configuring selected display (1) to stream
` + codecEncoderLine + `
[2026-07-19 09:30:06.855]: Info: CLIENT DISCONNECTED`
}

func newTestProcess(t *testing.T, logContent string) *Process {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "sunshine.log")
	if err := os.WriteFile(logPath, []byte(logContent), 0o644); err != nil {
		t.Fatalf("write fixture log: %v", err)
	}
	return NewProcess("", logPath)
}

// This is the exact regression this fix targets: on this fork, Sunshine logs
// a "Creating encoder [...]" line for EVERY codec it capability-probes, not
// just the one the session actually used. The pre-fix code did an unanchored
// scan for the last "codec"/"encoder" line in the tail, so a real H264
// session followed by an unrelated later probe (e.g. the client's start
// dialog fetching /serverinfo without starting a stream) would misreport
// H265 — even though the actual, currently-running session is plain H264.
func TestCurrentVideoCodec_IgnoresProbeNoiseAfterRealSession(t *testing.T) {
	log := realSession(`[2026-07-19 09:28:21.709]: Info: Creating encoder [h264_videotoolbox]`) + "\n" +
		`[2026-07-19 09:31:00.001]: Info: // Testing for available encoders, this may generate errors. You can safely ignore those errors. //
[2026-07-19 09:31:00.002]: Info: Trying encoder [videotoolbox]
[2026-07-19 09:31:00.010]: Info: Creating encoder [h264_videotoolbox]
[2026-07-19 09:31:00.180]: Info: Creating encoder [hevc_videotoolbox]`

	p := newTestProcess(t, log)
	if got := p.CurrentVideoCodec(); got != "h264" {
		t.Fatalf("CurrentVideoCodec() = %q, want %q (must reflect the real session, not the trailing HEVC probe noise)", got, "h264")
	}
}

func TestCurrentVideoCodec_RealSessionPickedHEVC(t *testing.T) {
	log := realSession(`[2026-07-19 09:28:21.709]: Info: Creating encoder [hevc_videotoolbox]`)

	p := newTestProcess(t, log)
	if got := p.CurrentVideoCodec(); got != "h265" {
		t.Fatalf("CurrentVideoCodec() = %q, want %q", got, "h265")
	}
}

// AV1 fails to open on this Mac's hardware (real capture: "Error: Couldn't
// open [av1_videotoolbox]"). The session actually streamed H264. Detection
// must not be fooled by the AV1 probe attempt earlier in the same window.
func TestCurrentVideoCodec_AV1UnavailableFallsBackToH264InRealSession(t *testing.T) {
	log := realSession(`[2026-07-19 09:28:21.709]: Info: Creating encoder [h264_videotoolbox]`)
	if !strings.Contains(log, "Couldn't open [av1_videotoolbox]") {
		t.Fatal("fixture sanity check failed: expected AV1 probe failure line")
	}

	p := newTestProcess(t, log)
	if got := p.CurrentVideoCodec(); got != "h264" {
		t.Fatalf("CurrentVideoCodec() = %q, want %q", got, "h264")
	}
}

// No client has ever connected yet (only the startup capability probe is in
// the log) — there's no session to anchor on, so this exercises the
// unanchored best-effort fallback path.
func TestCurrentVideoCodec_NoSessionYetUsesUnanchoredFallback(t *testing.T) {
	p := newTestProcess(t, realSessionPreamble)
	// Last "Creating encoder [...]" line in the preamble is the 10-bit HEVC
	// re-probe, so the best-effort fallback should report h265.
	if got := p.CurrentVideoCodec(); got != "h265" {
		t.Fatalf("CurrentVideoCodec() = %q, want %q", got, "h265")
	}
}

func TestCurrentVideoCodec_EmptyLogDefaultsToH264(t *testing.T) {
	p := newTestProcess(t, "")
	if got := p.CurrentVideoCodec(); got != "h264" {
		t.Fatalf("CurrentVideoCodec() = %q, want %q", got, "h264")
	}
}

func TestCurrentVideoCodec_NoLogPathDefaultsToH264(t *testing.T) {
	p := NewProcess("", "")
	if got := p.CurrentVideoCodec(); got != "h264" {
		t.Fatalf("CurrentVideoCodec() = %q, want %q", got, "h264")
	}
}

func TestCreatingEncoderCodec(t *testing.T) {
	cases := []struct {
		line  string
		codec string
		ok    bool
	}{
		{`[t]: Info: Creating encoder [h264_videotoolbox]`, "h264", true},
		{`[t]: Info: Creating encoder [hevc_videotoolbox]`, "h265", true},
		{`[t]: Info: Creating encoder [av1_videotoolbox]`, "av1", true},
		{`[t]: Info: Creating encoder [h264_nvenc]`, "h264", true},
		{`[t]: Info: Creating encoder [hevc_nvenc]`, "h265", true},
		{`[t]: Info: Creating encoder [av1_nvenc]`, "av1", true},
		{`[t]: Info: Creating encoder [h264_qsv]`, "h264", true},
		{`[t]: Info: Creating encoder [hevc_vaapi]`, "h265", true},
		{`[t]: Info: Creating encoder [av1_amf]`, "av1", true},
		{`[t]: Info: Creating encoder [libx264]`, "h264", true},
		{`[t]: Info: Creating encoder [libx265]`, "h265", true},
		{`[t]: Info: Creating encoder [libsvtav1]`, "av1", true},
		{`[t]: Info: Trying encoder [videotoolbox]`, "", false},
		{`[t]: Info: Found H.264 encoder: h264_videotoolbox [videotoolbox]`, "", false},
		{`some unrelated line`, "", false},
	}
	for _, c := range cases {
		codec, ok := creatingEncoderCodec(c.line)
		if codec != c.codec || ok != c.ok {
			t.Errorf("creatingEncoderCodec(%q) = (%q, %v), want (%q, %v)", c.line, codec, ok, c.codec, c.ok)
		}
	}
}
