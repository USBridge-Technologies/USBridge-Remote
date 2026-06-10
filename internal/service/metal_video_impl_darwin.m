// metal_video_impl_darwin.m — ObjC implementation compiled as a separate translation unit.
// CGO picks up .m files automatically; keeping implementation here avoids
// duplicate-symbol errors that occur when C code is inlined in multiple
// Go files that all do `import "C"` in the same package.

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

static volatile atomic_int g_active           = 0;
static volatile atomic_int g_pending_dispatch = 0;

static pthread_mutex_t g_mu = PTHREAD_MUTEX_INITIALIZER;
static CVPixelBufferRef g_pendingBuf = NULL;

static int64_t g_submitCount = 0;
static int64_t g_renderCount = 0;
static int64_t g_fpsFrames   = 0;
static double  g_fpsStart    = 0.0;
static int     g_lastW = 0, g_lastH = 0;
static int     g_fullWindow  = 0; // 1 when overlay covers the full contentView (fullscreen mode)

// Last rendered pixel buffer — retained for pause snapshot (read by metal_video_get_last_frame_rgba).
static CVPixelBufferRef g_lastRenderedBuf = NULL;

static double mono_sec(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}

// ─────────────────────────────────────────────────────────────────────────────
// Render — called on main thread; takes ownership of one pending frame.
// ─────────────────────────────────────────────────────────────────────────────
static void metal_render_main(void) {
    if (!atomic_load(&g_active) || !g_layer) return;

    pthread_mutex_lock(&g_mu);
    CVPixelBufferRef buf = g_pendingBuf;
    g_pendingBuf = NULL;
    pthread_mutex_unlock(&g_mu);

    if (!buf) return;

    int w = (int)CVPixelBufferGetWidth(buf);
    int h = (int)CVPixelBufferGetHeight(buf);

    // Save a retained copy for the pause snapshot (one frame, cheap retain).
    CVPixelBufferRetain(buf);
    if (g_lastRenderedBuf) CVPixelBufferRelease(g_lastRenderedBuf);
    g_lastRenderedBuf = buf;

    // IOSurface path: CALayer compositor reads GPU memory directly — zero CPU copy.
    IOSurfaceRef surf = CVPixelBufferGetIOSurface(buf);
    if (surf) {
        [CATransaction begin];
        [CATransaction setDisableActions:YES];
        g_layer.contents = (__bridge id)surf;
        [CATransaction commit];
    }
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
        char msg[192];
        snprintf(msg, sizeof(msg),
                 "fps=%.1f  rendered=%lld  submitted=%lld  size=%dx%d",
                 (double)g_fpsFrames / elapsed,
                 (long long)g_renderCount, (long long)g_submitCount,
                 g_lastW, g_lastH);
        goMetalLog(msg, 0);
        g_fpsStart  = now;
        g_fpsFrames = 0;
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Public C API
// ─────────────────────────────────────────────────────────────────────────────

int metal_video_is_active(void) {
    return atomic_load(&g_active);
}

// Returns the instantaneous Metal render FPS measured over the last 2-second window.
// Returns 0 if not enough data yet.
double metal_video_last_fps(void) {
    if (!atomic_load(&g_active) || g_fpsStart == 0.0 || g_fpsFrames == 0) return 0.0;
    double elapsed = mono_sec() - g_fpsStart;
    if (elapsed < 0.5) return 0.0;
    return (double)g_fpsFrames / elapsed;
}

int metal_video_try_submit(CVImageBufferRef img) {
    if (!atomic_load(&g_active)) return 0;
    if (!CVPixelBufferGetIOSurface(img)) return 0;

    CVPixelBufferRetain(img);

    pthread_mutex_lock(&g_mu);
    CVPixelBufferRef old = g_pendingBuf;
    g_pendingBuf = (CVPixelBufferRef)img;
    g_submitCount++;
    pthread_mutex_unlock(&g_mu);

    if (old) CVPixelBufferRelease(old);

    if (atomic_exchange(&g_pending_dispatch, 1) == 0) {
        dispatch_async(dispatch_get_main_queue(), ^{
            atomic_store(&g_pending_dispatch, 0);
            metal_render_main();
        });
    }
    return 1;
}

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
        }

        g_fullWindow = (w <= 0 || h <= 0);
        CGFloat cvH = cv.bounds.size.height;
        NSRect frame = g_fullWindow ? cv.bounds
                                    : fyne_to_nsrect(cvH, x, y, w, h);

        NSView *ov = [[NSView alloc] initWithFrame:frame];
        ov.wantsLayer = YES;
        ov.layer.backgroundColor = CGColorGetConstantColor(kCGColorBlack);

        CALayer *vl = [CALayer layer];
        vl.frame = ov.bounds;
        vl.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
        vl.contentsGravity = kCAGravityResizeAspect;
        vl.backgroundColor = CGColorGetConstantColor(kCGColorBlack);
        vl.contentsScale   = NSScreen.mainScreen.backingScaleFactor;
        [ov.layer addSublayer:vl];
        [cv addSubview:ov];

        g_view  = ov;
        g_layer = vl;

        g_submitCount = 0; g_renderCount = 0;
        g_fpsFrames = 0;   g_fpsStart = 0;
        g_lastW = 0;       g_lastH = 0;
        pthread_mutex_lock(&g_mu);
        CVPixelBufferRef old = g_pendingBuf;
        g_pendingBuf = NULL;
        pthread_mutex_unlock(&g_mu);
        if (old) CVPixelBufferRelease(old);

        atomic_store(&g_active, 1);
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

void metal_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_active, 0);
    g_fullWindow = 0;
    dispatch_block_t blk = ^{
        if (g_view) {
            [g_view removeFromSuperview];
            g_view  = nil;
            g_layer = nil;
        }
        pthread_mutex_lock(&g_mu);
        CVPixelBufferRef old = g_pendingBuf;
        g_pendingBuf = NULL;
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
