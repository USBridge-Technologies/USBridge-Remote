// metal_video_impl_ios.m — iOS Metal CALayer overlay via IOSurface (zero CPU copy).
// Same approach as macOS but uses UIView/UIWindow instead of NSView/NSWindow.
// CGO picks up .m files automatically; implementation kept here to avoid
// duplicate-symbol errors from inline C in multiple Go files.
#include <TargetConditionals.h>
#if TARGET_OS_IPHONE

#import <UIKit/UIKit.h>
#import <CoreVideo/CoreVideo.h>
#import <QuartzCore/QuartzCore.h>
#include <stdatomic.h>
#include <pthread.h>
#include <stdio.h>
#include <time.h>

// Forward declaration — CGO generates this export from metal_video_ios.go.
extern void goMetalLog(char *msg, int level);

// ─────────────────────────────────────────────────────────────────────────────
// Global singleton state (one Metal overlay at a time).
// All UIView/CALayer access happens exclusively on the main thread.
// CVPixelBufferRef pending frame is protected by g_mu.
// ─────────────────────────────────────────────────────────────────────────────
static UIView    *g_view  = nil;
static CALayer   *g_layer = nil;

static volatile atomic_int g_active = 0;
static CADisplayLink *g_display_link = nil;

static pthread_mutex_t g_mu = PTHREAD_MUTEX_INITIALIZER;
static CVPixelBufferRef g_pendingBuf = NULL;

static int64_t g_submitCount   = 0;
static int64_t g_renderCount   = 0;
static int64_t g_fpsFrames     = 0;
static double  g_fpsStart      = 0.0;
static double  g_lastKnownFps  = 0.0;
static int     g_lastW = 0, g_lastH = 0;

static double mono_sec(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}

// ─────────────────────────────────────────────────────────────────────────────
// Render — called on main thread; OWNS buf and always releases it.
// ─────────────────────────────────────────────────────────────────────────────
static void metal_render_main_with_buf(CVPixelBufferRef buf) {
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

    CVPixelBufferRelease(buf);

    int64_t n = ++g_renderCount;
    if (n == 1) {
        g_lastW = w; g_lastH = h;
        g_fpsStart  = mono_sec();
        g_fpsFrames = 0;
        char msg[128];
        snprintf(msg, sizeof(msg),
                 "iOS Metal: first frame %dx%d iosurface=%s",
                 w, h, surf ? "yes" : "no (fallback)");
        NSLog(@"[USBridge] %s", msg);
        goMetalLog(msg, 0);
    }

    g_lastW = w; g_lastH = h;
    g_fpsFrames++;
    double now     = mono_sec();
    double elapsed = now - g_fpsStart;
    if (n == 30 && g_fpsStart > 0.0) {
        char msg[192];
        snprintf(msg, sizeof(msg),
                 "iOS Metal: first 30 frames fps=%.1f submitted=%lld %dx%d",
                 (double)g_fpsFrames / elapsed,
                 (long long)g_submitCount, g_lastW, g_lastH);
        goMetalLog(msg, 0);
    }
    if (elapsed >= 2.0 && g_fpsFrames > 0) {
        g_lastKnownFps = (double)g_fpsFrames / elapsed;
        char msg[192];
        snprintf(msg, sizeof(msg),
                 "iOS Metal: fps=%.1f rendered=%lld submitted=%lld %dx%d",
                 g_lastKnownFps,
                 (long long)g_renderCount, (long long)g_submitCount,
                 g_lastW, g_lastH);
        goMetalLog(msg, 0);
        g_fpsStart  = now;
        g_fpsFrames = 0;
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// CADisplayLink target — fires on the main thread at the display refresh rate.
// ─────────────────────────────────────────────────────────────────────────────
@interface IOSMetalDisplayLinkTarget : NSObject
- (void)displayLinkFired:(CADisplayLink *)link;
@end
@implementation IOSMetalDisplayLinkTarget
- (void)displayLinkFired:(CADisplayLink __unused *)link {
    if (!atomic_load(&g_active)) return;
    pthread_mutex_lock(&g_mu);
    CVPixelBufferRef buf = g_pendingBuf;
    g_pendingBuf = NULL;
    pthread_mutex_unlock(&g_mu);
    if (!buf) return;
    metal_render_main_with_buf(buf);
}
@end
static IOSMetalDisplayLinkTarget *g_dl_target = nil;

// ─────────────────────────────────────────────────────────────────────────────
// Public C API
// ─────────────────────────────────────────────────────────────────────────────

int metal_video_is_active(void) {
    return atomic_load(&g_active);
}

double metal_video_last_fps(void) {
    if (!atomic_load(&g_active)) return 0.0;
    if (g_fpsStart == 0.0) return 0.0;
    double elapsed = mono_sec() - g_fpsStart;
    if (g_fpsFrames == 0 || elapsed < 0.5) return g_lastKnownFps;
    return (double)g_fpsFrames / elapsed;
}

static _Atomic int g_submit_call_count = 0;
static int g_submit_logged = 0;
int metal_video_try_submit(CVImageBufferRef img) {
    int call_n = atomic_fetch_add(&g_submit_call_count, 1);
    if (call_n == 0) {
        NSLog(@"[USBridge] metal_video_try_submit: first call (active=%d has_iosurface=%d)",
              atomic_load(&g_active), CVPixelBufferGetIOSurface(img) != NULL ? 1 : 0);
    }
    if (!atomic_load(&g_active)) return 0;
    if (!CVPixelBufferGetIOSurface(img)) {
        if (!g_submit_logged) {
            g_submit_logged = 1;
            NSLog(@"[USBridge] metal_video_try_submit: no IOSurface — CPU fallback");
        }
        return 0;
    }
    if (!g_submit_logged) {
        g_submit_logged = 1;
        NSLog(@"[USBridge] metal_video_try_submit: first frame with IOSurface OK");
    }

    CVPixelBufferRetain(img);

    pthread_mutex_lock(&g_mu);
    CVPixelBufferRef old = g_pendingBuf;
    g_pendingBuf = (CVPixelBufferRef)img;
    g_submitCount++;
    pthread_mutex_unlock(&g_mu);

    if (old) CVPixelBufferRelease(old);
    return 1;
}

// Find the current key UIWindow — works on iOS 13+ via connectedScenes,
// falls back to keyWindow for older runtimes.
static UIWindow *_find_key_window(void) {
    if (@available(iOS 13.0, *)) {
        for (UIScene *scene in [UIApplication sharedApplication].connectedScenes) {
            if (![scene isKindOfClass:[UIWindowScene class]]) continue;
            UIWindowScene *ws = (UIWindowScene *)scene;
            for (UIWindow *win in ws.windows) {
                if (win.isKeyWindow) return win;
            }
        }
    }
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
    return [UIApplication sharedApplication].keyWindow;
#pragma clang diagnostic pop
}

// metal_video_create — creates the CALayer overlay on the key UIWindow.
// Pass w=0, h=0 to cover the entire window (fullscreen mode).
// The first argument (nsWinPtr) is unused on iOS; the window is found internally.
int metal_video_create(uintptr_t unused, float x, float y, float w, float h) {
    (void)unused;
    __block int ok = 0;
    dispatch_block_t blk = ^{
        UIWindow *win = _find_key_window();
        if (!win) {
            NSLog(@"[USBridge] metal_video_create: no key window found!");
            goMetalLog((char*)"metal_video_create: no key window", 2);
            return;
        }

        if (g_view) {
            [g_view removeFromSuperview];
            g_view  = nil;
            g_layer = nil;
        }

        // w=0,h=0 signals full-window mode.
        CGRect frame = (w > 0 && h > 0) ? CGRectMake(x, y, w, h) : win.bounds;

        // Overlay view: pass-through for touches so Fyne keeps receiving them.
        // Use the view's own CALayer directly — no sublayer, no autoresizing needed
        // (UIView.layer always fills the view's bounds automatically).
        UIView *ov = [[UIView alloc] initWithFrame:frame];
        ov.userInteractionEnabled = NO;
        ov.layer.contentsGravity = kCAGravityResizeAspect;
        ov.layer.backgroundColor = [UIColor blackColor].CGColor;
        ov.layer.contentsScale   = [UIScreen mainScreen].scale;
        [win addSubview:ov];

        g_view  = ov;
        g_layer = ov.layer;

        g_submitCount = 0; g_renderCount = 0;
        g_fpsFrames   = 0; g_fpsStart = 0.0; g_lastKnownFps = 0.0;
        g_lastW = 0;       g_lastH = 0;

        pthread_mutex_lock(&g_mu);
        CVPixelBufferRef old = g_pendingBuf;
        g_pendingBuf = NULL;
        pthread_mutex_unlock(&g_mu);
        if (old) CVPixelBufferRelease(old);

        atomic_store(&g_active, 1);

        if (!g_dl_target) g_dl_target = [IOSMetalDisplayLinkTarget new];
        g_display_link = [CADisplayLink displayLinkWithTarget:g_dl_target
                                                     selector:@selector(displayLinkFired:)];
        [g_display_link addToRunLoop:[NSRunLoop mainRunLoop] forMode:NSRunLoopCommonModes];

        ok = 1;
        char msg[192];
        if (w <= 0 || h <= 0) {
            snprintf(msg, sizeof(msg),
                     "iOS Metal: overlay created full-window %.0fx%.0f",
                     (double)win.bounds.size.width, (double)win.bounds.size.height);
        } else {
            snprintf(msg, sizeof(msg),
                     "iOS Metal: overlay created %.0fx%.0f at (%.0f,%.0f)",
                     (double)w, (double)h, (double)x, (double)y);
        }
        NSLog(@"[USBridge] %s (win=%@ subviews=%lu)", msg, win, (unsigned long)win.subviews.count);
        goMetalLog(msg, 0);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_sync(dispatch_get_main_queue(), blk);
    return ok;
}

void metal_video_update_frame(float x, float y, float w, float h) {
    if (!atomic_load(&g_active) || !g_view) return;
    dispatch_block_t blk = ^{
        if (g_view) g_view.frame = CGRectMake(x, y, w, h);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
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

    if (g_display_link) {
        [g_display_link invalidate];
        g_display_link = nil;
    }

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

        char msg[128];
        snprintf(msg, sizeof(msg),
                 "iOS Metal: overlay destroyed rendered=%lld submitted=%lld",
                 (long long)g_renderCount, (long long)g_submitCount);
        goMetalLog(msg, 0);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_sync(dispatch_get_main_queue(), blk);
}

#endif // TARGET_OS_IPHONE
