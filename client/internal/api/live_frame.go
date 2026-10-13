package api

import (
	"image"
	"sync/atomic"
	"time"
)

// Live-frame bridge for local ui.parse (see tryLocalUIParse in
// local_ui_intercept.go): lets the video decode path
// (internal/service/ai_vision.go, which already imports this package for
// GetLocalUIParser/SetLocalUIParser) hand over its next decoded frame on
// demand, so a local ui.parse call made while a video session is already
// streaming doesn't ALSO pay for a device screenshot round-trip
// (screen.get_image over /api/mcp, plus whatever capture cost the device
// side pays for it) when a frame is already sitting locally, decoded,
// waiting to be displayed.
//
// This lives in package api, not service, specifically to avoid an import
// cycle: service already imports api (for GetLocalUIParser), so the
// dependency has to run this direction.
//
// Deliberately independent of the AI Vision overlay checkbox
// (service.SetAIVisionEnabled) -- that toggle gates a periodic (every
// aiVisionInterval) background detection pass burned into the live
// picture, a completely different concern from "give me whatever frame
// you're decoding right now, once". The video decode hot path (see
// ai_vision.go's ApplyAIVisionOverlay/goAIVisionSample) checks
// LiveFrameWanted on every frame regardless of that checkbox, at the cost
// of one atomic load when nobody's asking.
var (
	liveFrameWanted atomic.Bool
	liveFrameCh     = make(chan []byte, 1)

	// liveFrameGetter, when set (service package's init on platforms that
	// keep a stable "last rendered" buffer -- currently macOS's Metal path,
	// see metal_video_darwin.go's g_lastRenderedBuf), answers RequestLiveFrame
	// immediately from that already-composited frame instead of arming
	// liveFrameWanted and waiting for the decode thread's next callback.
	// Confirmed live: the wait-based path could catch a frame mid-reconnect
	// glitch (a visibly smeared/torn frame the operator never actually saw
	// as a stable picture) simply because it happened to be whatever the
	// decoder produced in the timeout window right after being asked --
	// asking "what's already on screen" instead of "decode me a fresh one"
	// avoids that class of bug entirely, not just the timing race the
	// timeout value alone fixed. nil on platforms without such an accessor,
	// where the wait-based fallback below still applies.
	liveFrameGetter atomic.Pointer[func() []byte]
)

// SetLiveFrameGetter registers a getter that returns the current frame
// (PNG-encoded) instantly, no waiting -- see liveFrameGetter's doc comment.
// Pass nil to clear it back to the wait-based fallback.
func SetLiveFrameGetter(fn func() []byte) {
	if fn == nil {
		liveFrameGetter.Store(nil)
		return
	}
	liveFrameGetter.Store(&fn)
}

// RequestLiveFrame asks the video decode path for a frame (PNG-encoded,
// full resolution). Prefers liveFrameGetter's instant "already rendered"
// snapshot when one is registered; otherwise waits up to timeout for the
// decode thread's next callback to hand one over via SubmitLiveFrame.
// Returns (nil, false) if no video session is actively decoding frames
// right now -- the caller should fall back to fetching a screenshot from
// the device in that case, exactly as if this didn't exist.
func RequestLiveFrame(timeout time.Duration) ([]byte, bool) {
	if p := liveFrameGetter.Load(); p != nil {
		if png := (*p)(); png != nil {
			return png, true
		}
		return nil, false
	}

	// Drain a stale frame left over from a previous call that timed out
	// before anything arrived, so this call doesn't get handed a frame
	// from seconds ago instead of a fresh one.
	select {
	case <-liveFrameCh:
	default:
	}

	liveFrameWanted.Store(true)
	defer liveFrameWanted.Store(false)

	select {
	case png := <-liveFrameCh:
		return png, true
	case <-time.After(timeout):
		return nil, false
	}
}

// LiveFrameWanted reports whether a RequestLiveFrame call is currently
// blocked waiting for a frame -- checked by the decode path's hot-path
// callback (internal/service/ai_vision.go) so it knows whether to bother
// capturing+encoding this frame, without service needing to import api (it
// already does) or api needing to import service (which would cycle).
func LiveFrameWanted() bool {
	return liveFrameWanted.Load()
}

// SubmitLiveFrame delivers one PNG-encoded frame to a pending
// RequestLiveFrame call. Non-blocking and a no-op if nobody's currently
// waiting (LiveFrameWanted was false, or another frame already claimed
// this request) -- the decode thread must never stall on this.
func SubmitLiveFrame(png []byte) {
	if !liveFrameWanted.CompareAndSwap(true, false) {
		return
	}
	select {
	case liveFrameCh <- png:
	default:
	}
}

// LumaStride is the sampling stride of a LumaGrid: every LumaStride-th pixel
// in x and y, matching screenChangePercent's own sampling so a grid-vs-grid
// diff reads identically to the PNG-vs-PNG diff it replaces.
const LumaStride = 4

// LumaGrid is a cheap, PNG-free fingerprint of one decoded frame: the
// luma of every LumaStride-th pixel. Click confirmation only needs "did
// the picture change", and encoding+decoding two full-resolution PNGs for
// that cost ~1s per frame at 2560x1600 -- the dominant part of a click_at's
// ~2.8s round trip.
type LumaGrid struct {
	W, H int // full-frame dimensions
	GW   int // grid width  (ceil(W/LumaStride))
	GH   int // grid height (ceil(H/LumaStride))
	Pix  []uint8
}

var (
	liveLumaWanted atomic.Bool
	liveLumaCh     = make(chan *LumaGrid, 1)
)

// RequestLiveLuma waits up to timeout for the decode thread's next frame
// and returns its LumaGrid. ok=false when no frame arrives in time, or when
// a platform getter owns frame delivery (macOS's liveFrameGetter -- callers
// then fall back to the PNG path via RequestLiveFrame).
func RequestLiveLuma(timeout time.Duration) (*LumaGrid, bool) {
	if liveFrameGetter.Load() != nil {
		return nil, false
	}
	select {
	case <-liveLumaCh:
	default:
	}
	liveLumaWanted.Store(true)
	defer liveLumaWanted.Store(false)
	select {
	case g := <-liveLumaCh:
		return g, true
	case <-time.After(timeout):
		return nil, false
	}
}

// LiveLumaWanted reports whether a RequestLiveLuma call is blocked waiting.
func LiveLumaWanted() bool { return liveLumaWanted.Load() }

// SubmitLiveLuma delivers a grid to a pending RequestLiveLuma. Non-blocking.
func SubmitLiveLuma(g *LumaGrid) {
	if !liveLumaWanted.CompareAndSwap(true, false) {
		return
	}
	select {
	case liveLumaCh <- g:
	default:
	}
}

// LumaGridChangePercent mirrors screenChangePercent (luma delta > 12 on a
// stride-4 grid) for two LumaGrids. Differing dimensions read as 100%.
func LumaGridChangePercent(a, b *LumaGrid) float64 {
	if a == nil || b == nil || a.W != b.W || a.H != b.H || len(a.Pix) != len(b.Pix) || len(a.Pix) == 0 {
		return 100
	}
	const lumaThreshold = 12
	changed := 0
	for i, v := range a.Pix {
		d := int(v) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > lumaThreshold {
			changed++
		}
	}
	return float64(changed) / float64(len(a.Pix)) * 100
}

var (
	liveRGBAWanted atomic.Bool
	liveRGBACh     = make(chan *image.RGBA, 1)
)

// RequestLiveRGBA is RequestLiveFrame without the PNG: the next decoded
// frame as raw pixels, for in-process consumers (local ui.parse) that would
// only decode it straight back. ok=false when no frame arrives in time or a
// platform getter owns frame delivery (callers fall back to RequestLiveFrame).
func RequestLiveRGBA(timeout time.Duration) (*image.RGBA, bool) {
	if liveFrameGetter.Load() != nil {
		return nil, false
	}
	select {
	case <-liveRGBACh:
	default:
	}
	liveRGBAWanted.Store(true)
	defer liveRGBAWanted.Store(false)
	select {
	case img := <-liveRGBACh:
		return img, true
	case <-time.After(timeout):
		return nil, false
	}
}

// LiveRGBAWanted reports whether a RequestLiveRGBA call is blocked waiting.
func LiveRGBAWanted() bool { return liveRGBAWanted.Load() }

// SubmitLiveRGBA delivers a frame to a pending RequestLiveRGBA. Non-blocking.
func SubmitLiveRGBA(img *image.RGBA) {
	if !liveRGBAWanted.CompareAndSwap(true, false) {
		return
	}
	select {
	case liveRGBACh <- img:
	default:
	}
}

// letterboxDarkMax is the highest channel value still treated as "black
// bar": encoded black is rarely exactly 0 after H.265.
const letterboxDarkMax = 8

// CropLetterbox removes pure-black bars the stream adds when the host's
// display aspect differs from the encode size (a 2560x1440 desktop in a
// 2560x1600 stream). The Agent normalizes click coordinates over the real
// display, not the padded frame, so an AI working from the padded frame
// misses by the bar offset. A crop is only applied when bars are present on
// BOTH opposite sides with similar thickness -- a dark window or a black
// wallpaper edge alone never triggers it. Returns img itself when there is
// nothing to crop; the result shares pixel memory with img.
func CropLetterbox(img *image.RGBA) *image.RGBA {
	if img == nil {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 64 || h < 64 {
		return img
	}
	rowDark := func(y int) bool {
		off := img.PixOffset(b.Min.X, b.Min.Y+y)
		for x := 0; x < w; x += 2 {
			p := img.Pix[off+x*4 : off+x*4+3]
			if p[0] > letterboxDarkMax || p[1] > letterboxDarkMax || p[2] > letterboxDarkMax {
				return false
			}
		}
		return true
	}
	colDark := func(x int) bool {
		for y := 0; y < h; y += 2 {
			off := img.PixOffset(b.Min.X+x, b.Min.Y+y)
			p := img.Pix[off : off+3]
			if p[0] > letterboxDarkMax || p[1] > letterboxDarkMax || p[2] > letterboxDarkMax {
				return false
			}
		}
		return true
	}
	count := func(n int, dark func(i int) bool, fromEnd bool) int {
		c := 0
		for c < n/2 {
			i := c
			if fromEnd {
				i = n - 1 - c
			}
			if !dark(i) {
				break
			}
			c++
		}
		return c
	}
	similar := func(a, b int) bool {
		if a < 4 || b < 4 {
			return false
		}
		d := a - b
		if d < 0 {
			d = -d
		}
		m := a
		if b > m {
			m = b
		}
		return d <= 4 || d*10 <= m
	}
	top, bottom := count(h, rowDark, false), count(h, rowDark, true)
	left, right := count(w, colDark, false), count(w, colDark, true)
	r := b
	if similar(top, bottom) {
		r.Min.Y += top
		r.Max.Y -= bottom
	}
	if similar(left, right) {
		r.Min.X += left
		r.Max.X -= right
	}
	if r == b || r.Dx() < 32 || r.Dy() < 32 {
		return img
	}
	return img.SubImage(r).(*image.RGBA)
}

// mcpDesktopSpace is true when the connected host maps absolute 0..32767
// onto the desktop itself (RustShine: pro/free/enterprise tariffs) instead of
// the encoded stream (Sunshine: it contain-fits in touch_port and wants
// stream-space coordinates). Only then are stream letterbox bars not part of
// the coordinate space an AI should work in. Set from the GUI's agent-protocol
// handling; false (nothing cropped) until a protocol is known.
var mcpDesktopSpace atomic.Bool

// SetMCPDesktopSpaceMouse records the host's absolute-mouse coordinate space.
func SetMCPDesktopSpaceMouse(desktopSpace bool) { mcpDesktopSpace.Store(desktopSpace) }

// CropForMCP is the frame the MCP surface (ui.parse, screen.get_image)
// hands out: letterbox bars removed on desktop-space hosts, untouched
// otherwise.
func CropForMCP(img *image.RGBA) *image.RGBA {
	if !mcpDesktopSpace.Load() {
		return img
	}
	return CropLetterbox(img)
}
