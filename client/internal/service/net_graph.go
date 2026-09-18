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
// TF2 net_graph-style box in the bottom-right corner showing packet
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
	netGraphHistoryLen = 280                    // ring buffer length; also the graph plot width in px
	netGraphCanvasW    = 280
	netGraphCanvasH    = 180
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
	// JitterMs/PlayoutDelayMs: the client-side adaptive playout buffer's
	// own live jitter estimate and currently-applied extra delay
	// (LiGetPlayoutJitterUs/LiGetPlayoutAppliedDelayUs) -- arrival-time
	// variance measured locally, distinct from RTTVarianceMs (which is
	// ENet's network-level RTT variance estimate).
	JitterMs       float64
	PlayoutDelayMs float64
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
	JitterMs         float64
	PlayoutDelayMs   float64
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

	// netGraphCachedImg holds the most recently built HUD canvas for
	// ApplyNetGraphOverlay below -- the CPU-buffer blit path Linux/Windows
	// use instead of a native compositor layer (see that function's doc
	// comment). Updated every tick alongside the netGraphMetalPush call,
	// nil when disabled.
	netGraphCachedImg atomic.Pointer[image.RGBA]
)

// netGraphHudMargin is the gap, in pixels, between the HUD box and the
// bottom/right edges of the frame -- shared by every platform's anchor math
// (metal_video_impl_darwin.m's HUD_MARGIN, metal_video_impl_ios.m's mirror
// of it, and netGraphBlitOverlay below) so the HUD sits the same visual
// distance from the corner everywhere.
const netGraphHudMargin = 12

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
		netGraphCachedImg.Store(nil)
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
//
// Pushes a freshly-built HUD image every tick (10Hz) -- a real "smoothly
// crawling" scope trace needs a new column landing that often, not just
// fresh data collected that often. metal_video_set_hud_overlay
// (metal_video_impl_darwin.m) only stores the pixels and marks them
// dirty; the actual on-screen apply happens from displayLinkFired, not a
// dispatch_async, since measurement showed dispatch_async'd blocks onto
// the main queue were landing roughly once every 1.6s in this app
// (see that file's g_hud_dirty doc comment) while displayLinkFired itself
// kept firing at the true display refresh rate the whole time.
func netGraphLoop() {
	ticker := time.NewTicker(netGraphInterval)
	defer ticker.Stop()
	// lastTick diagnoses the "HUD sometimes freezes, sometimes crawls
	// smoothly" report: if THIS gap is also large, the stall is upstream
	// of the native push entirely (Go scheduler/GC pause, or this
	// goroutine blocked on something) -- compare against
	// metal_video_impl_darwin.m's own "HUD apply gap" log, which instead
	// catches a stall between the push call and the actual on-screen
	// apply (e.g. main thread backed up). Remove once the report is
	// resolved.
	var lastTick time.Time
	for range ticker.C {
		now := time.Now()
		if !lastTick.IsZero() {
			if gap := now.Sub(lastTick); gap > 2*netGraphInterval {
				logrus.Warnf("📊 [Net Graph] ticker gap %v (expected ~%v) -- Go-side scheduling delay, not the native compositor", gap, netGraphInterval)
			}
		}
		lastTick = now

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

		img := buildNetGraphHUD(samples)
		// Always cached, regardless of which (if any) push hook is wired --
		// ApplyNetGraphOverlay (Linux/Windows CPU-buffer path) reads this
		// directly and has no push hook of its own to be called from.
		netGraphCachedImg.Store(img)
		if push := netGraphMetalPush; push != nil {
			push(img)
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
		JitterMs:         raw.JitterMs,
		PlayoutDelayMs:   raw.PlayoutDelayMs,
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

// Colors deliberately mimic the old GoldSrc/Source net_graph HUD: a
// barely-there dark wash (just enough to keep green-on-video text
// legible) instead of a solid panel, no border box, bright saturated
// green/yellow/red -- the "readable straight over gameplay" look, not a
// dashboard widget.
var (
	netGraphBg   = color.RGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x38} // faint wash for legibility, not a panel
	netGraphText = color.RGBA{R: 0x9a, G: 0xff, B: 0x6e, A: 0xff} // classic HUD green
	netGraphDim  = color.RGBA{R: 0x6a, G: 0x8a, B: 0x62, A: 0xc0}
	netGraphGood = color.RGBA{R: 0x4a, G: 0xff, B: 0x5a, A: 0xff}
	netGraphWarn = color.RGBA{R: 0xff, G: 0xd8, B: 0x2a, A: 0xff}
	netGraphBad  = color.RGBA{R: 0xff, G: 0x3a, B: 0x2a, A: 0xff}
)

// buildNetGraphHUD draws the current numeric readouts plus scrolling
// history graphs (latency, loss/FEC, decode latency, host latency) onto a
// fixed-size canvas, most-recent sample at the right edge, scrolling left
// -- same convention as the classic net_graph. samples is ordered
// oldest-first; an empty slice still produces a valid (mostly blank)
// canvas.
func buildNetGraphHUD(samples []NetGraphSample) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, netGraphCanvasW, netGraphCanvasH))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: netGraphBg}, image.Point{}, draw.Src)

	var latest NetGraphSample
	if len(samples) > 0 {
		latest = samples[len(samples)-1]
	}

	const marginX = 6
	const col2 = 150
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
		// basicfont.Face7x13 is ASCII-only -- "±" isn't in it and rendered
		// as a garbled/unreadable glyph. "+/-" is the ASCII-safe stand-in.
		rttText = netGraphFmtMs("RTT", latest.RTTMs) + " +/-" + netGraphFmtMs("", latest.RTTVarianceMs)
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
	netGraphDrawText(img, col2, row, netGraphFmtPct("LOSS", lossPct), lossColor)

	row += 12
	jitColor := netGraphGood
	switch {
	case latest.JitterMs >= 20:
		jitColor = netGraphBad
	case latest.JitterMs >= 8:
		jitColor = netGraphWarn
	}
	netGraphDrawText(img, marginX, row, netGraphFmtMs("JIT", latest.JitterMs), jitColor)
	netGraphDrawText(img, col2, row, netGraphFmtMs("BUF", latest.PlayoutDelayMs), netGraphText)

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
	netGraphDrawText(img, col2, row, netGraphFmtMs("DEC", latest.DecodeMs), decColor)

	// Row is always reserved (like RTT above) even when invalid -- making
	// it conditional on HostLatencyValid made graphTop/graphH below jump
	// every time the host stopped/resumed providing this field, visibly
	// resizing the graphs underneath from one frame to the next.
	row += 12
	hostColor := netGraphDim
	hostText := "HOST -- "
	if latest.HostLatencyValid {
		switch {
		case latest.HostLatencyMs >= 20:
			hostColor = netGraphBad
		case latest.HostLatencyMs >= 10:
			hostColor = netGraphWarn
		default:
			hostColor = netGraphGood
		}
		hostText = netGraphFmtMs("HOST", latest.HostLatencyMs)
	}
	netGraphDrawText(img, marginX, row, hostText, hostColor)

	graphTop := row + 6
	graphH := (netGraphCanvasH - graphTop - marginX - 2*4) / 3
	if graphH < 10 {
		graphH = 10
	}
	graphX, graphW := marginX, netGraphCanvasW-2*marginX

	netGraphDrawPointGraph(img, graphX, graphTop, graphW, graphH, samples, func(s NetGraphSample) (float64, bool) {
		if !s.RTTValid {
			return 0, false
		}
		return s.RTTMs, true
	}, 100)
	graphTop += graphH + 4

	netGraphDrawEventGraph(img, graphX, graphTop, graphW, graphH, samples)
	graphTop += graphH + 4

	netGraphDrawPointGraph(img, graphX, graphTop, graphW, graphH, samples, func(s NetGraphSample) (float64, bool) {
		return s.DecodeMs, s.DecodeMs > 0
	}, 66)

	return img
}

// ApplyNetGraphOverlay burns the most recently built HUD canvas
// (netGraphCachedImg) directly into a live decoded RGBA frame, bottom-right
// corner, alpha-composited over the video pixels -- the CPU-buffer
// counterpart to netGraphMetalPush's native compositor layer (macOS/iOS,
// see metal_video_darwin.go/metal_video_ios.go): Linux and Windows already
// run every decoded frame through a CPU-readable RGBA buffer on its way to
// vk_video_try_submit/gl_video_try_submit (see moonlight_cgo_linux.go's
// deliver_frame and moonlight_cgo_windows.go's win_deliver_frame), exactly
// like ai_vision.go's drawCachedOverlay already does for AI Vision on those
// platforms -- so there's no need for a separate compositor layer there.
// Called once per decoded frame; the disabled case (the default) costs one
// atomic load, same philosophy as ApplyAIVisionOverlay.
func ApplyNetGraphOverlay(rgba []byte, w, h, stride int) {
	if !netGraphEnabled.Load() {
		return
	}
	img := netGraphCachedImg.Load()
	if img == nil {
		return
	}
	netGraphBlitOverlay(rgba, w, h, stride, img)
}

// netGraphBlitOverlay alpha-composites img onto dst (a live video frame's
// RGBA buffer), anchored to the bottom-right corner with netGraphHudMargin
// px of breathing room -- the CPU equivalent of the native HUD layer's
// frame-anchoring math on macOS/iOS. img's background wash is deliberately
// semi-transparent (see netGraphBg's doc comment), so this does a real
// per-pixel alpha blend rather than a straight overwrite.
func netGraphBlitOverlay(dst []byte, w, h, stride int, img *image.RGBA) {
	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	x0 := w - netGraphHudMargin - iw
	if x0 < netGraphHudMargin {
		x0 = netGraphHudMargin
	}
	y0 := h - netGraphHudMargin - ih
	if y0 < netGraphHudMargin {
		y0 = netGraphHudMargin
	}
	for y := 0; y < ih; y++ {
		dy := y0 + y
		if dy < 0 || dy >= h {
			continue
		}
		srcRow := img.Pix[y*img.Stride:]
		dstRowOff := dy * stride
		for x := 0; x < iw; x++ {
			dx := x0 + x
			if dx < 0 || dx >= w {
				continue
			}
			so := x * 4
			sa := srcRow[so+3]
			if sa == 0 {
				continue
			}
			do := dstRowOff + dx*4
			if do+3 >= len(dst) {
				continue
			}
			if sa == 255 {
				dst[do+0] = srcRow[so+0]
				dst[do+1] = srcRow[so+1]
				dst[do+2] = srcRow[so+2]
				dst[do+3] = 255
				continue
			}
			a := int(sa)
			inv := 255 - a
			dst[do+0] = byte((int(srcRow[so+0])*a + int(dst[do+0])*inv) / 255)
			dst[do+1] = byte((int(srcRow[so+1])*a + int(dst[do+1])*inv) / 255)
			dst[do+2] = byte((int(srcRow[so+2])*a + int(dst[do+2])*inv) / 255)
			dst[do+3] = 255
		}
	}
}

// netGraphLossPercent counts a packet as "lost" whether or not FEC managed
// to recover it -- FecRecovered means a packet genuinely didn't arrive and
// had to be reconstructed from redundancy, which is real loss by any
// normal definition, just not VISIBLE loss (only FecFailed corrupts what
// actually reaches the screen). Counting only FecFailed here made this
// read as "0% loss" during network hiccups that FEC was successfully
// absorbing, which looked like a bug (loss stuck at 0 while jitter/RTT
// were visibly spiking) rather than FEC quietly doing its job.
func netGraphLossPercent(s NetGraphSample) float64 {
	total := s.PacketsVideo + s.PacketsFec
	if total == 0 {
		return 0
	}
	lost := s.FecRecovered + s.FecFailed + s.PacketsInvalid
	return float64(lost) / float64(total) * 100
}

// netGraphDrawPointGraph plots one scalar per sample as an ISOLATED dot at
// its own height -- not a bar filled from the baseline, not a line
// connecting neighbors. This is the actual CS 1.6/GoldSrc net_graph "ping
// dots" look: packets arrive chaotically at different latencies, and a
// scatter of independent dots reads as exactly that chaos ("видно эфир" --
// you can see the air/radio channel's own jitter), where a filled bar or a
// connected trace visually smooths it into something more orderly than it
// really is. Each dot is colored purely by ITS OWN vertical position within
// the graph -- bottom third green, middle third yellow, top third red --
// rather than by the raw ms value against a fixed threshold, so where a
// dot lands is what determines its color. valueOf returning ok=false (e.g.
// no RTT estimate yet) leaves that column blank.
func netGraphDrawPointGraph(img *image.RGBA, x0, y0, w, h int, samples []NetGraphSample, valueOf func(NetGraphSample) (float64, bool), maxVal float64) {
	const dotSize = 2 // px tall/wide -- a single pixel reads as nearly invisible at this scale
	n := len(samples)
	start := 0
	if n > w {
		start = n - w
	}
	for i := start; i < n; i++ {
		col := x0 + w - (n - i)
		if col < x0 || col >= x0+w {
			continue
		}
		v, ok := valueOf(samples[i])
		if !ok {
			continue
		}
		if v > maxVal {
			v = maxVal
		}
		if v < 0 {
			v = 0
		}
		dy := int(v / maxVal * float64(h-1))
		frac := v / maxVal
		c := netGraphGood
		switch {
		case frac >= 0.66:
			c = netGraphBad
		case frac >= 0.33:
			c = netGraphWarn
		}
		y := y0 + h - 1 - dy
		for py := y; py > y-dotSize && py >= y0; py-- {
			img.SetRGBA(col, py, c)
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
