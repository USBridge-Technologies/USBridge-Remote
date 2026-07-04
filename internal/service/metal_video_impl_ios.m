// metal_video_impl_ios.m — iOS Metal CALayer overlay via IOSurface (zero CPU copy).
// Same approach as macOS but uses UIView/UIWindow instead of NSView/NSWindow.
// CGO picks up .m files automatically; implementation kept here to avoid
// duplicate-symbol errors from inline C in multiple Go files.
#include <TargetConditionals.h>
#if TARGET_OS_IPHONE

#import <UIKit/UIKit.h>
#import <CoreVideo/CoreVideo.h>
#import <QuartzCore/QuartzCore.h>
#import <CoreGraphics/CoreGraphics.h>
#include <stdatomic.h>
#include <pthread.h>
#include <stdio.h>
#include <time.h>
#include <stdlib.h>

// Forward declaration — CGO generates this export from metal_video_ios.go.
extern void goMetalLog(char *msg, int level);

// ─────────────────────────────────────────────────────────────────────────────
// Global singleton state (one Metal overlay at a time).
// All UIView/CALayer access happens exclusively on the main thread.
// CVPixelBufferRef pending frame is protected by g_mu.
// ─────────────────────────────────────────────────────────────────────────────
static UIView    *g_view      = nil;
static UIView    *g_clip_view = nil;   // clips video to the touchpad widget bounds
static CALayer   *g_layer     = nil;
static float      g_keyboard_height_pt = 0.0f;  // current on-screen keyboard height in points

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
// Forward declaration — defined later in this file.
static UIWindow *_find_key_window(void);

@interface IOSMetalDisplayLinkTarget : NSObject
- (void)displayLinkFired:(CADisplayLink *)link;
- (void)keyboardWillChangeFrame:(NSNotification *)notification;
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
// Track keyboard frame so Go can query height each render frame.
- (void)keyboardWillChangeFrame:(NSNotification *)notification {
    CGRect endFrame = [notification.userInfo[UIKeyboardFrameEndUserInfoKey] CGRectValue];
    UIWindow *win = _find_key_window();
    float kbH = 0.0f;
    if (win) {
        float winH = (float)win.frame.size.height;
        float kbTop = (float)endFrame.origin.y;
        kbH = winH - kbTop;
        if (kbH < 0.0f || kbH > winH) kbH = 0.0f;
    }
    g_keyboard_height_pt = kbH;
}
@end
static IOSMetalDisplayLinkTarget *g_dl_target = nil;
static BOOL g_keyboard_observed = NO;

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

        if (g_clip_view) {
            [g_clip_view removeFromSuperview];
            g_clip_view = nil;
            g_view  = nil;
            g_layer = nil;
        }

        // w=0,h=0 signals full-window mode.
        CGRect frame = (w > 0 && h > 0) ? CGRectMake(x, y, w, h) : win.bounds;

        // Clip container: constrain video to the assigned widget area.
        // clipsToBounds = YES prevents zoomed/panned video from bleeding over
        // the toolbar, bottom button panel, or other UI elements.
        UIView *clip = [[UIView alloc] initWithFrame:frame];
        clip.clipsToBounds = YES;
        clip.backgroundColor = [UIColor blackColor];
        clip.userInteractionEnabled = NO;
        [win addSubview:clip];
        g_clip_view = clip;

        // Video view sits inside clip, initially at the same size.
        // metal_video_update_layout repositions it for zoom/pan.
        UIView *ov = [[UIView alloc] initWithFrame:clip.bounds];
        ov.userInteractionEnabled = NO;
        ov.layer.contentsGravity = kCAGravityResizeAspect;
        ov.layer.backgroundColor = [UIColor blackColor].CGColor;
        ov.layer.contentsScale   = [UIScreen mainScreen].scale;
        [clip addSubview:ov];

        g_view  = ov;
        g_layer = ov.layer;

        // Subscribe to keyboard frame changes (once per process lifetime).
        if (!g_keyboard_observed) {
            g_keyboard_observed = YES;
            [[NSNotificationCenter defaultCenter]
                addObserver:g_dl_target
                   selector:@selector(keyboardWillChangeFrame:)
                       name:UIKeyboardWillChangeFrameNotification
                     object:nil];
        }

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
        // Lock minimum rate to 60fps so iOS adaptive-refresh (ProMotion) doesn't
        // drop the display link below the video frame rate when content looks "static"
        // (no Fyne animations). Without this, 120Hz iPhones can throttle to 30fps or
        // lower during idle, causing slideshow even though the decoder is keeping up.
        if (@available(iOS 15.0, *)) {
            g_display_link.preferredFrameRateRange =
                CAFrameRateRangeMake(60, 120, 60);
        } else {
            g_display_link.preferredFramesPerSecond = 60;
        }
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

// ─────────────────────────────────────────────────────────────────────────────
// Cursor layer — arrow cursor overlaid on the video (above g_view).
// g_cursor_image is set once by metal_video_set_cursor_image and held for the
// lifetime of the process so the CGImage inside it remains valid.
// ─────────────────────────────────────────────────────────────────────────────
static CALayer  *g_cursor_layer = nil;
static UIImage  *g_cursor_image = nil;   // ARC-retained arrow bitmap
static CGFloat   g_cursor_pt_w  = 18.0;
static CGFloat   g_cursor_pt_h  = 24.0;

// Apply the stored arrow image (or a green-circle fallback) to layer.
// Must be called on the main thread.
static void _apply_cursor_image_to_layer(CALayer *layer) {
    if (!layer) return;
    if (g_cursor_image) {
        layer.contents       = (__bridge id)g_cursor_image.CGImage;
        layer.bounds         = CGRectMake(0, 0, g_cursor_pt_w, g_cursor_pt_h);
        layer.contentsScale  = 3.0;
        layer.cornerRadius   = 0;
        layer.backgroundColor = nil;
        layer.borderWidth    = 0;
    } else {
        // Fallback visible circle until Go uploads the arrow bitmap.
        layer.contents       = nil;
        layer.bounds         = CGRectMake(0, 0, 16, 16);
        layer.cornerRadius   = 8.0;
        layer.backgroundColor = [UIColor greenColor].CGColor;
        layer.borderWidth    = 2.0;
        layer.borderColor    = [UIColor whiteColor].CGColor;
    }
}

// metal_video_set_cursor_image — upload NRGBA pixel data for the cursor arrow.
// pixels must be valid for the duration of this call; the function copies immediately.
// w × h are pixel dimensions; the layer is displayed at w/3 × h/3 points.
void metal_video_set_cursor_image(uint8_t *pixels, int w, int h) {
    if (!pixels || w <= 0 || h <= 0) return;

    // Copy bytes into NSData before dispatching (Go GC may free pixels after return).
    NSData *data = [NSData dataWithBytes:pixels length:(NSUInteger)(w * h * 4)];
    int capturedW = w, capturedH = h;

    dispatch_block_t blk = ^{
        CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
        CGDataProviderRef dp = CGDataProviderCreateWithCFData((__bridge CFDataRef)data);
        CGImageRef cgImg = CGImageCreate(
            (size_t)capturedW, (size_t)capturedH,
            8, 32, (size_t)(capturedW * 4),
            cs,
            kCGBitmapByteOrderDefault | kCGImageAlphaLast,
            dp, NULL, false, kCGRenderingIntentDefault);
        CGColorSpaceRelease(cs);
        CGDataProviderRelease(dp);
        if (!cgImg) return;

        g_cursor_pt_w = capturedW / 3.0;
        g_cursor_pt_h = capturedH / 3.0;
        // Store as UIImage so ARC keeps the CGImage alive.
        g_cursor_image = [UIImage imageWithCGImage:cgImg
                                             scale:3.0
                                       orientation:UIImageOrientationUp];
        CGImageRelease(cgImg);

        // Update layer immediately if it already exists.
        if (g_cursor_layer) {
            [CATransaction begin];
            [CATransaction setDisableActions:YES];
            _apply_cursor_image_to_layer(g_cursor_layer);
            [CATransaction commit];
        }
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

void metal_video_update_cursor(float x, float y, int visible) {
    if (!g_view && !g_layer) return;
    float cx = x, cy = y;
    int   cv = visible;
    dispatch_block_t blk = ^{
        if (cv && !g_cursor_layer) {
            g_cursor_layer = [CALayer layer];
            // Anchor at top-left so position = cursor hotspot (tip of arrow).
            g_cursor_layer.anchorPoint = CGPointMake(0, 0);
            g_cursor_layer.masksToBounds = YES;
            // High zPosition ensures the cursor stays above the Metal video overlay
            // and any Fyne views (keyboard panel, popups) added later.
            g_cursor_layer.zPosition = 1000.0;
            // Apply stored image or fallback circle.
            _apply_cursor_image_to_layer(g_cursor_layer);
            UIWindow *win = _find_key_window();
            if (win) [win.layer addSublayer:g_cursor_layer];
        }
        if (g_cursor_layer) {
            g_cursor_layer.hidden = !cv;
            if (cv) {
                [CATransaction begin];
                [CATransaction setDisableActions:YES];
                UIWindow *win = _find_key_window();
                if (win && g_cursor_layer.superlayer != win.layer) {
                    [g_cursor_layer removeFromSuperlayer];
                    g_cursor_layer.zPosition = 1000.0;
                    [win.layer addSublayer:g_cursor_layer];
                }
                g_cursor_layer.position = CGPointMake(cx, cy);
                [CATransaction commit];
            }
        }
    };
    // dispatch_async: 1-frame latency is imperceptible; avoids blocking Fyne goroutine.
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// metal_video_update_layout — update both the clip container (widget bounds) and the
// video content view (zoomed/panned content rect) in one atomic CATransaction.
//   clip_*   : widget bounds in window coordinates (defines what area is visible)
//   content_*: video content rect in window coordinates (may be larger than clip when zoomed)
void metal_video_update_layout(float clip_x, float clip_y, float clip_w, float clip_h,
                                float content_x, float content_y, float content_w, float content_h) {
    if (!atomic_load(&g_active) || !g_clip_view) return;
    float cx=clip_x, cy=clip_y, cw=clip_w, ch=clip_h;
    float vx=content_x, vy=content_y, vw=content_w, vh=content_h;
    float kbH = g_keyboard_height_pt;
    dispatch_block_t blk = ^{
        if (!g_clip_view || !g_view) return;

        float fcx=cx, fcy=cy, fcw=cw, fch=ch;

        [CATransaction begin];
        [CATransaction setDisableActions:YES];
        g_clip_view.frame = CGRectMake(fcx, fcy, fcw, fch);
        // Position video view relative to the clip view's coordinate space.
        g_view.frame = CGRectMake(vx - fcx, vy - fcy, vw, vh);
        [CATransaction commit];
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// metal_video_update_frame — legacy API: clip = content (no zoom clipping).
// Kept for startup / fullscreen path where clip and content are the same rect.
void metal_video_update_frame(float x, float y, float w, float h) {
    metal_video_update_layout(x, y, w, h, x, y, w, h);
}

float metal_video_get_keyboard_height(void) {
    return g_keyboard_height_pt;
}

void metal_video_set_hidden(int hidden) {
    if (!atomic_load(&g_active)) return;
    dispatch_block_t blk = ^{
        // Hide the clip container — hides both clip and video views together.
        if (g_clip_view) g_clip_view.hidden = (hidden != 0);
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
        if (g_cursor_layer) {
            [g_cursor_layer removeFromSuperlayer];
            g_cursor_layer = nil;
        }
        // Keep g_cursor_image — it's reused on next overlay creation so the
        // arrow appears immediately without waiting for Go to re-upload it.
        if (g_clip_view) {
            // Removing the clip container also removes the video subview.
            [g_clip_view removeFromSuperview];
            g_clip_view = nil;
        }
        g_view  = nil;
        g_layer = nil;
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
void usbridge_syslog(const char* msg) { NSLog(@"[USBridge] %s", msg); }

// Disable the idle timer while streaming so iOS keeps the CPU in high-performance
// mode. Without this, the decoder thread gets throttled when the user is idle,
// causing the video to drop to slideshow FPS.
void set_streaming_active(int active) {
#if TARGET_OS_IPHONE
    dispatch_block_t blk = ^{
        [UIApplication sharedApplication].idleTimerDisabled = (active != 0) ? YES : NO;
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
#endif
}
