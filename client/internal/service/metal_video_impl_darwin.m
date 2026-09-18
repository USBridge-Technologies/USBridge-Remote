// metal_video_impl_darwin.m — ObjC implementation compiled as a separate translation unit.
// CGO picks up .m files automatically; keeping implementation here avoids
// duplicate-symbol errors that occur when C code is inlined in multiple
// Go files that all do `import "C"` in the same package.
#include <TargetConditionals.h>
#if !TARGET_OS_IPHONE

#import <AppKit/AppKit.h>
#import <CoreVideo/CoreVideo.h>
#import <QuartzCore/QuartzCore.h>
#include <stdatomic.h>
#include <pthread.h>
#include <stdio.h>
#include <time.h>

// Forward declaration — CGO generates this export from metal_video_darwin.go.
// Must use char* (not const char*) to match the CGO-generated signature.
extern void goMetalLog(char *msg, int level);

// ─────────────────────────────────────────────────────────────────────────────
// Global singleton state (one Metal overlay at a time).
// All NSView/CALayer access happens exclusively on the main thread.
// CVPixelBufferRef pending frame is protected by g_mu.
// ─────────────────────────────────────────────────────────────────────────────
static NSView  *g_view   = nil;
static CALayer *g_layer  = nil;

// AI Vision overlay layer, stacked directly above g_layer (the video
// IOSurface layer) and sharing its frame/gravity so a box drawn at pixel
// (x,y) of the detected frame lands on the exact same screen pixel the
// video content does, regardless of aspect-fit scaling. Populated via
// metal_video_set_overlay (see ai_vision.go's pushAIVisionOverlayToMetal)
// with a mostly-transparent RGBA image -- Core Animation composites it on
// the GPU for free every frame, so unlike the CPU-fallback decode path
// (moonlight_cgo_apple.go's vt_callback, which burns boxes directly into
// pixels via goAIVisionOverlay) this needs no per-frame CPU work at all:
// the image only changes once per completed detection pass.
static CALayer *g_overlay_layer = nil;

// Net Graph HUD layer, stacked above g_overlay_layer -- a small FIXED-size
// canvas anchored to the bottom-right corner of the view via its own frame
// (set directly in metal_video_set_hud_overlay), deliberately NOT sharing
// g_overlay_layer's full-video-frame sizing/gravity: AI Vision's overlay is
// sized to match the video content exactly so a box lands on the right
// pixel, but a net_graph HUD is a fixed small box independent of the video's
// resolution/aspect/scaling. Kept as its own layer rather than reusing
// g_overlay_layer so pushing it at 10-20Hz never means re-uploading a
// full-frame-sized (megabyte-class) image -- see net_graph.go's own doc
// comment on cost.
static CALayer *g_hud_layer = nil;

// Set while a metal_video_set_hud_overlay dispatch is in flight (queued or
// running on the main queue) -- a new push arriving before the previous one
// has actually run is dropped rather than queued, so a stalled/backed-up
// main thread (this app already logs "AppKit/DisplayLink stalled" warnings
// under load, independent of Net Graph) can't accumulate an ever-growing
// backlog of HUD-image dispatch_async blocks that would themselves further
// delay a later dispatch_sync (e.g. metal_video_destroy having to drain the
// same queue first before it can run). net_graph.go's HUD data collection
// still runs at its own full rate; only how often a fresh image actually
// reaches the screen is capped by this.
static atomic_int g_hud_push_pending = 0;

// HUD_MARGIN: device points between the HUD box and the bottom/right edges
// of the view. (0,0) in this layer's (unflipped, AppKit-default) coordinate
// space is already the bottom-left corner, so the Y side needs no flip
// math -- only the X side has to subtract the HUD's own width from the
// container's width to anchor it to the right instead, done in
// metal_video_set_hud_overlay itself (it needs the image width anyway).
#define HUD_MARGIN 12.0

static volatile atomic_int g_active           = 0;

// Display link — drives rendering at the display refresh rate, decoupled from VT decode timing.
// CADisplayLink (macOS 14+) fires on the main thread directly; no extra dispatch needed.
// Stored as id to avoid pulling in CoreVideo/CVDisplayLink headers here.
static CADisplayLink *g_display_link = nil;

static pthread_mutex_t g_mu = PTHREAD_MUTEX_INITIALIZER;
static CVPixelBufferRef g_pendingBuf = NULL;
// Timestamp (mono_sec) of the metal_video_try_submit call that produced
// g_pendingBuf, captured under the same g_mu as the buffer itself. Read back
// out when the display link drains g_pendingBuf so metal_render_main_with_buf
// can report "how long did this frame sit between decode-submit and actual
// display" -- a stand-in for true per-frame decode latency (see
// metal_video_last_decode_ms's doc comment) that needs no extra plumbing
// through the VT decode callback.
static double g_pendingBufSubmitTime = 0.0;

static int64_t g_submitCount = 0;
static int64_t g_renderCount = 0;
static int64_t g_fpsFrames      = 0;
static double  g_fpsStart       = 0.0;
static double  g_lastKnownFps   = 0.0; // last computed value, returned during reset gap

// Decode/render-latency rolling window -- same shape as the fps window
// above (accumulate, snapshot+reset every 2s, cache last value during the
// reset gap), just averaging submit-to-display latency (ms) instead of a
// frame count. Fed by metal_render_main_with_buf's latencyMs argument.
static double  g_decodeMsSum    = 0.0;
static int64_t g_decodeSamples  = 0;
static double  g_lastKnownDecodeMs = 0.0;
static int     g_lastW = 0, g_lastH = 0;
static int     g_fullWindow  = 0; // 1 when overlay covers the full contentView (fullscreen mode)

// Last rendered pixel buffer — retained for pause snapshot (read by metal_video_get_last_frame_rgba).
static CVPixelBufferRef g_lastRenderedBuf = NULL;

// Desired EDR presentation state, set by metal_video_set_hdr -- persisted
// independently of g_layer's own lifetime (metal_video_create tears down
// and rebuilds a fresh CALayer on every new session) so a call that arrives
// before metal_video_create runs isn't silently lost: metal_video_create
// applies this value to the freshly-created layer itself, and
// metal_video_set_hdr applies it directly whenever g_layer already exists.
static atomic_int g_hdr_enabled = 0;

// Forward declaration -- defined further down (near metal_video_set_hdr),
// used by metal_video_create above that definition.
static void apply_dynamic_range(CALayer *layer, BOOL hdr);

static double mono_sec(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}

// ─────────────────────────────────────────────────────────────────────────────
// Render — called on main thread; OWNS buf and always releases it.
// ─────────────────────────────────────────────────────────────────────────────
static void metal_render_main_with_buf(CVPixelBufferRef buf, double latencyMs) {
    if (!atomic_load(&g_active) || !g_layer) {
        CVPixelBufferRelease(buf);
        return;
    }

    int w = (int)CVPixelBufferGetWidth(buf);
    int h = (int)CVPixelBufferGetHeight(buf);

    // IOSurface path: CALayer compositor reads GPU memory directly — zero CPU copy.
    IOSurfaceRef surf = CVPixelBufferGetIOSurface(buf);
    if (surf) {
        [CATransaction begin];
        [CATransaction setDisableActions:YES];
        g_layer.contents = (__bridge id)surf;
        [CATransaction commit];
    }

    // Save a retained copy for the pause snapshot (cheap retain; releases old).
    CVPixelBufferRetain(buf);
    CVPixelBufferRef old_last = g_lastRenderedBuf;
    g_lastRenderedBuf = buf;
    if (old_last) CVPixelBufferRelease(old_last);

    // Release the caller's ref (g_lastRenderedBuf holds its own).
    CVPixelBufferRelease(buf);

    // ── Logging ──────────────────────────────────────────────────────────────
    int64_t n = ++g_renderCount;
    if (n == 1) {
        g_lastW = w; g_lastH = h;
        g_fpsStart  = mono_sec();
        g_fpsFrames = 0;
        char msg[128];
        snprintf(msg, sizeof(msg),
                 "first frame rendered — %dx%d  iosurface=%s",
                 w, h, surf ? "yes" : "no (fallback)");
        goMetalLog(msg, 0);
    }

    g_lastW = w; g_lastH = h;
    g_fpsFrames++;
    if (latencyMs >= 0.0) {
        g_decodeMsSum += latencyMs;
        g_decodeSamples++;
    }
    double now     = mono_sec();
    double elapsed = now - g_fpsStart;
    // Early snapshot after 30 frames to catch FPS issues quickly.
    if (n == 30 && g_fpsStart > 0.0) {
        char msg[192];
        snprintf(msg, sizeof(msg),
                 "first 30 frames: fps=%.1f  submitted=%lld  size=%dx%d",
                 (double)g_fpsFrames / elapsed,
                 (long long)g_submitCount, g_lastW, g_lastH);
        goMetalLog(msg, 0);
    }
    if (elapsed >= 2.0 && g_fpsFrames > 0) {
        g_lastKnownFps = (double)g_fpsFrames / elapsed;
        if (g_decodeSamples > 0) {
            g_lastKnownDecodeMs = g_decodeMsSum / (double)g_decodeSamples;
        }
        char msg[192];
        snprintf(msg, sizeof(msg),
                 "fps=%.1f  rendered=%lld  submitted=%lld  size=%dx%d  decodeMs=%.1f",
                 g_lastKnownFps,
                 (long long)g_renderCount, (long long)g_submitCount,
                 g_lastW, g_lastH, g_lastKnownDecodeMs);
        goMetalLog(msg, 0);
        g_fpsStart      = now;
        g_fpsFrames     = 0;
        g_decodeMsSum   = 0.0;
        g_decodeSamples = 0;
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// CADisplayLink target — fires on the main thread at the display refresh rate.
// Drains g_pendingBuf and renders directly; no extra dispatch needed.
// ─────────────────────────────────────────────────────────────────────────────
@interface MetalDisplayLinkTarget : NSObject
- (void)displayLinkFired:(CADisplayLink *)link;
@end
#include <mach/mach_time.h>

static uint64_t g_last_dl_time = 0;
static uint64_t g_last_submit_time = 0;

// TEMP DIAGNOSTIC (render-throughput regression investigation): counts every
// displayLinkFired call and every time it found a pending buffer, logged
// every ~2s -- distinguishes "CVDisplayLink itself isn't firing at the
// display refresh rate" from "it's firing fine but g_pendingBuf is usually
// empty/stale by the time it checks".
static uint64_t g_dl_fire_count = 0;
static uint64_t g_dl_hit_count = 0;
static double g_dl_diag_start = 0.0;

@implementation MetalDisplayLinkTarget
- (void)displayLinkFired:(CADisplayLink __unused *)link {
    if (!atomic_load(&g_active)) return;

    g_dl_fire_count++;
    double diagNow = mono_sec();
    if (g_dl_diag_start == 0.0) g_dl_diag_start = diagNow;
    double diagElapsed = diagNow - g_dl_diag_start;
    if (diagElapsed >= 2.0) {
        char diagMsg[160];
        snprintf(diagMsg, sizeof(diagMsg),
                 "[DIAG] DisplayLink fire_rate=%.1fHz hit_rate=%.1fHz (fires=%llu hits=%llu window=%.1fs)",
                 (double)g_dl_fire_count / diagElapsed, (double)g_dl_hit_count / diagElapsed,
                 (unsigned long long)g_dl_fire_count, (unsigned long long)g_dl_hit_count, diagElapsed);
        goMetalLog(diagMsg, 0);
        g_dl_fire_count = 0;
        g_dl_hit_count = 0;
        g_dl_diag_start = diagNow;
    }

    // Stutter Profiler: DisplayLink stall detection
    uint64_t now = mach_absolute_time();
    if (g_last_dl_time != 0) {
        mach_timebase_info_data_t tb;
        mach_timebase_info(&tb);
        uint64_t elapsed_ns = (now - g_last_dl_time) * tb.numer / tb.denom;
        if (elapsed_ns > 50000000) { // 50ms
            char msg[128];
            snprintf(msg, sizeof(msg), "⚠️ [Profiler] AppKit/DisplayLink stalled for %llu ms (UI freeze!)", elapsed_ns / 1000000);
            goMetalLog(msg, 2); // warn
        }
    }
    g_last_dl_time = now;

    pthread_mutex_lock(&g_mu);
    CVPixelBufferRef buf = g_pendingBuf;
    double submitTime = g_pendingBufSubmitTime;
    g_pendingBuf = NULL;
    pthread_mutex_unlock(&g_mu);
    if (!buf) return;
    g_dl_hit_count++;
    double latencyMs = submitTime > 0.0 ? (mono_sec() - submitTime) * 1000.0 : -1.0;
    metal_render_main_with_buf(buf, latencyMs); // already on main thread
}
@end
static MetalDisplayLinkTarget *g_dl_target = nil;

// ─────────────────────────────────────────────────────────────────────────────
// Public C API
// ─────────────────────────────────────────────────────────────────────────────

int metal_video_is_active(void) {
    return atomic_load(&g_active);
}

// Returns the Metal render FPS from the current measurement window.
// Falls back to the last known value during the brief reset gap so callers
// never see a spurious zero while frames are still being rendered.
double metal_video_last_fps(void) {
    if (!atomic_load(&g_active)) return 0.0;
    if (g_fpsStart == 0.0) return 0.0; // no frames at all yet
    double elapsed = mono_sec() - g_fpsStart;
    if (g_fpsFrames == 0 || elapsed < 0.5) {
        // Just reset the window — return last known value to avoid a gap.
        return g_lastKnownFps;
    }
    return (double)g_fpsFrames / elapsed;
}

// Returns the rolling-average submit-to-display latency (ms) from the
// current measurement window (same window as metal_video_last_fps, see
// g_decodeMsSum's doc comment) -- a stand-in for true per-frame decode
// latency, cheap to compute since it needs no extra plumbing through the VT
// decode callback. Falls back to the last known value during the brief
// reset gap, same as metal_video_last_fps. 0 if no sample has ever landed
// (e.g. overlay inactive, or every submit happened before any pendingBuf
// timestamp was set).
double metal_video_last_decode_ms(void) {
    if (!atomic_load(&g_active)) return 0.0;
    return g_lastKnownDecodeMs;
}

// One-shot diagnostic for the HDR black-screen investigation (2026-09-14):
// logs into app.log (unlike metal_video_impl_ios.m's NSLog-only equivalent,
// which never reaches it) exactly which of the two early-out checks below
// -- inactive overlay vs. no IOSurface -- is actually responsible when every
// frame silently drops on the CPU-fallback-rejects-10-bit path in
// moonlight_cgo_apple.go's vt_callback. Safe to leave in: fires once per
// process, not per frame.
static _Atomic int g_submit_call_count = 0;
int metal_video_try_submit(CVImageBufferRef img) {
    if (atomic_fetch_add(&g_submit_call_count, 1) == 0) {
        char msg[128];
        snprintf(msg, sizeof(msg), "metal_video_try_submit: first call (active=%d has_iosurface=%d)",
                 atomic_load(&g_active), CVPixelBufferGetIOSurface(img) != NULL ? 1 : 0);
        goMetalLog(msg, 0);
    }
    if (!atomic_load(&g_active)) return 0;
    if (!CVPixelBufferGetIOSurface(img)) return 0;

    // Stutter Profiler: Decoder stall detection
    uint64_t now = mach_absolute_time();
    if (g_last_submit_time != 0) {
        mach_timebase_info_data_t tb;
        mach_timebase_info(&tb);
        uint64_t elapsed_ns = (now - g_last_submit_time) * tb.numer / tb.denom;
        if (elapsed_ns > 50000000) { // 50ms
            char msg[128];
            snprintf(msg, sizeof(msg), "⚠️ [Profiler] Moonlight Decoder/Network stalled for %llu ms (Dropped packets or host keyframe!)", elapsed_ns / 1000000);
            goMetalLog(msg, 2); // warn
        }
    }
    g_last_submit_time = now;

    CVPixelBufferRetain(img);

    pthread_mutex_lock(&g_mu);
    CVPixelBufferRef old = g_pendingBuf;
    g_pendingBuf = (CVPixelBufferRef)img;
    g_pendingBufSubmitTime = mono_sec();
    g_submitCount++;
    pthread_mutex_unlock(&g_mu);

    if (old) CVPixelBufferRelease(old);
    // CVDisplayLink drains g_pendingBuf at the display refresh rate — no dispatch here.
    return 1;
}

// ─────────────────────────────────────────────────────────────────────────────
// AI Vision overlay — composited as its own CALayer (see g_overlay_layer's
// doc comment) instead of touching pixels, since metal_video_try_submit's
// IOSurface path never produces a CPU-writable frame buffer to draw into.
// rgba must be straight/premultiplied-equivalent RGBA (alpha 0 or 255 only,
// which both are the same thing) at stride*h bytes -- exactly what
// image.RGBA produces, see ai_vision.go's pushAIVisionOverlayToMetal.
// ─────────────────────────────────────────────────────────────────────────────

void metal_video_set_overlay(const uint8_t *rgba, int w, int h, int stride) {
    if (!atomic_load(&g_active) || !rgba || w <= 0 || h <= 0 || stride <= 0) return;

    // Copy now (NSData:dataWithBytes: copies) — the caller's buffer is a Go
    // slice only valid for the duration of this call.
    NSData *data = [NSData dataWithBytes:rgba length:(size_t)stride * (size_t)h];

    dispatch_block_t blk = ^{
        if (!g_overlay_layer) return;
        CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
        CGDataProviderRef provider = CGDataProviderCreateWithCFData((CFDataRef)data);
        CGImageRef img = CGImageCreate((size_t)w, (size_t)h, 8, 32, (size_t)stride, cs,
            kCGImageAlphaPremultipliedLast | kCGBitmapByteOrderDefault,
            provider, NULL, false, kCGRenderingIntentDefault);
        CGDataProviderRelease(provider);
        CGColorSpaceRelease(cs);
        if (!img) return;

        [CATransaction begin];
        [CATransaction setDisableActions:YES];
        g_overlay_layer.contents = (__bridge id)img; // CALayer retains it internally
        [CATransaction commit];
        CGImageRelease(img);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

void metal_video_clear_overlay(void) {
    dispatch_block_t blk = ^{
        if (g_overlay_layer) g_overlay_layer.contents = nil;
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// ─────────────────────────────────────────────────────────────────────────────
// Net Graph HUD overlay — its own small CALayer (see g_hud_layer's doc
// comment), anchored bottom-right via a fixed frame set on every push
// (cheap: w/h rarely change once the HUD is built once in Go). Structured
// like metal_video_set_overlay, plus a busy-guard (g_hud_push_pending) that
// drops a push outright instead of queuing it behind a main thread that's
// currently stalled -- see that var's own doc comment for why unbounded
// queuing there is a real problem, not just a theoretical one. See
// net_graph.go's buildNetGraphHUD for what actually gets uploaded here.
// ─────────────────────────────────────────────────────────────────────────────

void metal_video_set_hud_overlay(const uint8_t *rgba, int w, int h, int stride) {
    if (!atomic_load(&g_active) || !rgba || w <= 0 || h <= 0 || stride <= 0) return;

    int expected = 0;
    if (!atomic_compare_exchange_strong(&g_hud_push_pending, &expected, 1)) {
        return; // a previous push is still queued/running -- drop this one rather than pile up
    }

    NSData *data = [NSData dataWithBytes:rgba length:(size_t)stride * (size_t)h];

    dispatch_block_t blk = ^{
        // Released as soon as we actually start running (not at the end):
        // once the queue is draining again, the next push is allowed
        // through immediately rather than waiting for this one's CATransaction
        // to fully commit.
        atomic_store(&g_hud_push_pending, 0);

        if (!g_hud_layer) return;
        CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
        CGDataProviderRef provider = CGDataProviderCreateWithCFData((CFDataRef)data);
        CGImageRef img = CGImageCreate((size_t)w, (size_t)h, 8, 32, (size_t)stride, cs,
            kCGImageAlphaPremultipliedLast | kCGBitmapByteOrderDefault,
            provider, NULL, false, kCGRenderingIntentDefault);
        CGDataProviderRelease(provider);
        CGColorSpaceRelease(cs);
        if (!img) return;

        // Bottom-right anchor: (0,0) is already the bottom-left corner in
        // this unflipped coordinate space (see HUD_MARGIN's doc comment),
        // so only X needs adjusting -- push the box's right edge in from
        // the container's own right edge by HUD_MARGIN.
        CGFloat containerW = g_hud_layer.superlayer ? g_hud_layer.superlayer.bounds.size.width : (CGFloat)w;
        CGFloat x = containerW - HUD_MARGIN - (CGFloat)w;
        if (x < HUD_MARGIN) x = HUD_MARGIN; // clamp: a window narrower than the HUD pins it left instead of going negative

        [CATransaction begin];
        [CATransaction setDisableActions:YES];
        g_hud_layer.frame = CGRectMake(x, HUD_MARGIN, (CGFloat)w, (CGFloat)h);
        g_hud_layer.contents = (__bridge id)img;
        [CATransaction commit];
        CGImageRelease(img);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

void metal_video_clear_hud_overlay(void) {
    dispatch_block_t blk = ^{
        if (g_hud_layer) g_hud_layer.contents = nil;
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// Forward declaration — CGO generates this export from video_widget_metal_darwin.go.
extern void goMetalMouseEvent(int typ, float x, float y, int btn);

// ─────────────────────────────────────────────────────────────────────────────
// USBridgeMetalView: captures all pointer events and sends them directly to Go.
// ─────────────────────────────────────────────────────────────────────────────
@interface USBridgeMetalView : NSView
@end
@implementation USBridgeMetalView

- (BOOL)acceptsFirstResponder { return YES; }
- (BOOL)acceptsFirstMouse:(NSEvent __unused *)event { return YES; }

// Tracking area: deliver mouseMoved: (and drag events) even without a held button.
- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    for (NSTrackingArea *ta in self.trackingAreas.copy) [self removeTrackingArea:ta];
    NSTrackingAreaOptions opts = NSTrackingActiveInKeyWindow
                               | NSTrackingMouseMoved
                               | NSTrackingInVisibleRect;
    [self addTrackingArea:[[NSTrackingArea alloc]
        initWithRect:NSZeroRect options:opts owner:self userInfo:nil]];
}

// ── coordinate helper ──────────────────────────────────────────────────────
// NSView coordinates have origin at bottom-left; Fyne expects top-left.
- (void)pushMoveEvent:(NSEvent *)e {
    NSPoint pt = [self convertPoint:e.locationInWindow fromView:nil];
    goMetalMouseEvent(1, (float)pt.x, (float)(self.bounds.size.height - pt.y), 0);
}
- (void)pushButtonEvent:(int)type btn:(int)btn event:(NSEvent *)e {
    NSPoint pt = [self convertPoint:e.locationInWindow fromView:nil];
    goMetalMouseEvent(type, (float)pt.x, (float)(self.bounds.size.height - pt.y), btn);
}

// ── mouse movement (no button held) ───────────────────────────────────────
- (void)mouseMoved:(NSEvent *)e       { [self pushMoveEvent:e]; }
// ── drag (button held) ────────────────────────────────────────────────────
- (void)mouseDragged:(NSEvent *)e     { [self pushMoveEvent:e]; }
- (void)rightMouseDragged:(NSEvent *)e{ [self pushMoveEvent:e]; }
- (void)otherMouseDragged:(NSEvent *)e{ [self pushMoveEvent:e]; }
// ── buttons ───────────────────────────────────────────────────────────────
- (void)mouseDown:(NSEvent *)e        { [self pushButtonEvent:2 btn:1 event:e]; }
- (void)mouseUp:(NSEvent *)e          { [self pushButtonEvent:3 btn:1 event:e]; }
- (void)rightMouseDown:(NSEvent *)e   { [self pushButtonEvent:2 btn:3 event:e]; }
- (void)rightMouseUp:(NSEvent *)e     { [self pushButtonEvent:3 btn:3 event:e]; }
- (void)otherMouseDown:(NSEvent *)e   { [self pushButtonEvent:2 btn:2 event:e]; }
- (void)otherMouseUp:(NSEvent *)e     { [self pushButtonEvent:3 btn:2 event:e]; }
// ── scroll wheel ──────────────────────────────────────────────────────────
- (void)scrollWheel:(NSEvent *)e {
    int btn = (e.scrollingDeltaY >= 0) ? 4 : 5; // 4=up 5=down
    NSPoint pt = [self convertPoint:e.locationInWindow fromView:nil];
    goMetalMouseEvent(2, (float)pt.x, (float)(self.bounds.size.height - pt.y), btn);
}

@end

static NSRect fyne_to_nsrect(CGFloat cvH, float x, float y, float w, float h) {
    return NSMakeRect((CGFloat)x, cvH - (CGFloat)y - (CGFloat)h,
                      (CGFloat)w, (CGFloat)h);
}

int metal_video_create(uintptr_t nsWinPtr, float x, float y, float w, float h) {
    if (!nsWinPtr) return 0;
    __block int ok = 0;
    dispatch_block_t blk = ^{
        NSWindow *win = (__bridge NSWindow *)((void *)nsWinPtr);
        NSView   *cv  = win.contentView;
        if (!cv) { goMetalLog("metal_video_create: no contentView", 2); return; }

        if (g_view) {
            [g_view removeFromSuperview];
            g_view  = nil;
            g_layer = nil;
            g_overlay_layer = nil;
            g_hud_layer = nil;
        }

        g_fullWindow = (w <= 0 || h <= 0);
        CGFloat cvH = cv.bounds.size.height;
        NSRect frame = g_fullWindow ? cv.bounds
                                    : fyne_to_nsrect(cvH, x, y, w, h);

        USBridgeMetalView *ov = [[USBridgeMetalView alloc] initWithFrame:frame];
        ov.wantsLayer = YES;
        ov.layer.backgroundColor = CGColorGetConstantColor(kCGColorBlack);

        CALayer *vl = [CALayer layer];
        vl.frame = ov.bounds;
        vl.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
        vl.contentsGravity = kCAGravityResizeAspect;
        vl.backgroundColor = CGColorGetConstantColor(kCGColorBlack);
        vl.contentsScale   = NSScreen.mainScreen.backingScaleFactor;
        // Applies whatever metal_video_set_hdr's most recent call requested
        // -- see g_hdr_enabled's own doc comment: this layer is rebuilt
        // fresh every session, so the desired state has to be re-applied
        // here rather than assumed to survive from a previous session's
        // (now-destroyed) layer.
        apply_dynamic_range(vl, atomic_load(&g_hdr_enabled) != 0);
        [ov.layer addSublayer:vl];

        CALayer *ol = [CALayer layer];
        ol.frame = ov.bounds;
        ol.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
        ol.contentsGravity = kCAGravityResizeAspect;
        ol.contentsScale   = NSScreen.mainScreen.backingScaleFactor;
        [ov.layer addSublayer:ol]; // above vl -> boxes render on top of video

        CALayer *hl = [CALayer layer];
        hl.contentsScale = NSScreen.mainScreen.backingScaleFactor;
        // No frame yet -- metal_video_set_hud_overlay sets it (bottom-left
        // anchored, HUD_MARGIN) the first time net_graph.go pushes an image;
        // no autoresizingMask either, since this is a fixed-size box, unlike
        // vl/ol which track the whole view.
        [ov.layer addSublayer:hl]; // above ol -> HUD renders on top of everything

        [cv addSubview:ov];

        g_view  = ov;
        g_layer = vl;
        g_overlay_layer = ol;
        g_hud_layer = hl;

        g_submitCount = 0; g_renderCount = 0;
        g_fpsFrames = 0;   g_fpsStart = 0;   g_lastKnownFps = 0.0;
        g_decodeMsSum = 0.0; g_decodeSamples = 0; g_lastKnownDecodeMs = 0.0;
        g_pendingBufSubmitTime = 0.0;
        atomic_store(&g_hud_push_pending, 0);
        g_lastW = 0;       g_lastH = 0;
        atomic_store(&g_submit_call_count, 0); // re-arm the one-shot try_submit diagnostic for this session
        pthread_mutex_lock(&g_mu);
        CVPixelBufferRef old = g_pendingBuf;
        g_pendingBuf = NULL;
        pthread_mutex_unlock(&g_mu);
        if (old) CVPixelBufferRelease(old);

        atomic_store(&g_active, 1);

        // Start a CADisplayLink that fires on the main thread at the display refresh
        // rate (60/120 Hz). This decouples rendering from VT decode timing and gives
        // vsync-aligned frames regardless of network or decode jitter.
        if (!g_dl_target) g_dl_target = [MetalDisplayLinkTarget new];
        g_display_link = [ov displayLinkWithTarget:g_dl_target
                                          selector:@selector(displayLinkFired:)];
        [g_display_link addToRunLoop:[NSRunLoop mainRunLoop]
                             forMode:NSRunLoopCommonModes];

        ok = 1;

        char msg[192];
        if (g_fullWindow) {
            snprintf(msg, sizeof(msg),
                     "overlay created (full-window %.0fx%.0f) NSWindow=%p",
                     cv.bounds.size.width, cv.bounds.size.height, (void*)nsWinPtr);
        } else {
            snprintf(msg, sizeof(msg),
                     "overlay created %.0fx%.0f at (%.0f,%.0f)  NSWindow=%p",
                     (double)w, (double)h, (double)x, (double)y, (void*)nsWinPtr);
        }
        goMetalLog(msg, 0);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_sync(dispatch_get_main_queue(), blk);
    return ok;
}

void metal_video_update_frame(float x, float y, float w, float h) {
    if (!atomic_load(&g_active) || !g_view) return;
    // Full-window (fullscreen) overlay is pinned to the contentView — don't resize it.
    if (g_fullWindow) return;
    dispatch_block_t blk = ^{
        if (!g_view) return;
        NSView *cv = g_view.superview;
        if (!cv) return;
        CGFloat cvH = cv.bounds.size.height;
        NSRect nr = fyne_to_nsrect(cvH, x, y, w, h);
        if (!NSEqualRects(g_view.frame, nr)) g_view.frame = nr;
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// Copies the last rendered frame to a caller-owned RGBA buffer.
// Returns 1 on success; caller must free(*out) with free().
// Safe to call from any thread; uses the main queue for pixel access.
int metal_video_get_last_frame_rgba(int *outW, int *outH, uint8_t **out) {
    if (!g_lastRenderedBuf) return 0;

    CVPixelBufferRef buf = g_lastRenderedBuf;
    CVPixelBufferRetain(buf);

    int w = (int)CVPixelBufferGetWidth(buf);
    int h = (int)CVPixelBufferGetHeight(buf);
    if (w <= 0 || h <= 0) { CVPixelBufferRelease(buf); return 0; }

    CVPixelBufferLockBaseAddress(buf, kCVPixelBufferLock_ReadOnly);
    uint8_t *src    = (uint8_t *)CVPixelBufferGetBaseAddress(buf);
    size_t srcStride = CVPixelBufferGetBytesPerRow(buf);

    uint8_t *rgba = (uint8_t *)malloc((size_t)w * (size_t)h * 4);
    if (!rgba) {
        CVPixelBufferUnlockBaseAddress(buf, kCVPixelBufferLock_ReadOnly);
        CVPixelBufferRelease(buf);
        return 0;
    }

    // VT decodes to kCVPixelFormatType_32BGRA; swap B↔R to produce RGBA.
    for (int y = 0; y < h; y++) {
        uint8_t *s = src  + (size_t)y * srcStride;
        uint8_t *d = rgba + (size_t)y * (size_t)w * 4;
        for (int x = 0; x < w; x++, s += 4, d += 4) {
            d[0] = s[2]; d[1] = s[1]; d[2] = s[0]; d[3] = s[3];
        }
    }

    CVPixelBufferUnlockBaseAddress(buf, kCVPixelBufferLock_ReadOnly);
    CVPixelBufferRelease(buf);

    *outW = w; *outH = h; *out = rgba;
    return 1;
}

void metal_video_set_hidden(int hidden) {
    if (!atomic_load(&g_active)) return;
    dispatch_block_t blk = ^{
        if (g_view) g_view.hidden = (hidden != 0);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// apply_dynamic_range sets a layer's EDR presentation mode via whichever API
// this OS actually has: `preferredDynamicRange` (macOS 26+) is the
// non-deprecated replacement for `wantsExtendedDynamicRangeContent`
// (deprecated in the same release, still the only option before it) -- see
// CALayer.h's own API_AVAILABLE/API_DEPRECATED annotations. This project's
// floor is macOS 14 (see this file's own CADisplayLink usage), well below
// 26, so the deprecated property is still the only thing that actually
// exists at runtime on most machines this ships to today; branching at
// runtime rather than picking one gets both "works everywhere this ships"
// and "no deprecated-API warning/behavior on the OS that already moved on".
static void apply_dynamic_range(CALayer *layer, BOOL hdr) {
    if (@available(macOS 26.0, *)) {
        layer.preferredDynamicRange = hdr ? CADynamicRangeHigh : CADynamicRangeStandard;
    } else {
        // Deliberate: this is the guarded pre-26 fallback for a property
        // deprecated exactly at 26 -- there is no other API to use here on
        // an OS this old, so the deprecation warning is expected noise, not
        // a real "you should update this" signal.
        #pragma clang diagnostic push
        #pragma clang diagnostic ignored "-Wdeprecated-declarations"
        layer.wantsExtendedDynamicRangeContent = hdr;
        #pragma clang diagnostic pop
    }
}

// metal_video_set_hdr toggles g_layer's EDR (extended dynamic range)
// presentation mode -- called from moonlight_cgo_apple.go's
// platform_set_video_format the moment the negotiated codec is known (see
// that function's own comment), before the first frame ever reaches
// metal_render_main_with_buf. Deliberately just this one property, no
// custom shader/texture pipeline: the video layer here is a PLAIN CALayer
// (not CAMetalLayer, despite this file's name -- see
// metal_render_main_with_buf's own "IOSurface path" comment) whose
// `contents` is set directly to the decoded frame's IOSurface every frame;
// Core Animation's own compositor already does the accurate YUV(BT.2020,PQ)
// -> display conversion using the color primaries/transfer function/matrix
// VideoToolbox tagged onto that IOSurface from the stream's own signaled
// colorimetry (see rust-shine's video-encode::videotoolbox module, which
// sets those tags explicitly for an HDR session) -- letting the system do
// this rather than hand-rolling a PQ EOTF conversion in a shader is both
// the more correct (accurate, tested-by-Apple color science) and the
// faster (zero extra GPU work beyond what SDR frames already do) choice.
// See apply_dynamic_range's own doc comment for which actual CALayer
// property this ends up touching on a given OS version.
void metal_video_set_hdr(int enabled) {
    atomic_store(&g_hdr_enabled, enabled != 0);
    dispatch_block_t blk = ^{
        if (!g_layer) return; // metal_video_create (see its own comment) applies g_hdr_enabled once it exists
        BOOL want = (atomic_load(&g_hdr_enabled) != 0);
        apply_dynamic_range(g_layer, want);
        char msg[64];
        snprintf(msg, sizeof(msg), "HDR presentation %s", want ? "enabled" : "disabled");
        goMetalLog(msg, 0);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

void metal_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_active, 0);
    g_fullWindow = 0;

    // Invalidate the display link before teardown so no more callbacks fire.
    // -invalidate is safe from any thread and is idempotent.
    if (g_display_link) {
        [g_display_link invalidate];
        g_display_link = nil;
    }

    dispatch_block_t blk = ^{
        if (g_view) {
            [g_view removeFromSuperview];
            g_view  = nil;
            g_layer = nil;
            g_overlay_layer = nil;
            g_hud_layer = nil;
        }
        atomic_store(&g_hud_push_pending, 0);
        pthread_mutex_lock(&g_mu);
        CVPixelBufferRef old = g_pendingBuf;
        g_pendingBuf = NULL;
        g_pendingBufSubmitTime = 0.0;
        pthread_mutex_unlock(&g_mu);
        if (old) CVPixelBufferRelease(old);

        if (g_lastRenderedBuf) {
            CVPixelBufferRelease(g_lastRenderedBuf);
            g_lastRenderedBuf = NULL;
        }

        char msg[192];
        snprintf(msg, sizeof(msg),
                 "overlay destroyed — rendered=%lld  submitted=%lld",
                 (long long)g_renderCount, (long long)g_submitCount);
        goMetalLog(msg, 0);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_sync(dispatch_get_main_queue(), blk);
}

#endif // !TARGET_OS_IPHONE
