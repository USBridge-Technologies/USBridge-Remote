//go:build js && wasm

package service

import (
	"image"
	"math"
	"strconv"
	"sync"
	"syscall/js"
	"time"

	"usbridge-client/internal/webrtcweb"
)

func init() {
	netGraphNetworkStatsFn = netGraphWasmNetworkStats
	netGraphRenderFPS = netGraphWasmRenderFPS
	netGraphDecodeMs = netGraphWasmDecodeMs
	netGraphMetalPush = pushNetGraphWasmOverlay
	netGraphMetalClear = clearNetGraphWasmOverlay
}

// The HUD ticks at 10Hz (netGraphInterval) but getStats() is only polled at
// 4Hz (webrtcweb's netGraphStatsPollInterval), so most ticks see the same
// snapshot as the tick before. Diffing "since my last call" then read 0 on
// those ticks and a burst on the next -- every counter on the WebRTC HUD
// jumped between its real value and zero. Everything below works off the
// last two *distinct* snapshots instead.
var (
	netGraphWasmMu   sync.Mutex
	netGraphWasmPrev webrtcweb.NetGraphSnapshot
	netGraphWasmCur  webrtcweb.NetGraphSnapshot
)

// netGraphWasmPair returns the two most recent distinct snapshots
// (prev.At < cur.At), ok=false until two have arrived.
func netGraphWasmPair() (prev, cur webrtcweb.NetGraphSnapshot, ok bool) {
	snap, have := webrtcweb.LatestNetGraphSnapshot()
	netGraphWasmMu.Lock()
	defer netGraphWasmMu.Unlock()
	if !have || !snap.Valid {
		netGraphWasmPrev, netGraphWasmCur = webrtcweb.NetGraphSnapshot{}, webrtcweb.NetGraphSnapshot{}
		return prev, cur, false
	}
	if snap.At.After(netGraphWasmCur.At) {
		netGraphWasmPrev, netGraphWasmCur = netGraphWasmCur, snap
	}
	prev, cur = netGraphWasmPrev, netGraphWasmCur
	return prev, cur, prev.Valid && cur.At.After(prev.At)
}

// netGraphWasmLerpU32 interpolates a cumulative counter between two
// snapshots. Clamped at cur (never ahead of what was actually measured),
// and restarting from cur when the next snapshot arrives keeps it
// monotonic.
func netGraphWasmLerpU32(prev, cur uint32, frac float64) uint32 {
	if cur <= prev {
		return cur
	}
	return prev + uint32(float64(cur-prev)*frac)
}

// netGraphWasmNetworkStats adapts webrtcweb's cumulative getStats()
// snapshots into net_graph.go's own cumulative-counter shape --
// collectNetGraphSample diffs it into per-tick deltas exactly like it
// already does for moonlight-common-c's RTPVideoStats on every other
// platform. The counters are replayed one poll interval late, linearly
// interpolated from the previous snapshot to the latest, so each 100ms
// tick gets its share of packets instead of 0-0-burst.
//
// WebRTC has no FEC concept of its own (loss recovery happens
// transparently via NACK/RTX inside the RTCPeerConnection, never surfaced
// as separate "recovered" vs "failed" counters) -- PacketCountFecFailed
// carries packetsLost instead, so the HUD's loss% and FEC-row color
// severity still reflect real loss, just without the recovered/failed split
// moonlight's own FEC gives on other platforms. Called from net_graph.go's
// 100ms HUD tick, so this must never block -- it only reads an
// atomically-stored snapshot, the actual getStats() promise await happens
// on StartNetGraphStatsPolling's own goroutine.
func netGraphWasmNetworkStats() netGraphRawNetworkStats {
	prev, cur, ok := netGraphWasmPair()
	if !ok {
		return netGraphRawNetworkStats{}
	}
	frac := float64(time.Since(cur.At)) / float64(cur.At.Sub(prev.At))
	frac = math.Max(0, math.Min(1, frac))
	return netGraphRawNetworkStats{
		PacketCountVideo:     netGraphWasmLerpU32(prev.PacketsReceived, cur.PacketsReceived, frac),
		PacketCountFecFailed: netGraphWasmLerpU32(prev.PacketsLost, cur.PacketsLost, frac),
		JitterMs:             cur.JitterMs,
		RTTMs:                cur.RTTMs,
		RTTValid:             cur.RTTValid,
	}
}

// netGraphWasmRenderFPS derives a render-fps proxy from framesDecoded's
// growth between the last two snapshots -- the DOM `<video>` overlay path
// (video_widget_dom_overlay_wasm.go) never hands decoded frames through Go,
// so framesDecoded (from getStats(), not a local frame counter) is the only
// signal available here, same as every count this file reports.
func netGraphWasmRenderFPS() float64 {
	prev, cur, ok := netGraphWasmPair()
	if !ok || cur.FramesDecoded < prev.FramesDecoded {
		return 0
	}
	return float64(cur.FramesDecoded-prev.FramesDecoded) / cur.At.Sub(prev.At).Seconds()
}

// netGraphWasmDecodeMs derives average per-frame decode time from
// totalDecodeTime's growth (a standard RTCInboundRtpStreamStats field, the
// browser's own accumulated decode-time counter) divided by how many frames
// decoded between the last two snapshots -- the closest web equivalent to
// the native platforms' own per-frame decode timer.
func netGraphWasmDecodeMs() float64 {
	prev, cur, ok := netGraphWasmPair()
	if !ok || cur.FramesDecoded <= prev.FramesDecoded {
		return 0
	}
	dt := cur.TotalDecodeTimeMs - prev.TotalDecodeTimeMs
	if dt < 0 {
		return 0
	}
	return dt / float64(cur.FramesDecoded-prev.FramesDecoded)
}

// ─────────────────────────────────────────────────────────────────────────
// DOM overlay push -- the wasm counterpart of metal_video_darwin.go's
// netGraphMetalPush. There's no CPU-readable decoded frame to blit into
// here (the DOM `<video>` overlay path never hands pixels through Go, see
// video_widget_dom_overlay_wasm.go's own doc comment) and no native
// Vulkan/Metal compositor layer either -- so instead this draws the built
// HUD canvas (netGraphCanvasW x netGraphCanvasH, see net_graph.go) into a
// plain <canvas> element, positioned as its own fixed-position DOM overlay
// in the corner, the same general trick the <video> element itself uses to
// sit above Fyne's own wasm canvas (client_wasm.go's videoEl style
// comment). Anchored to the browser viewport's own corner rather than the
// video widget's on-screen rect (unlike the <video> overlay) -- simpler,
// and a diagnostics HUD doesn't need to track pan/zoom/letterboxing the way
// the actual video picture does.
var (
	netGraphDOMMu     sync.Mutex
	netGraphDOMCanvas js.Value
	netGraphDOMCtx    js.Value
)

const (
	netGraphDOMMarginPx = 16
	// netGraphDOMZIndex sits just above the WebRTC <video> overlay's own
	// z-index 5 (client_wasm.go) so the HUD is always visible over the
	// picture, and below the touch overlay (10)/cursor dot (11) which need
	// to stay interactive above everything else -- same numbering scheme
	// that comment documents.
	netGraphDOMZIndex = "6"
	// netGraphDOMCSSScale halves the 640x400 HUD canvas for on-screen
	// display (drawn at 2x internally for crisp text, see net_graph.go's
	// netGraphCanvasW/H doc comment) -- NetGraphScalePercent() (50-150)
	// scales further from this 320x200 baseline.
	netGraphDOMCSSScale = 0.5
)

func ensureNetGraphDOMCanvas() {
	if !netGraphDOMCanvas.IsUndefined() && !netGraphDOMCanvas.IsNull() {
		return
	}
	doc := js.Global().Get("document")
	canvasEl := doc.Call("createElement", "canvas")
	canvasEl.Set("id", "usbridge-netgraph-overlay")
	canvasEl.Set("width", netGraphCanvasW)
	canvasEl.Set("height", netGraphCanvasH)
	style := canvasEl.Get("style")
	style.Set("position", "fixed")
	style.Set("right", pxInt(netGraphDOMMarginPx))
	style.Set("bottom", pxInt(netGraphDOMMarginPx))
	style.Set("zIndex", netGraphDOMZIndex)
	style.Set("pointerEvents", "none")
	style.Set("visibility", "hidden")
	doc.Get("body").Call("appendChild", canvasEl)
	netGraphDOMCanvas = canvasEl
	netGraphDOMCtx = canvasEl.Call("getContext", "2d")
}

// pushNetGraphWasmOverlay is netGraphMetalPush's wasm implementation --
// called once per 100ms HUD tick (net_graph.go's netGraphLoop) with a
// freshly built image.RGBA.
func pushNetGraphWasmOverlay(img *image.RGBA) {
	netGraphDOMMu.Lock()
	defer netGraphDOMMu.Unlock()

	// Gate on the actual WebRTC session, not just the operator's checkbox
	// (netGraphEnabled, which this function's caller already gated on):
	// netGraphWasmNetworkStats/RenderFPS/DecodeMs all replay the last snapshot
	// netGraphWasmPair() ever saw once the session's stats poller stops, so
	// without this the HUD kept pushing (and showing) frozen numbers forever
	// after disconnect instead of disappearing along with the stream, unlike
	// Darwin's Metal HUD layer which vanishes for free when MetalVideoDestroy
	// tears down the whole native surface it's drawn on.
	if !webrtcVideoSessionActive() {
		if !netGraphDOMCanvas.IsUndefined() && !netGraphDOMCanvas.IsNull() {
			netGraphDOMCanvas.Get("style").Set("visibility", "hidden")
		}
		return
	}
	ensureNetGraphDOMCanvas()

	w, h := img.Rect.Dx(), img.Rect.Dy()
	n := len(img.Pix)
	buf := js.Global().Get("Uint8Array").New(n)
	js.CopyBytesToJS(buf, img.Pix)
	// ImageData wants a Uint8ClampedArray view -- constructed over the same
	// underlying ArrayBuffer CopyBytesToJS just filled, no extra copy.
	clamped := js.Global().Get("Uint8ClampedArray").New(buf.Get("buffer"))
	imgData := js.Global().Get("ImageData").New(clamped, w, h)
	netGraphDOMCtx.Call("putImageData", imgData, 0, 0)

	scale := netGraphDOMCSSScale * float64(NetGraphScalePercent()) / 100
	style := netGraphDOMCanvas.Get("style")
	style.Set("width", pxFloat(float64(w)*scale))
	style.Set("height", pxFloat(float64(h)*scale))
	style.Set("visibility", "visible")
}

// clearNetGraphWasmOverlay is netGraphMetalClear's wasm implementation --
// called once when the checkbox is unticked.
func clearNetGraphWasmOverlay() {
	netGraphDOMMu.Lock()
	defer netGraphDOMMu.Unlock()
	if netGraphDOMCanvas.IsUndefined() || netGraphDOMCanvas.IsNull() {
		return
	}
	netGraphDOMCanvas.Get("style").Set("visibility", "hidden")
}

func pxInt(v int) string {
	return strconv.Itoa(v) + "px"
}

func pxFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64) + "px"
}
