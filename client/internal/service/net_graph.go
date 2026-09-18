package service

import (
	"image"
	"image/color"
	"image/draw"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Net Graph is an optional live HUD overlay, off by default: a small
// TF2 net_graph-style box in the bottom-left corner showing packet
// arrival/loss, RTT, FEC recovery, render fps, decode/render latency, and
// host (capture+encode) latency -- none of which is otherwise visible to
// the operator today even though moonlight-common-c already computes or
// carries most of it: LiGetRTPVideoStats and LiGetEstimatedRttInfo (both
// wired in moonlight_cgo_wrapper.go), plus DECODE_UNIT.frameHostProcessingLatency,
// a standard Sunshine-protocol per-frame header field carried on the video
// stream itself (see moonlight_cgo_shared.h's dr_submit and
// GetLastHostLatencyMs). All of it is pure client-side protocol
// accounting -- no side channel, no new endpoint -- so it works with ANY
// server that fills these fields in: stock Sunshine/Apollo builds that
// support it, and our own rust-shine once it does the same (see rust-shine's
// crates/rtp-video/src/packetizer.rs, which already has the wire field,
// just never fed a real value).
//
// Same split as AI Vision (ai_vision.go): this file has NO build tag and
// touches no cgo directly -- every platform-specific piece (reading the
// network counters, reading render fps/decode latency, pushing the built
// HUD image to the screen) is a nil-able func var, wired from the
// platform-specific file that actually implements it (moonlight_cgo_wrapper.go
// for the network counters; metal_video_darwin.go for render/decode/push on
// macOS). That's what makes porting this to the Linux/Windows Vulkan/GL
// path later (see metal_video_darwin.go's doc comment) a matter of wiring a
// few more hooks, not rewriting buildNetGraphHUD.
const (
	netGraphInterval   = 100 * time.Millisecond // 10Hz -- fast enough that a single dropped packet or a one-frame stall shows up as its own visible tick
	netGraphHistoryLen = 220                    // ring buffer length; also the graph plot width in px
	netGraphCanvasW    = 256
	netGraphCanvasH    = 150
)

// netGraphRawNetworkStats is a tag-free mirror of RTPVideoStats
// (moonlight_cgo_wrapper.go) plus the RTT estimate, so this file can stay
// buildable on every platform regardless of which (if any) cgo wrapper is
// actually compiled in. Cumulative counters, exactly as moonlight-common-c
// reports them -- collectNetGraphSample diffs them into per-tick rates.
type netGraphRawNetworkStats struct {
	PacketCountVideo        uint32
	PacketCountFec          uint32
	PacketCountFecRecovered uint32
	PacketCountFecFailed    uint32
	PacketCountOOS          uint32
	PacketCountInvalid      uint32
	RTTMs                   float64
	RTTVarianceMs           float64
	RTTValid                bool
	// HostLatencyMs is the most recently received frame's host
	// processing (capture+encode) time, straight from the standard
	// Sunshine-protocol frame header (DECODE_UNIT.frameHostProcessingLatency)
	// -- works with ANY server that fills this field in, not just
	// rust-shine, and needs no side channel at all: it rides the video
	// stream every server already sends. HostLatencyValid is false when
	// the host doesn't provide it (see GetLastHostLatencyMs's doc comment).
	HostLatencyMs    float64
	HostLatencyValid bool
}

// NetGraphSample is one 100ms tick's worth of HUD data, kept in a rolling
// ring buffer (netGraphSamples) that buildNetGraphHUD renders from.
type NetGraphSample struct {
	RTTMs         float64
	RTTVarianceMs float64
	RTTValid      bool

	// Per-tick deltas, NOT cumulative -- "how many of these happened in the
	// last ~100ms". PacketsFecFailed is the closest proxy available today
	// to "a frame was lost/corrupted": moonlight-common-c doesn't expose a
	// dedicated frame-loss counter, only connectionDetectedFrameLoss's
	// internal accounting, which is why FecFailed doubles as that signal
	// here.
	PacketsVideo   uint32
	PacketsFec     uint32
	FecRecovered   uint32
	FecFailed      uint32
	PacketsOOS     uint32
	PacketsInvalid uint32

	RenderFPS float64
	DecodeMs  float64

	// HostLatencyMs/HostLatencyValid: see netGraphRawNetworkStats' own doc
	// comment -- this is "how fast is the host capturing+encoding", straight
	// from the video stream's standard per-frame header field.
	HostLatencyMs    float64
	HostLatencyValid bool
}

var (
	// Platform hooks -- nil until a platform-specific init() wires them
	// (see metal_video_darwin.go, and moonlight_cgo_wrapper.go's own init
	// below). Reading through a nil hook is just "no data from this source
	// yet", never a crash -- same contract as ai_vision.go's
	// aiVisionMetalPush/aiVisionMetalClear.
	netGraphNetworkStatsFn func() netGraphRawNetworkStats
	netGraphRenderFPS      func() float64
	netGraphDecodeMs       func() float64
	netGraphMetalPush      func(img *image.RGBA)
	netGraphMetalClear     func()

	netGraphEnabled  atomic.Bool
	netGraphLoopOnce sync.Once

	netGraphPrevMu    sync.Mutex
	netGraphPrevRaw   netGraphRawNetworkStats
	netGraphPrevValid bool

	netGraphMu      sync.Mutex
	netGraphSamples []NetGraphSample
)

// SetNetGraphEnabled turns the HUD on or off. Wired to the "Net Graph"
// checkbox in the video settings popup (see gui/view/video_start_dialog.go)
// -- takes effect immediately, independent of Start/Apply, since it only
// affects local rendering. Disabling clears the cached samples and the
// on-screen HUD image right away so a stale readout never lingers after the
// checkbox is unticked.
func SetNetGraphEnabled(enabled bool) {
	wasEnabled := netGraphEnabled.Swap(enabled)
	if enabled {
		netGraphLoopOnce.Do(func() { go netGraphLoop() })
		// Re-enabling after a while: the cumulative counters kept moving
		// the whole time (moonlight-common-c doesn't know the checkbox
		// exists), so the very next diff against a stale netGraphPrevRaw
		// would look like a huge loss/FEC spike that never really
		// happened in that one tick. Force a fresh baseline instead.
		netGraphPrevMu.Lock()
		netGraphPrevValid = false
		netGraphPrevMu.Unlock()
	} else {
		netGraphMu.Lock()
		netGraphSamples = nil
		netGraphMu.Unlock()
		if clear := netGraphMetalClear; clear != nil {
			clear()
		}
	}
	if wasEnabled != enabled {
		logrus.Infof("📊 [Net Graph] %s", map[bool]string{true: "enabled", false: "disabled"}[enabled])
	}
}

// NetGraphEnabled reports the HUD checkbox's current state.
func NetGraphEnabled() bool {
	return netGraphEnabled.Load()
}

// netGraphLoop runs for the lifetime of the process once started (first
// SetNetGraphEnabled(true) call) -- cheaper to leave ticking in the
// background than to tear down/restart per stream, and the disabled case
// costs one atomic load per tick, same philosophy as
// ai_vision.go's ApplyAIVisionOverlay.
func netGraphLoop() {
	ticker := time.NewTicker(netGraphInterval)
	defer ticker.Stop()
	for range ticker.C {
		if !netGraphEnabled.Load() {
			continue
		}
		sample := collectNetGraphSample()

		netGraphMu.Lock()
		netGraphSamples = append(netGraphSamples, sample)
		if len(netGraphSamples) > netGraphHistoryLen {
			netGraphSamples = netGraphSamples[len(netGraphSamples)-netGraphHistoryLen:]
		}
		samples := append([]NetGraphSample(nil), netGraphSamples...)
		netGraphMu.Unlock()

		if push := netGraphMetalPush; push != nil {
			push(buildNetGraphHUD(samples))
		}
	}
}

// collectNetGraphSample reads every wired platform hook once and turns the
// cumulative network counters into this tick's deltas.
func collectNetGraphSample() NetGraphSample {
	var raw netGraphRawNetworkStats
	if fn := netGraphNetworkStatsFn; fn != nil {
		raw = fn()
	}

	netGraphPrevMu.Lock()
	prev := netGraphPrevRaw
	hadPrev := netGraphPrevValid
	netGraphPrevRaw = raw
	netGraphPrevValid = true
	netGraphPrevMu.Unlock()

	s := NetGraphSample{
		RTTMs:            raw.RTTMs,
		RTTVarianceMs:    raw.RTTVarianceMs,
		RTTValid:         raw.RTTValid,
		HostLatencyMs:    raw.HostLatencyMs,
		HostLatencyValid: raw.HostLatencyValid,
	}
	if hadPrev {
		s.PacketsVideo = netGraphDeltaU32(raw.PacketCountVideo, prev.PacketCountVideo)
		s.PacketsFec = netGraphDeltaU32(raw.PacketCountFec, prev.PacketCountFec)
		s.FecRecovered = netGraphDeltaU32(raw.PacketCountFecRecovered, prev.PacketCountFecRecovered)
		s.FecFailed = netGraphDeltaU32(raw.PacketCountFecFailed, prev.PacketCountFecFailed)
		s.PacketsOOS = netGraphDeltaU32(raw.PacketCountOOS, prev.PacketCountOOS)
		s.PacketsInvalid = netGraphDeltaU32(raw.PacketCountInvalid, prev.PacketCountInvalid)
	}
	if fn := netGraphRenderFPS; fn != nil {
		s.RenderFPS = fn()
	}
	if fn := netGraphDecodeMs; fn != nil {
		s.DecodeMs = fn()
	}
	return s
}

// netGraphDeltaU32 diffs two cumulative counters, clamping to 0 instead of
// wrapping when cur < prev -- which happens once, harmlessly, whenever a
// fresh session resets moonlight-common-c's statics to zero.
func netGraphDeltaU32(cur, prev uint32) uint32 {
	if cur < prev {
		return 0
	}
	return cur - prev
}

// ─────────────────────────────────────────────────────────────────────────────
// HUD rendering -- pure Go, no cgo, no platform dependency. This is the
// piece a Linux/Windows port reuses unchanged (see metal_video_darwin.go's
// doc comment); only the "hand this image to the screen" call differs.
// ─────────────────────────────────────────────────────────────────────────────

var (
	netGraphBg     = color.RGBA{R: 0x0a, G: 0x0c, B: 0x0e, A: 0xc8} // translucent near-black, like TF2's net_graph background
	netGraphBorder = color.RGBA{R: 0x33, G: 0x37, B: 0x2f, A: 0xff}
	netGraphText   = color.RGBA{R: 0xeb, G: 0xff, B: 0xbc, A: 0xff}
	netGraphDim    = color.RGBA{R: 0x8a, G: 0x94, B: 0x84, A: 0xff}
	netGraphGood   = color.RGBA{R: 0x4a, G: 0xd9, B: 0x6a, A: 0xff}
	netGraphWarn   = color.RGBA{R: 0xe0, G: 0xc4, B: 0x3a, A: 0xff}
	netGraphBad    = color.RGBA{R: 0xe0, G: 0x4a, B: 0x3a, A: 0xff}
)

// buildNetGraphHUD draws the current numeric readouts plus scrolling
// history graphs (latency, loss/FEC, decode latency) onto a fixed-size
// canvas, most-recent sample at the right edge, scrolling left -- same
// convention as TF2's net_graph. samples is ordered oldest-first; an empty
// slice still produces a valid (mostly blank) canvas.
func buildNetGraphHUD(samples []NetGraphSample) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, netGraphCanvasW, netGraphCanvasH))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: netGraphBg}, image.Point{}, draw.Src)
	netGraphDrawBorder(img)

	var latest NetGraphSample
	if len(samples) > 0 {
		latest = samples[len(samples)-1]
	}

	const marginX = 6
	row := 12
	rttColor := netGraphGood
	switch {
	case !latest.RTTValid:
		rttColor = netGraphDim
	case latest.RTTMs >= 100:
		rttColor = netGraphBad
	case latest.RTTMs >= 50:
		rttColor = netGraphWarn
	}
	rttText := "RTT -- "
	if latest.RTTValid {
		rttText = netGraphFmtMs("RTT", latest.RTTMs) + " ±" + netGraphFmtMs("", latest.RTTVarianceMs)
	}
	netGraphDrawText(img, marginX, row, rttText, rttColor)

	lossPct := netGraphLossPercent(latest)
	lossColor := netGraphGood
	switch {
	case lossPct >= 2:
		lossColor = netGraphBad
	case lossPct > 0:
		lossColor = netGraphWarn
	}
	netGraphDrawText(img, 140, row, netGraphFmtPct("LOSS", lossPct), lossColor)

	row += 12
	fecColor := netGraphGood
	if latest.FecFailed > 0 {
		fecColor = netGraphBad
	} else if latest.FecRecovered > 0 {
		fecColor = netGraphWarn
	}
	netGraphDrawText(img, marginX, row, netGraphFmtCounts("FEC rec/fail", latest.FecRecovered, latest.FecFailed), fecColor)

	row += 12
	decColor := netGraphGood
	switch {
	case latest.DecodeMs >= 33:
		decColor = netGraphBad
	case latest.DecodeMs >= 16:
		decColor = netGraphWarn
	}
	netGraphDrawText(img, marginX, row, netGraphFmtFPS("FPS", latest.RenderFPS), netGraphText)
	netGraphDrawText(img, 140, row, netGraphFmtMs("DEC", latest.DecodeMs), decColor)

	row += 12
	if latest.HostLatencyValid {
		hostColor := netGraphGood
		switch {
		case latest.HostLatencyMs >= 20:
			hostColor = netGraphBad
		case latest.HostLatencyMs >= 10:
			hostColor = netGraphWarn
		}
		netGraphDrawText(img, marginX, row, netGraphFmtMs("HOST", latest.HostLatencyMs), hostColor)
		row += 12
	}

	graphTop := row + 4
	graphH := (netGraphCanvasH - graphTop - marginX - 2*4) / 3
	if graphH < 10 {
		graphH = 10
	}
	graphX, graphW := marginX, netGraphCanvasW-2*marginX

	netGraphDrawLineGraph(img, graphX, graphTop, graphW, graphH, samples, func(s NetGraphSample) (float64, bool) {
		if !s.RTTValid {
			return 0, false
		}
		return s.RTTMs, true
	}, 50)
	graphTop += graphH + 4

	netGraphDrawEventGraph(img, graphX, graphTop, graphW, graphH, samples)
	graphTop += graphH + 4

	netGraphDrawLineGraph(img, graphX, graphTop, graphW, graphH, samples, func(s NetGraphSample) (float64, bool) {
		return s.DecodeMs, s.DecodeMs > 0
	}, 33)

	return img
}

func netGraphDrawBorder(img *image.RGBA) {
	b := img.Bounds()
	for x := b.Min.X; x < b.Max.X; x++ {
		img.SetRGBA(x, b.Min.Y, netGraphBorder)
		img.SetRGBA(x, b.Max.Y-1, netGraphBorder)
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		img.SetRGBA(b.Min.X, y, netGraphBorder)
		img.SetRGBA(b.Max.X-1, y, netGraphBorder)
	}
}

func netGraphLossPercent(s NetGraphSample) float64 {
	total := s.PacketsVideo + s.PacketsFec
	if total == 0 {
		return 0
	}
	lost := s.FecFailed + s.PacketsInvalid
	return float64(lost) / float64(total) * 100
}

// netGraphDrawLineGraph plots one scalar per sample as a 1px-wide bar
// scrolling right-to-left (most recent sample at the graph's right edge),
// colored by the same green/yellow/red thresholds the numeric readouts use.
// valueOf returning ok=false (e.g. no RTT estimate yet) leaves that column
// blank rather than drawing a misleading zero.
func netGraphDrawLineGraph(img *image.RGBA, x0, y0, w, h int, samples []NetGraphSample, valueOf func(NetGraphSample) (float64, bool), warnAt float64) {
	n := len(samples)
	start := 0
	if n > w {
		start = n - w
	}
	for i := start; i < n; i++ {
		v, ok := valueOf(samples[i])
		if !ok {
			continue
		}
		col := x0 + w - (n - i)
		if col < x0 || col >= x0+w {
			continue
		}
		maxVal := warnAt * 2
		barH := int(v / maxVal * float64(h))
		if barH > h {
			barH = h
		}
		if barH < 1 {
			barH = 1
		}
		c := netGraphGood
		switch {
		case v >= warnAt*2:
			c = netGraphBad
		case v >= warnAt:
			c = netGraphWarn
		}
		for dy := 0; dy < barH; dy++ {
			img.SetRGBA(col, y0+h-1-dy, c)
		}
	}
}

// netGraphDrawEventGraph plots FEC-recovered (yellow) and FEC-failed/loss
// (red) counts per tick, stacked from the baseline up. Any non-zero count
// gets at least a few visible pixels regardless of magnitude -- the whole
// point is that a single lost or recovered packet must never be invisible
// just because it's a "small" number next to a 10Hz sample rate.
func netGraphDrawEventGraph(img *image.RGBA, x0, y0, w, h int, samples []NetGraphSample) {
	const minBar = 3
	n := len(samples)
	start := 0
	if n > w {
		start = n - w
	}
	for i := start; i < n; i++ {
		s := samples[i]
		col := x0 + w - (n - i)
		if col < x0 || col >= x0+w {
			continue
		}
		if s.FecRecovered == 0 && s.FecFailed == 0 {
			img.SetRGBA(col, y0+h-1, netGraphDim) // faint baseline tick so the axis itself is visible
			continue
		}
		y := y0 + h - 1
		if s.FecRecovered > 0 {
			bar := minBar + int(s.FecRecovered)
			if bar > h {
				bar = h
			}
			for dy := 0; dy < bar; dy++ {
				img.SetRGBA(col, y-dy, netGraphWarn)
			}
			y -= bar
		}
		if s.FecFailed > 0 {
			bar := minBar + int(s.FecFailed)*2
			if y-bar < y0 {
				bar = y - y0
			}
			for dy := 0; dy < bar; dy++ {
				img.SetRGBA(col, y-dy, netGraphBad)
			}
		}
	}
}

func netGraphDrawText(img *image.RGBA, x, baselineY int, text string, c color.Color) {
	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: c},
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, baselineY),
	}
	d.DrawString(text)
}

func netGraphFmtMs(label string, ms float64) string {
	if label == "" {
		return netGraphFmtFloat(ms) + "ms"
	}
	return label + " " + netGraphFmtFloat(ms) + "ms"
}

func netGraphFmtPct(label string, pct float64) string {
	return label + " " + netGraphFmtFloat(pct) + "%"
}

func netGraphFmtCounts(label string, a, b uint32) string {
	return label + " " + netGraphFmtUint(a) + "/" + netGraphFmtUint(b)
}

func netGraphFmtFPS(label string, fps float64) string {
	return label + " " + netGraphFmtFloat(fps)
}

// netGraphFmtFloat formats to one decimal place without pulling in fmt's
// full formatting machinery on this hot-ish (10Hz) path.
func netGraphFmtFloat(v float64) string {
	if v < 0 {
		v = 0
	}
	whole := int(v)
	frac := int((v-float64(whole))*10 + 0.5)
	if frac >= 10 {
		whole++
		frac = 0
	}
	return netGraphFmtUint(uint32(whole)) + "." + netGraphFmtUint(uint32(frac))
}

func netGraphFmtUint(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
