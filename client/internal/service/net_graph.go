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
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Net Graph is an optional live HUD overlay, off by default: a small
// diagnostics box in the bottom-right corner showing packet
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
	netGraphHistoryLen = 600                    // ring buffer length; also (approx) the graph plot width in px -- see netGraphCanvasW below
	// netGraphCanvasW/H: 2x the original 320x200 (sized for netGraphFace's
	// Go Medium @ 13px) -- the HUD read as too small/cramped to make out at
	// normal viewing distance, so this doubles the canvas, the font size
	// (netGraphFace's init() below), and the margin/dot/bar sizes together.
	// Must stay in sync with vk_video_impl_windows.c's VK_HUD_W/VK_HUD_H
	// (see that file's own comment on why there's no shared constant across
	// the Go/C boundary).
	netGraphCanvasW = 640
	netGraphCanvasH = 400
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
	// stream every server already sends. HostLatencyValid is always true
	// (see GetLastHostLatencyMs's doc comment for why 0 is kept as a real
	// value instead of being reported as invalid) -- kept as a field for
	// parity with the other Valid flags here, in case a future host-side
	// signal for "field genuinely unsupported" becomes available.
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

	// ConcealedFrames: how many motion-extrapolated frames frame smoothing
	// (frame_smoothing.go) presented during this tick -- a per-tick delta
	// off GetFrameSmoothingStats().ConcealedFrames's running total, same
	// diffing convention as PacketsVideo etc. above. 0 on every platform
	// without netGraphConcealedFramesFn wired (nil hook, see below) or
	// while frame smoothing is off/not currently concealing a stall.
	ConcealedFrames uint32

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
	// netGraphConcealedFramesFn returns frame smoothing's running total of
	// synthesized frames (GetFrameSmoothingStats().ConcealedFrames on
	// Windows, see frame_smoothing_windows.go) -- nil on platforms without
	// that feature, same "nil hook = no data yet" contract as the others.
	netGraphConcealedFramesFn func() int64
	netGraphMetalPush         func(img *image.RGBA)
	netGraphMetalClear        func()
	// netGraphScalePush applies the on-screen HUD scale (Vulkan quad /
	// Metal layer frame) without changing the 640x400 canvas -- nil on
	// platforms that only blit via ApplyNetGraphOverlay.
	netGraphScalePush func(scale float32)

	netGraphEnabled  atomic.Bool
	netGraphLoopOnce sync.Once

	// netGraphScalePercent is the on-screen HUD size as a percent of the
	// native 640x400 canvas (50-150, default 100). The Vulkan/Metal texture
	// stays 640x400 -- this only scales the dest quad/layer. See
	// SetNetGraphScale.
	netGraphScalePercent atomic.Uint32
	// netGraphBgAlpha is the HUD wash's A channel (0-255, default
	// netGraphBg.A). Applied on the next 10Hz rebuild.
	netGraphBgAlpha atomic.Uint32

	netGraphPrevMu              sync.Mutex
	netGraphPrevRaw             netGraphRawNetworkStats
	netGraphPrevValid           bool
	netGraphPrevConcealedFrames int64

	netGraphMu      sync.Mutex
	netGraphSamples []NetGraphSample
	// netGraphDrawMu serializes buildNetGraphHUD: opentype.Face is not
	// safe for concurrent Glyph/LoadGlyph (see netGraphFace's init
	// comment). The 10Hz loop is the normal caller; the UI slider used
	// to rebuild on the Fyne thread and raced it -- index-out-of-range
	// inside sfnt.LoadGlyph.
	netGraphDrawMu sync.Mutex

	// netGraphCachedImg holds the most recently built HUD canvas for
	// ApplyNetGraphOverlay below -- the CPU-buffer blit path Linux/Windows
	// use instead of a native compositor layer (see that function's doc
	// comment). Updated every tick alongside the netGraphMetalPush call,
	// nil when disabled.
	netGraphCachedImg atomic.Pointer[image.RGBA]
)

// netGraphHudMargin is the gap, in pixels, between the HUD box and the
// bottom/right edges of the frame -- used by the CPU-buffer compositing
// path (netGraphBlitOverlay below, Linux/Windows) and by
// vk_video_impl_windows.c's VK_HUD_MARGIN (native Vulkan overlay layer,
// kept in sync with this by hand -- see that file's own comment). Bumped to
// 24 alongside the 2x HUD canvas (netGraphCanvasW/H's doc comment) so the
// larger box keeps proportionally the same breathing room from the corner.
// macOS/iOS's own native HUD_MARGIN (metal_video_impl_darwin.m/
// metal_video_impl_ios.m) is a separate, independently-anchored constant,
// not wired to this one -- left at its original 12 for now, a purely
// cosmetic (not size-critical) difference until that path gets the same
// treatment.
const netGraphHudMargin = 24

// SetNetGraphEnabled turns the HUD on or off. The video settings dialog
// applies this from Apply/Start; the Control footer toggle still applies
// immediately. Disabling clears the cached samples and the on-screen HUD
// image right away so a stale readout never lingers after the setting is
// turned off.
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
		netGraphPrevConcealedFrames = 0
		netGraphPrevMu.Unlock()
		// A new Vulkan/Metal session starts with C-side scale = 100%;
		// push the Go value so a reconnect keeps the operator's size.
		SyncNetGraphNativeScale()
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

const (
	netGraphScalePercentMin     = 50
	netGraphScalePercentMax     = 150
	netGraphScalePercentDefault = 100
)

// SetNetGraphScale sets the on-screen HUD size as a percent of the native
// 640x400 canvas (50-150). Takes effect immediately on the Vulkan/Metal
// dest quad; the CPU blit path picks it up on the next decoded frame.
func SetNetGraphScale(percent int) {
	if percent < netGraphScalePercentMin {
		percent = netGraphScalePercentMin
	}
	if percent > netGraphScalePercentMax {
		percent = netGraphScalePercentMax
	}
	netGraphScalePercent.Store(uint32(percent))
	if push := netGraphScalePush; push != nil {
		push(float32(percent) / 100)
	}
}

// NetGraphScalePercent is the current on-screen HUD size (50-150).
func NetGraphScalePercent() int {
	p := int(netGraphScalePercent.Load())
	if p == 0 {
		return netGraphScalePercentDefault
	}
	return p
}

// SetNetGraphBgAlpha sets the HUD wash opacity (0 = invisible panel, 255 =
// solid black). The next 10Hz rebuild picks it up; graphs/text stay opaque.
func SetNetGraphBgAlpha(a uint8) {
	netGraphBgAlpha.Store(uint32(a))
}

// NetGraphBgAlpha is the current HUD wash opacity (0-255).
func NetGraphBgAlpha() uint8 {
	return uint8(netGraphBgAlpha.Load())
}

// SyncNetGraphNativeScale pushes the current HUD size to the Vulkan/Metal
// dest quad. Call after a new video session is up: C-side scale is 100%
// until the first SetNetGraphScale of that session.
func SyncNetGraphNativeScale() {
	if push := netGraphScalePush; push != nil {
		push(float32(NetGraphScalePercent()) / 100)
	}
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

	var concealedTotal int64
	if fn := netGraphConcealedFramesFn; fn != nil {
		concealedTotal = fn()
	}

	netGraphPrevMu.Lock()
	prev := netGraphPrevRaw
	hadPrev := netGraphPrevValid
	prevConcealed := netGraphPrevConcealedFrames
	netGraphPrevRaw = raw
	netGraphPrevValid = true
	netGraphPrevConcealedFrames = concealedTotal
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
		if concealedTotal > prevConcealed {
			s.ConcealedFrames = uint32(concealedTotal - prevConcealed)
		}
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

// A barely-there dark wash (just enough to keep green-on-video text
// legible) instead of a solid panel, no border box, bright saturated
// green/yellow/red -- readable straight over the live picture, not a
// dashboard widget.
var (
	netGraphBg   = color.RGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x38} // faint wash for legibility, not a panel
	netGraphText = color.RGBA{R: 0x9a, G: 0xff, B: 0x6e, A: 0xff} // classic HUD green
	netGraphDim  = color.RGBA{R: 0x6a, G: 0x8a, B: 0x62, A: 0xc0}
	netGraphGood = color.RGBA{R: 0x4a, G: 0xff, B: 0x5a, A: 0xff}
	netGraphWarn = color.RGBA{R: 0xff, G: 0xd8, B: 0x2a, A: 0xff}
	netGraphBad  = color.RGBA{R: 0xff, G: 0x3a, B: 0x2a, A: 0xff}
	// netGraphPurple marks a concealed (motion-extrapolated) frame -- see
	// netGraphDrawConcealedMarkers -- distinct from the good/warn/bad
	// traffic-light palette above since it's not a severity, just "this
	// tick, frame smoothing painted over a network stall".
	netGraphPurple = color.RGBA{R: 0xc8, G: 0x5a, B: 0xff, A: 0xff}
)

// buildNetGraphHUD draws the current numeric readouts plus scrolling
// history graphs (latency, loss/FEC, decode latency, host latency) onto a
// fixed-size canvas, most-recent sample at the right edge, scrolling left
// -- same convention as the classic net_graph. samples is ordered
// oldest-first; an empty slice still produces a valid (mostly blank)
// canvas.
func buildNetGraphHUD(samples []NetGraphSample) *image.RGBA {
	netGraphDrawMu.Lock()
	defer netGraphDrawMu.Unlock()

	img := image.NewRGBA(image.Rect(0, 0, netGraphCanvasW, netGraphCanvasH))
	bg := netGraphBg
	bg.A = NetGraphBgAlpha()
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	var latest NetGraphSample
	if len(samples) > 0 {
		latest = samples[len(samples)-1]
	}

	const marginX = 12
	const col2 = 350
	row := netGraphLineH
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

	row += netGraphLineH
	jitColor := netGraphGood
	switch {
	case latest.JitterMs >= 20:
		jitColor = netGraphBad
	case latest.JitterMs >= 8:
		jitColor = netGraphWarn
	}
	netGraphDrawText(img, marginX, row, netGraphFmtMs("JIT", latest.JitterMs), jitColor)
	netGraphDrawText(img, col2, row, netGraphFmtMs("BUF", latest.PlayoutDelayMs), netGraphText)

	row += netGraphLineH
	fecColor := netGraphGood
	if latest.FecFailed > 0 {
		fecColor = netGraphBad
	} else if latest.FecRecovered > 0 {
		fecColor = netGraphWarn
	}
	// recv/rec/fail: total FEC packets received this tick, how many lost
	// video packets they recovered, and how many couldn't be recovered.
	// Recovered/failed sitting at 0/0 on a clean connection is correct (FEC
	// only does anything when a packet actually goes missing) -- recv being
	// nonzero is what shows the FEC stream itself is flowing rather than
	// this line simply being wired to nothing.
	netGraphDrawText(img, marginX, row, netGraphFmtCounts3("FEC recv/rec/fail", latest.PacketsFec, latest.FecRecovered, latest.FecFailed), fecColor)

	row += netGraphLineH
	decColor := netGraphGood
	switch {
	case latest.DecodeMs >= 33:
		decColor = netGraphBad
	case latest.DecodeMs >= 16:
		decColor = netGraphWarn
	}
	netGraphDrawText(img, marginX, row, netGraphFmtFPS("FPS", latest.RenderFPS), netGraphText)
	netGraphDrawText(img, col2, row, netGraphFmtMs("DEC", latest.DecodeMs), decColor)

	// Row is always reserved (like RTT above) -- making it conditional made
	// graphTop/graphH below jump every time the host's reported latency hit
	// exactly 0 (a normal occurrence: an unchanged picture, nothing encoded),
	// visibly resizing the graphs underneath from one frame to the next. 0
	// is a genuine measurement (see GetLastHostLatencyMs's doc comment), so
	// it's shown as "HOST 0ms", not hidden behind a placeholder.
	row += netGraphLineH
	hostColor := netGraphGood
	switch {
	case latest.HostLatencyMs >= 20:
		hostColor = netGraphBad
	case latest.HostLatencyMs >= 10:
		hostColor = netGraphWarn
	}
	netGraphDrawText(img, marginX, row, netGraphFmtMs("HOST", latest.HostLatencyMs), hostColor)

	graphTop := row + 12
	graphH := (netGraphCanvasH - graphTop - marginX - 2*8) / 3
	if graphH < 20 {
		graphH = 20
	}
	graphX, graphW := marginX, netGraphCanvasW-2*marginX

	netGraphDrawPointGraph(img, graphX, graphTop, graphW, graphH, samples, func(s NetGraphSample) (float64, bool) {
		if !s.RTTValid {
			return 0, false
		}
		return s.RTTMs, true
	}, 100)
	netGraphDrawConcealedMarkers(img, graphX, graphTop, graphW, samples)
	graphTop += graphH + 8

	netGraphDrawEventGraph(img, graphX, graphTop, graphW, graphH, samples)
	graphTop += graphH + 8

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
	// Android uses the same blit, but only while the HUD is on: its default
	// path is AHardwareBuffer zero-copy (no CPU pixels), so
	// moonlight_cgo_android.go's dr_submit falls back to glReadPixels +
	// android_vk_try_submit for as long as the checkbox is ticked.
// Called once per decoded frame; the disabled case (the default) costs one
// atomic load, same philosophy as ApplyAIVisionOverlay.
// bgr: true when dst's byte order is BGRA rather than RGBA -- Windows's GDI
// fallback path (moonlight_cgo_windows.go's win_deliver_frame, when neither
// Vulkan nor the D3D11VA->RGBA path is in play) hands sws_scale output in
// BGRA to match BI_RGB's 32-bit DIB layout (see gl_video_impl_windows.c).
// img (the cached HUD canvas) is always a standard Go image.RGBA, so R/B
// need swapping on the way into a BGRA dst or the HUD's greens/reds would
// come out wrong -- this used to be sidestepped by skipping the overlay
// entirely on that path, which meant the HUD just never appeared there.
func ApplyNetGraphOverlay(rgba []byte, w, h, stride int, bgr bool) {
	if !netGraphEnabled.Load() {
		return
	}
	img := netGraphCachedImg.Load()
	if img == nil {
		return
	}
	netGraphBlitOverlay(rgba, w, h, stride, img, bgr)
}

// netGraphBlitOverlay alpha-composites img onto dst (a live video frame's
// RGBA or BGRA buffer -- see bgr), anchored to the bottom-right corner with
// netGraphHudMargin px of breathing room -- the CPU equivalent of the native
// HUD layer's frame-anchoring math on macOS/iOS. img's background wash is
// deliberately semi-transparent (see netGraphBg's doc comment), so this does
// a real per-pixel alpha blend rather than a straight overwrite.
func netGraphBlitOverlay(dst []byte, w, h, stride int, img *image.RGBA, bgr bool) {
	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	scalePct := NetGraphScalePercent()
	dw := iw * scalePct / 100
	dh := ih * scalePct / 100
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	x0 := w - netGraphHudMargin - dw
	if x0 < netGraphHudMargin {
		x0 = netGraphHudMargin
	}
	y0 := h - netGraphHudMargin - dh
	if y0 < netGraphHudMargin {
		y0 = netGraphHudMargin
	}
	rIdx, bIdx := 0, 2
	if bgr {
		rIdx, bIdx = 2, 0
	}
	for y := 0; y < dh; y++ {
		dy := y0 + y
		if dy < 0 || dy >= h {
			continue
		}
		sy := y * ih / dh
		srcRow := img.Pix[sy*img.Stride:]
		dstRowOff := dy * stride
		for x := 0; x < dw; x++ {
			dx := x0 + x
			if dx < 0 || dx >= w {
				continue
			}
			so := (x * iw / dw) * 4
			if so+3 >= len(srcRow) {
				continue
			}
			sa := srcRow[so+3]
			if sa == 0 {
				continue
			}
			do := dstRowOff + dx*4
			if do+3 >= len(dst) {
				continue
			}
			if sa == 255 {
				dst[do+rIdx] = srcRow[so+0]
				dst[do+1] = srcRow[so+1]
				dst[do+bIdx] = srcRow[so+2]
				dst[do+3] = 255
				continue
			}
			a := int(sa)
			inv := 255 - a
			dst[do+rIdx] = byte((int(srcRow[so+0])*a + int(dst[do+rIdx])*inv) / 255)
			dst[do+1] = byte((int(srcRow[so+1])*a + int(dst[do+1])*inv) / 255)
			dst[do+bIdx] = byte((int(srcRow[so+2])*a + int(dst[do+bIdx])*inv) / 255)
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
// connecting neighbors. Packets arrive chaotically at different latencies,
// and a scatter of independent dots reads as exactly that chaos (you can
// see the connection's own jitter), where a filled bar or a connected trace
// visually smooths it into something more orderly than it really is. Each
// dot is colored purely by ITS OWN vertical position within
// the graph -- bottom third green, middle third yellow, top third red --
// rather than by the raw ms value against a fixed threshold, so where a
// dot lands is what determines its color. valueOf returning ok=false (e.g.
// no RTT estimate yet) leaves that column blank.
func netGraphDrawPointGraph(img *image.RGBA, x0, y0, w, h int, samples []NetGraphSample, valueOf func(NetGraphSample) (float64, bool), maxVal float64) {
	const dotSize = 4 // px tall/wide -- a single pixel reads as nearly invisible at this scale
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

// netGraphDrawConcealedMarkers overlays a small purple dot at the TOP edge
// of the RTT point graph for every column whose tick had frame smoothing
// (frame_smoothing.go) present at least one motion-extrapolated frame --
// its own marker lane, deliberately separate from the RTT dot itself
// (netGraphDrawPointGraph draws that at the column's OWN height, which
// would otherwise collide with or hide this) so "a stall got concealed
// here" stays visible regardless of what RTT was doing at the same moment.
// Same column math as netGraphDrawPointGraph/netGraphDrawEventGraph
// (rightmost column = most recent sample) so all three stay aligned.
func netGraphDrawConcealedMarkers(img *image.RGBA, x0, y0, w int, samples []NetGraphSample) {
	const dotSize = 6 // taller than the RTT dots' own 4px so the marker lane stands out on its own
	n := len(samples)
	start := 0
	if n > w {
		start = n - w
	}
	for i := start; i < n; i++ {
		if samples[i].ConcealedFrames == 0 {
			continue
		}
		col := x0 + w - (n - i)
		if col < x0 || col >= x0+w {
			continue
		}
		for dy := 0; dy < dotSize; dy++ {
			img.SetRGBA(col, y0+dy, netGraphPurple)
		}
	}
}

// netGraphDrawEventGraph plots FEC-recovered (yellow) and FEC-failed/loss
// (red) counts per tick, stacked from the baseline up. Any non-zero count
// gets at least a few visible pixels regardless of magnitude -- the whole
// point is that a single lost or recovered packet must never be invisible
// just because it's a "small" number next to a 10Hz sample rate.
func netGraphDrawEventGraph(img *image.RGBA, x0, y0, w, h int, samples []NetGraphSample) {
	const minBar = 6
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

// netGraphFace is the HUD's font: Go Medium, a real proportional, hinted,
// anti-aliased TrueType face built into golang.org/x/image (no extra asset
// to ship or embed) -- swapped in for basicfont.Face7x13, whose fixed 7x13
// bitmap grid read as cramped, blocky and thin. Go Bold was tried
// first, but at this pixel size its strokes plus netGraphDrawText's 1px
// outline (which -- since font.Face's advance width has no idea an outline
// is coming -- always eats a couple pixels of the gap between glyphs that
// the face itself budgeted) left adjacent bold letters visibly touching;
// Medium weight plus a size bump (11 -> 13 -> 26, alongside the 2x canvas
// bump -- see netGraphCanvasW/H's doc comment) gives the outline that room
// back without going back to a washed-out Regular weight. Loaded once here;
// opentype.Face is documented as not safe for concurrent use -- every
// Glyph/LoadGlyph goes through buildNetGraphHUD, which holds
// netGraphDrawMu for the whole paint.
var (
	netGraphFace   font.Face
	netGraphGlyphH float64 = 16 // overwritten in init() from the real face metrics; this fallback only matters if font loading somehow fails
	// netGraphLineH is the row-to-row pixel spacing buildNetGraphHUD uses
	// between stat lines -- the face's own recommended baseline-to-baseline
	// distance, not a value hand-tuned for basicfont.Face7x13's fixed grid.
	netGraphLineH int = 15
)

func init() {
	netGraphScalePercent.Store(netGraphScalePercentDefault)
	netGraphBgAlpha.Store(uint32(netGraphBg.A))

	f, err := opentype.Parse(gomedium.TTF)
	if err != nil {
		logrus.Errorf("📊 [Net Graph] failed to parse embedded HUD font, falling back to a blank face: %v", err)
		return
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    26,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		logrus.Errorf("📊 [Net Graph] failed to rasterize embedded HUD font, falling back to a blank face: %v", err)
		return
	}
	netGraphFace = face
	m := face.Metrics()
	netGraphGlyphH = float64((m.Ascent + m.Descent).Round())
	if h := m.Height.Round(); h > 0 {
		netGraphLineH = h
	}
}

// netGraphDrawText renders dark-outlined, top-lit-gradient HUD text: an
// 8-direction 1px black halo (keeps it legible over ANY background, light
// or dark, the way a real HUD needs to be -- netGraphFace's own bold weight
// alone isn't enough over bright video) plus a vertical gradient from a
// lightened tint of c to c itself for a bit of depth/shine, the same idea
// classic HUD/subtitle text goes for.
func netGraphDrawText(img *image.RGBA, x, baselineY int, text string, c color.Color) {
	outline := color.RGBA{0, 0, 0, 235}
	for _, off := range netGraphTextOutlineOffsets {
		netGraphDrawGlyphs(img, x+off[0], baselineY+off[1], text, &image.Uniform{C: outline})
	}

	rc := color.RGBAModel.Convert(c).(color.RGBA)
	grad := netGraphVGradient{top: netGraphLighten(rc, 0.55), bottom: rc}
	netGraphDrawGlyphs(img, x, baselineY, text, grad)
}

// netGraphTextOutlineOffsets: the 8 neighbors of a pixel, used to stamp a 1px
// halo around each glyph.
var netGraphTextOutlineOffsets = [8][2]int{
	{-1, -1}, {0, -1}, {1, -1},
	{-1, 0} /*      */, {1, 0},
	{-1, 1}, {0, 1}, {1, 1},
}

func netGraphDrawGlyphs(img *image.RGBA, x, baselineY int, text string, src image.Image) {
	if netGraphFace == nil {
		return
	}
	d := &font.Drawer{
		Dst:  img,
		Src:  src,
		Face: netGraphFace,
		Dot:  fixed.P(x, baselineY),
	}
	d.DrawString(text)
}

// netGraphLighten blends c towards white by amt (0 = c unchanged, 1 = white)
// -- used for the gradient fill's top color.
func netGraphLighten(c color.RGBA, amt float64) color.RGBA {
	lerp := func(v uint8) uint8 {
		f := float64(v) + (255.0-float64(v))*amt
		if f > 255 {
			f = 255
		}
		return uint8(f)
	}
	return color.RGBA{R: lerp(c.R), G: lerp(c.G), B: lerp(c.B), A: c.A}
}

// netGraphVGradient is a virtual, effectively-infinite image whose color
// depends only on y (not x), interpolating linearly from top to bottom
// across one glyph cell's height. font.Drawer's Face.Glyph/draw.DrawMask
// always samples a text source starting at its own (0,0), with the *glyph's*
// bounding box top-left mapped there -- so At(x, y) here receives y already
// relative to each individual glyph's own top edge, meaning this produces
// the same top-to-bottom gradient inside every glyph on the line, regardless
// of that glyph's actual x position or which line of the HUD it's part of.
type netGraphVGradient struct {
	top, bottom color.RGBA
}

func (g netGraphVGradient) ColorModel() color.Model { return color.RGBAModel }
func (g netGraphVGradient) Bounds() image.Rectangle {
	return image.Rect(-1<<20, -1<<20, 1<<20, 1<<20)
}
func (g netGraphVGradient) At(_, y int) color.Color {
	t := float64(y) / netGraphGlyphH
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	lerp := func(a, b uint8) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t) }
	return color.RGBA{R: lerp(g.top.R, g.bottom.R), G: lerp(g.top.G, g.bottom.G), B: lerp(g.top.B, g.bottom.B), A: 255}
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

func netGraphFmtCounts3(label string, a, b, c uint32) string {
	return label + " " + netGraphFmtUint(a) + "/" + netGraphFmtUint(b) + "/" + netGraphFmtUint(c)
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
