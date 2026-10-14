//go:build darwin && !ios && cgo

package service

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework CoreVideo -framework QuartzCore -framework CoreFoundation -framework Metal -framework AVFoundation -framework CoreMedia
// PyroWave decode (pyrowave_decode_darwin.m): PyroWave's native Metal decoder, one static
// archive from scripts/build_pyrowave_macos.sh exporting the pwm_* C API
// (third_party/pyrowave/metal/pyrowave_metal.h). No Vulkan or MoltenVK.
#cgo CFLAGS: -I${SRCDIR}/../../third_party/pyrowave/metal
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/pyrowave/build -lusbridge-pyrowave -lc++ -framework IOSurface

#include <stdint.h>
#include <CoreVideo/CoreVideo.h>

// Implemented in metal_video_impl_darwin.m (compiled as a separate translation unit).
extern int  metal_video_is_active(void);
extern int64_t metal_video_rendered_count(void);
extern int  metal_video_try_submit(CVImageBufferRef img);
extern int  metal_video_create(uintptr_t nsWinPtr, float x, float y, float w, float h);
extern void metal_video_update_frame(float x, float y, float w, float h);
extern void metal_video_destroy(void);
extern double metal_video_last_fps(void);
extern void metal_video_set_hidden(int hidden);
extern int  metal_video_get_last_frame_rgba(int *outW, int *outH, uint8_t **out);
extern void metal_video_set_hdr(int enabled);
extern void metal_video_set_upscale_mode(int mode);

extern void metal_video_set_overlay(const uint8_t *rgba, int w, int h, int stride);
extern void metal_video_clear_overlay(void);

extern void metal_video_set_hud_overlay(const uint8_t *rgba, int w, int h, int stride);
extern void metal_video_clear_hud_overlay(void);
extern void metal_video_set_hud_scale(float s);
extern double metal_video_last_decode_ms(void);
extern int    metal_video_decode_ms_available(void);
extern void metal_video_get_gpu_name(char *out, int out_len);

extern void metal_video_debug_link_counts(int64_t *created, int64_t *invalidated);

// Forward declaration matching the CGO-generated export signature (char*, not const char*).
extern void goMetalLog(char *msg, int level);
*/
import "C"

import (
	"bytes"
	"image"
	"image/png"
	"sync/atomic"
	"unsafe"

	"github.com/sirupsen/logrus"

	usbapi "usbridge-client/internal/api"
	"usbridge-client/internal/localui"
	"usbridge-client/internal/models"
)

// init wires the AI Vision overlay (ai_vision.go) to this platform's
// zero-copy Metal compositor -- see pushAIVisionOverlayToMetal's doc
// comment for why the CPU pixel-drawing approach ai_vision.go otherwise
// uses (drawCachedOverlay) doesn't reach frames that took the Metal fast
// path. nil on every platform without a metal_video_darwin.go (Linux,
// Windows, Android, iOS), where ai_vision.go's own in-place drawing is the
// only mechanism and these hooks are simply never set.
func init() {
	aiVisionMetalPush = pushAIVisionOverlayToMetal
	aiVisionMetalClear = MetalVideoClearOverlay

	// Net Graph HUD (net_graph.go): same "platform-agnostic core, thin
	// platform push hook" split as AI Vision above -- net_graph.go builds
	// the HUD image itself with no cgo dependency, and only these four
	// hooks touch the Metal-specific side (a dedicated small HUD layer, see
	// metal_video_impl_darwin.m's g_hud_layer, not the full-frame-sized
	// g_overlay_layer AI Vision uses).
	netGraphMetalPush = pushNetGraphOverlayToMetal
	netGraphMetalClear = MetalVideoClearHudOverlay
	netGraphScalePush = func(scale float32) {
		C.metal_video_set_hud_scale(C.float(scale))
	}
	netGraphRenderFPS = MetalVideoLastFPS
	netGraphDecodeMs = MetalVideoLastDecodeMs
	netGraphDecodeMsValid = MetalVideoDecodeMsAvailable
	netGraphGPUNameFn = MetalVideoGPUName
	benchRenderedFramesFn = MetalVideoRenderedCount

	// UpscaleMode (upscale_mode.go's cross-platform SetUpscaleMode) -- see
	// that file's own doc comment for why this hook indirection exists.
	upscaleModeMetalSet = MetalVideoSetUpscaleMode

	// Live-frame getter for local ui.parse / the Control footer's
	// Screenshot tool (see api/live_frame.go's liveFrameGetter doc
	// comment): answer instantly from g_lastRenderedBuf, the same stable
	// "last actually rendered" frame the pause-snapshot path
	// (VideoWidget.clearVideo) already uses, instead of arming a
	// wait-for-next-decoded-frame signal.
	usbapi.SetLiveFrameGetter(getLastRenderedFramePNG)
}

// getLastRenderedFramePNG PNG-encodes MetalVideoGetLastFrameRGBA's result,
// or returns nil if no frame has been rendered yet (no video session, or
// the very first frame hasn't arrived).
func getLastRenderedFramePNG() []byte {
	img := MetalVideoGetLastFrameRGBA()
	if img == nil {
		return nil
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		logrus.Warnf("🔎 [live-frame] PNG encode failed: %v", err)
		return nil
	}
	return buf.Bytes()
}

// pushNetGraphOverlayToMetal hands a just-built HUD canvas (see
// net_graph.go's buildNetGraphHUD) to the native compositor's dedicated HUD
// layer. Unlike pushAIVisionOverlayToMetal's full-frame-sized image, img
// here is always the small fixed HUD canvas -- cheap to upload even at
// net_graph.go's 10Hz cadence (see metal_video_impl_darwin.m's
// metal_video_set_hud_overlay doc comment).
// netGraphMetalWasActive tracks MetalVideoIsActive()'s last-seen value so
// transitions (not every push) get logged -- diagnoses the "HUD sometimes
// freezes" report: if the overlay goes inactive for a stretch (e.g. during
// a codec-switch restart's destroy/create cycle) the HUD legitimately
// stops updating for that whole stretch, which is a different cause than
// either of the two "gap" logs in net_graph.go / metal_video_impl_darwin.m.
// Remove once the report is resolved.
var netGraphMetalWasActive atomic.Bool

func pushNetGraphOverlayToMetal(img *image.RGBA) {
	active := MetalVideoIsActive()
	if active != netGraphMetalWasActive.Swap(active) {
		logrus.Infof("📊 [Net Graph] Metal overlay active=%v (HUD pushes %s while this is false)", active, map[bool]string{true: "resume", false: "stop"}[active])
	}
	if !active {
		return
	}
	MetalVideoSetHudOverlay(img.Pix, img.Rect.Dx(), img.Rect.Dy(), img.Stride)
}

// pushAIVisionOverlayToMetal renders a just-completed AI Vision detection
// result onto a transparent w×h canvas and hands it to the native
// compositor layer (metal_video_impl_darwin.m's g_overlay_layer), which
// Core Animation then composites on top of the video IOSurface layer at
// zero per-frame CPU cost. Called once per completed detection pass (every
// aiVisionInterval, see ai_vision.go), not per frame -- unlike
// drawCachedOverlay's in-place pixel writes, which only run on paths that
// already produce a CPU-writable buffer (Linux, the CPU decode fallback
// here) and never on this one.
func pushAIVisionOverlayToMetal(result *localui.Result, w, h int) {
	if !MetalVideoIsActive() {
		return // Metal fast path isn't the active renderer right now (e.g. CPU fallback) -- nothing to do.
	}
	img := buildAIVisionOverlayImage(result, w, h)
	MetalVideoSetOverlay(img.Pix, w, h, img.Stride)
}

// goMetalLog is called from C (metal_video_impl_darwin.m) to log via logrus.
//
//export goMetalLog
func goMetalLog(msg *C.char, level C.int) {
	text := C.GoString(msg)
	switch int(level) {
	case 1:
		logrus.Warnf("[Metal] %s", text)
	case 2:
		logrus.Errorf("[Metal] %s", text)
	default:
		logrus.Infof("[Metal] %s", text)
	}
}

// MetalVideoCreate creates (or replaces) the Metal overlay on the given NSWindow.
// Pass w=0, h=0 to cover the entire contentView (fullscreen mode).
func MetalVideoCreate(nsWin uintptr, x, y, w, h float32) bool {
	return C.metal_video_create(C.uintptr_t(nsWin), C.float(x), C.float(y), C.float(w), C.float(h)) != 0
}

// MetalVideoUpdateFrame repositions the Metal overlay to track the Fyne video widget.
func MetalVideoUpdateFrame(x, y, w, h float32) {
	C.metal_video_update_frame(C.float(x), C.float(y), C.float(w), C.float(h))
}

// MetalVideoDestroy removes the overlay and disables the Metal decode path.
func MetalVideoDestroy() {
	C.metal_video_destroy()
}

// MetalVideoIsActive reports whether the Metal overlay is live.
func MetalVideoIsActive() bool {
	return C.metal_video_is_active() != 0
}

// MetalVideoLastFPS returns the current Metal render FPS from the last ~2s window.
// Returns 0 if not enough frames yet.
func MetalVideoLastFPS() float64 {
	return float64(C.metal_video_last_fps())
}

// MetalVideoRenderedCount is benchRenderedFramesFn's darwin implementation --
// the streamer benchmark's client-side render-fps counter (bench_recorder.go
// diffs this between polling ticks). Valid only while the overlay is active,
// same convention as the Linux/Windows VKVideoGetStats().Rendered equivalent.
func MetalVideoRenderedCount() (int64, bool) {
	if !MetalVideoIsActive() {
		return 0, false
	}
	return int64(C.metal_video_rendered_count()), true
}

// MetalVideoGetLastFrameRGBA returns the last rendered VT frame as an image.RGBA.
// Returns nil if no frame has been rendered yet. Called once on stream stop — cost is acceptable.
func MetalVideoGetLastFrameRGBA() *image.RGBA {
	var w, h C.int
	var ptr *C.uint8_t
	if C.metal_video_get_last_frame_rgba(&w, &h, &ptr) == 0 || ptr == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(ptr))
	width, height := int(w), int(h)
	size := width * height * 4
	src := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), size)
	dst := make([]byte, size)
	copy(dst, src)
	return &image.RGBA{
		Pix:    dst,
		Stride: width * 4,
		Rect:   image.Rect(0, 0, width, height),
	}
}

// MetalVideoSetOverlay uploads an AI Vision detection overlay (a mostly-
// transparent RGBA image the same pixel size as the video frame) onto the
// native compositor's overlay CALayer, stacked above the video IOSurface
// layer -- see pushAIVisionOverlayToMetal. rgba must be non-empty and w/h/
// stride must describe it; a no-op call is silently ignored so a stray
// empty detection result can't crash into the cgo boundary.
func MetalVideoSetOverlay(rgba []byte, w, h, stride int) {
	if len(rgba) == 0 || w <= 0 || h <= 0 || stride <= 0 {
		return
	}
	C.metal_video_set_overlay((*C.uint8_t)(unsafe.Pointer(&rgba[0])), C.int(w), C.int(h), C.int(stride))
}

// MetalVideoClearOverlay removes the AI Vision overlay image (checkbox
// turned off, or a fresh detection found nothing) without touching the
// video layer underneath.
func MetalVideoClearOverlay() {
	C.metal_video_clear_overlay()
}

// MetalVideoSetHudOverlay uploads the Net Graph HUD canvas (a small, mostly-
// opaque RGBA image, see net_graph.go's buildNetGraphHUD) onto the native
// compositor's dedicated HUD layer, anchored bottom-right independent of the
// video content's own size/scaling -- see MetalVideoSetOverlay's doc
// comment for why this needs its own layer rather than reusing that one.
func MetalVideoSetHudOverlay(rgba []byte, w, h, stride int) {
	if len(rgba) == 0 || w <= 0 || h <= 0 || stride <= 0 {
		return
	}
	C.metal_video_set_hud_overlay((*C.uint8_t)(unsafe.Pointer(&rgba[0])), C.int(w), C.int(h), C.int(stride))
}

// MetalVideoClearHudOverlay removes the Net Graph HUD image (checkbox
// turned off) without touching the video or AI Vision layers.
func MetalVideoClearHudOverlay() {
	C.metal_video_clear_hud_overlay()
}

// MetalVideoLastDecodeMs returns the rolling-average submit-to-display
// latency (ms) from the current ~2s measurement window -- see
// metal_video_impl_darwin.m's metal_video_last_decode_ms doc comment. 0 if
// the overlay is inactive or no sample has landed yet.
func MetalVideoLastDecodeMs() float64 {
	return float64(C.metal_video_last_decode_ms())
}

// MetalVideoDecodeMsAvailable reports whether MetalVideoLastDecodeMs
// reflects a real measurement for the current session -- see
// metal_video_impl_darwin.m's metal_video_decode_ms_available doc comment.
// false on the AVSampleBufferDisplayLayer path (the main H.264/H.265
// pipeline), which has no equivalent measurement; Net Graph shows "DEC --"
// instead of a frozen, misleading "0.0ms" in that case.
func MetalVideoDecodeMsAvailable() bool {
	return C.metal_video_decode_ms_available() != 0
}

// MetalVideoGPUName returns the system default Metal device's name (e.g.
// "Apple M3 Pro"), for Net Graph's GPU line -- see
// metal_video_impl_darwin.m's metal_video_get_gpu_name doc comment for why
// this needs no active stream to answer. ok is false only if Metal itself
// is unavailable.
func MetalVideoGPUName() (string, bool) {
	buf := make([]C.char, 256)
	C.metal_video_get_gpu_name(&buf[0], C.int(len(buf)))
	name := C.GoString(&buf[0])
	return name, name != ""
}

// MetalVideoDebugLinkCounts returns how many CADisplayLinks
// metal_video_create has ever created vs. actually invalidated -- test-only
// instrumentation for metal_video_leak_darwin_test.go's leak regression test (see
// metal_video_impl_darwin.m's g_display_link_created_count doc comment).
func MetalVideoDebugLinkCounts() (created, invalidated int64) {
	var c, i C.int64_t
	C.metal_video_debug_link_counts(&c, &i)
	return int64(c), int64(i)
}

// MetalVideoSetHidden hides or shows the Metal overlay NSView without destroying it.
// Use this to let Fyne popups/menus render on top of the video.
func MetalVideoSetHidden(hidden bool) {
	h := C.int(0)
	if hidden {
		h = 1
	}
	C.metal_video_set_hidden(h)
}

// MetalVideoSetHdr toggles the video layer's EDR (extended dynamic range)
// presentation mode -- called from platform_set_video_format the moment the
// negotiated codec is known, before the first HDR frame ever arrives. See
// metal_video_impl_darwin.m's metal_video_set_hdr doc comment for why this
// is the only color-pipeline change needed on the render side (Core
// Animation's own compositor does the actual BT.2020/PQ -> display
// conversion using the color tags VideoToolbox already attaches to the
// decoded IOSurface).
func MetalVideoSetHdr(enabled bool) {
	e := C.int(0)
	if enabled {
		e = 1
	}
	C.metal_video_set_hdr(e)
}

// MetalVideoSetUpscaleMode selects how the decoded video frame is resized
// to fit the window when it isn't already an exact pixel match -- see
// models.UpscaleMode* for the string values and metal_video_impl_darwin.m's
// metal_video_set_upscale_mode for what each one actually does. Unlike
// MetalVideoSetHdr, this has no codec-negotiation dependency: called
// directly, as soon as the value is known/changed (see upscale_mode.go's
// SetUpscaleMode, called from startVideoWithParamsInternal alongside
// SetColor444/SetHdr).
func MetalVideoSetUpscaleMode(mode string) {
	m := C.int(0) // bilinear -- matches models.UpscaleModeBilinear and UPSCALE_MODE_BILINEAR
	switch mode {
	case models.UpscaleModeBicubic:
		m = 1
	case models.UpscaleModeLanczos:
		m = 2
	case models.UpscaleModeFSR1:
		m = 3
	}
	C.metal_video_set_upscale_mode(m)
}
