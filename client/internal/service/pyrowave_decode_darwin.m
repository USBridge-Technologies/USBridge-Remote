// pyrowave_decode_darwin.m
// PyroWave decode for the macOS client: the intra-only wavelet codec a USBridge host
// (rust-shine on Linux, Windows or macOS; Punktfunk) sends when the session negotiated
// VIDEO_FORMAT_PYROWAVE.
//
// Native Metal, no Vulkan: third_party/pyrowave/metal is PyroWave's own decoder ported from
// Granite/Vulkan to the Metal API (the same compute shaders, translated to MSL; the same packet
// parsing), built by scripts/build_pyrowave_macos.sh. It writes the decoded frame straight into
// an IOSurface-backed CVPixelBuffer -- Y into its luma plane, Cb/Cr interleaved into its chroma
// plane, all on the GPU -- and that buffer goes to the renderer through the SAME
// metal_video_try_submit path VideoToolbox's H.26x/AV1 frames use (metal_video_impl_darwin.m).
// No pixel is read back or copied by the CPU.
//
// The buffer's pixel format and attachments say what the stream is (the sequence header's
// colour bits): '420f' or '420v' for full or limited range, BT.709, left or centred chroma --
// Core Animation converts accordingly. A non-bilinear UpscaleMode (FSR1/bicubic/Lanczos) falls
// back to CA's own scaling for these frames, same as any format metal_spike_render doesn't
// special-case -- see that function's own doc comment.
//
// A decode unit is one whole PyroWave packet: every frame is a keyframe, and a frame with a
// lost shard never reaches here (moonlight-common-c drops it).
//
// Decoding runs on its own thread, same reasoning as pyrowave_decode_linux.c's own doc
// comment: moonlight-common-c submits decode units on the thread draining the video socket,
// and a millisecond of decode there is enough for a 100+ Mb/s stream to overflow the socket
// buffer. The receive side only copies the unit into a one-frame slot, latest wins.

//go:build darwin && !ios && cgo

#import <Foundation/Foundation.h>
#import <CoreVideo/CoreVideo.h>
#include <pyrowave_metal.h>
#include "pyrowave_framing.h"

#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

// Go callbacks (defined in moonlight_cgo_wrapper.go, same package -- cgo links every
// //export'd Go function into every translation unit of the package, see
// metal_video_impl_darwin.m's goMetalLog forward declaration for the same pattern).
extern void goVTLog(char *msg);
extern void goVTFrame(uint8_t *rgba, int width, int height, int stride);

// Metal overlay fast path (metal_video_impl_darwin.m).
extern int metal_video_try_submit(CVImageBufferRef img);

static pthread_mutex_t g_pw_mu = PTHREAD_MUTEX_INITIALIZER;
static pwm_decoder    *g_pw_dec = NULL;
static int             g_pw_w = 0, g_pw_h = 0;
static uint64_t        g_pw_frames = 0;
static uint64_t        g_pw_decode_us = 0;
static uint64_t        g_pw_gpu_us = 0;

static void pw_log(const char *fmt, int a, int b) {
    char msg[160];
    snprintf(msg, sizeof(msg), fmt, a, b);
    goVTLog(msg);
}

// pw_fail logs why a unit was not decoded: the first few, then every 300th.
static uint64_t g_pw_failures = 0;
static void pw_fail(const char *why, int value) {
    g_pw_failures++;
    if (g_pw_failures > 5 && g_pw_failures % 300 != 0) return;
    char msg[320];
    snprintf(msg, sizeof(msg), "pyrowave: frame not decoded (%s, %d); %d so far", why, value, (int)g_pw_failures);
    goVTLog(msg);
}

// pyrowave_darwin_supported reports whether the Metal decoder runs on this Mac (32-wide SIMD
// groups: every Apple GPU), checked once.
int pyrowave_darwin_supported(void) {
    static dispatch_once_t once;
    static int supported = 0;
    dispatch_once(&once, ^{
        supported = pwm_supported(NULL);
        if (!supported) {
            char msg[320];
            snprintf(msg, sizeof(msg), "pyrowave: Metal decoder unavailable on this Mac: %s", pwm_last_error());
            goVTLog(msg);
        }
    });
    return supported;
}

static void pw_destroy_decoder(void) {
    if (g_pw_dec) { pwm_decoder_destroy(g_pw_dec); g_pw_dec = NULL; }
    g_pw_w = g_pw_h = 0;
}

static int pw_ensure_decoder(int w, int h) {
    if (g_pw_dec && g_pw_w == w && g_pw_h == h) return 1;
    pw_destroy_decoder();
    if (pwm_decoder_create(NULL, w, h, &g_pw_dec) != PWM_OK) {
        g_pw_dec = NULL;
        char msg[320];
        snprintf(msg, sizeof(msg), "pyrowave: Metal decoder create failed for %dx%d: %s", w, h, pwm_last_error());
        goVTLog(msg);
        return 0;
    }
    g_pw_w = w; g_pw_h = h;
    pw_log("pyrowave: Metal compute decoder ready %dx%d", w, h);
    return 1;
}

// ── CVPixelBuffer pool ───────────────────────────────────────────────────────────
//
// Every decoded frame lands in a reused IOSurface instead of one allocated per frame -- same
// intent as VideoToolbox's own 24-buffer pool (moonlight_cgo_apple.go's vt_create_session),
// just driven by hand since we build the buffer ourselves. A buffer the renderer still holds is
// not handed out again, so the decoder never writes into the frame on screen.
static CVPixelBufferPoolRef g_pw_pool = NULL;
static int g_pw_pool_w = 0, g_pw_pool_h = 0, g_pw_pool_full = -1;

static CVPixelBufferPoolRef pw_ensure_pool(int w, int h, int full_range) {
    if (g_pw_pool && g_pw_pool_w == w && g_pw_pool_h == h && g_pw_pool_full == full_range) return g_pw_pool;
    if (g_pw_pool) { CVPixelBufferPoolRelease(g_pw_pool); g_pw_pool = NULL; }

    NSDictionary *pixelAttrs = @{
        (id)kCVPixelBufferPixelFormatTypeKey: @(full_range ? kCVPixelFormatType_420YpCbCr8BiPlanarFullRange
                                                           : kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange),
        (id)kCVPixelBufferWidthKey: @(w),
        (id)kCVPixelBufferHeightKey: @(h),
        (id)kCVPixelBufferIOSurfacePropertiesKey: @{},
        // The decoder writes the planes as Metal textures.
        (id)kCVPixelBufferMetalCompatibilityKey: @YES,
    };
    NSDictionary *poolAttrs = @{ (id)kCVPixelBufferPoolMinimumBufferCountKey: @(8) };
    CVPixelBufferPoolRef pool = NULL;
    CVReturn cr = CVPixelBufferPoolCreate(kCFAllocatorDefault,
                                           (__bridge CFDictionaryRef)poolAttrs,
                                           (__bridge CFDictionaryRef)pixelAttrs,
                                           &pool);
    if (cr != kCVReturnSuccess || !pool) return NULL;
    g_pw_pool = pool;
    g_pw_pool_w = w; g_pw_pool_h = h; g_pw_pool_full = full_range;
    return g_pw_pool;
}

// pw_decode_dense decodes one dense access unit into a pixel buffer from the pool and hands it
// to the renderer. Returns 0 for a frame that cannot be decoded.
static int pw_decode_dense(const uint8_t *au, size_t len) {
    // BitstreamSequenceHeader (pyrowave_common.hpp), little endian: width-1 in bits 0..13,
    // height-1 in 14..27, `extended` in bit 31 of the first word; in the second, chroma
    // resolution in bit 26, BT.2020 primaries / PQ / BT.2020 matrix in 27..29, limited range in
    // bit 30 and left-sited chroma in bit 31.
    if (len < 8) { pw_fail("too short", (int)len); return 0; }
    uint32_t w0 = (uint32_t)au[0] | (uint32_t)au[1] << 8 | (uint32_t)au[2] << 16 | (uint32_t)au[3] << 24;
    uint32_t w1 = (uint32_t)au[4] | (uint32_t)au[5] << 8 | (uint32_t)au[6] << 16 | (uint32_t)au[7] << 24;
    if (!(w0 >> 31)) { pw_fail("no sequence header", (int)len); return 0; }
    int w = (int)(w0 & 0x3fff) + 1;
    int h = (int)((w0 >> 14) & 0x3fff) + 1;
    if ((w1 >> 26) & 1) { pw_fail("4:4:4 stream", (int)len); return 0; }   // not negotiated here
    if ((w1 >> 27) & 7) { pw_fail("HDR stream", (int)len); return 0; }     // not negotiated here
    if ((w | h) & 1) { pw_fail("odd size", w); return 0; }
    int full_range = !((w1 >> 30) & 1);
    int left_sited = (int)((w1 >> 31) & 1);

    int ok = 0;
    CVPixelBufferRef buf = NULL;
    pthread_mutex_lock(&g_pw_mu);
    if (!pw_ensure_decoder(w, h)) goto out;

    CVPixelBufferPoolRef pool = pw_ensure_pool(w, h, full_range);
    if (!pool) { pw_fail("no pixel buffer pool", 0); goto out; }
    if (CVPixelBufferPoolCreatePixelBuffer(kCFAllocatorDefault, pool, &buf) != kCVReturnSuccess || !buf) {
        buf = NULL;
        pw_fail("pixel buffer pool exhausted", 0);
        goto out;
    }
    IOSurfaceRef surface = CVPixelBufferGetIOSurface(buf);
    if (!surface) { pw_fail("pixel buffer without an IOSurface", 0); goto out; }

    struct timespec t0, t1;
    clock_gettime(CLOCK_MONOTONIC, &t0);
    int r = pwm_decoder_decode_nv12(g_pw_dec, au, len, surface);
    clock_gettime(CLOCK_MONOTONIC, &t1);
    if (r != PWM_OK) {
        char why[256];
        snprintf(why, sizeof(why), "decode: %s", pwm_last_error());
        pw_fail(why, r);
        goto out;
    }
    g_pw_decode_us += (uint64_t)((t1.tv_sec - t0.tv_sec) * 1000000 + (t1.tv_nsec - t0.tv_nsec) / 1000);
    g_pw_gpu_us += pwm_decoder_last_gpu_us(g_pw_dec);

    // What the stream is (the sequence header), so Core Animation converts it correctly. Hosts
    // send BT.709 (the header has no other SDR matrix).
    CVBufferSetAttachment(buf, kCVImageBufferYCbCrMatrixKey, kCVImageBufferYCbCrMatrix_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);
    CVBufferSetAttachment(buf, kCVImageBufferColorPrimariesKey, kCVImageBufferColorPrimaries_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);
    CVBufferSetAttachment(buf, kCVImageBufferTransferFunctionKey, kCVImageBufferTransferFunction_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);
    CFStringRef siting = left_sited ? kCVImageBufferChromaLocation_Left : kCVImageBufferChromaLocation_Center;
    CVBufferSetAttachment(buf, kCVImageBufferChromaLocationTopFieldKey, siting, kCVAttachmentMode_ShouldPropagate);
    CVBufferSetAttachment(buf, kCVImageBufferChromaLocationBottomFieldKey, siting, kCVAttachmentMode_ShouldPropagate);

    if (++g_pw_frames == 1) {
        char msg[160];
        snprintf(msg, sizeof(msg), "pyrowave: first frame decoded (%dx%d, %s range, %s chroma)", w, h,
                 full_range ? "full" : "limited", left_sited ? "left-sited" : "centred");
        goVTLog(msg);
    }
    if (g_pw_frames % 600 == 0) {
        pw_log("pyrowave: decode %d us per frame, %d us of it on the GPU (average of the last 600)",
               (int)(g_pw_decode_us / 600), (int)(g_pw_gpu_us / 600));
        g_pw_decode_us = 0;
        g_pw_gpu_us = 0;
    }
    ok = 1;
out:
    pthread_mutex_unlock(&g_pw_mu);
    if (ok) metal_video_try_submit(buf);
    if (buf) CVPixelBufferRelease(buf);
    // Reported whether or not it was shown, same convention as vt_callback's zero-copy path and
    // pyrowave_decode_linux.c's own pyrowave_linux_deliver.
    if (ok) goVTFrame(NULL, w, h, 0);
    return ok;
}

// pw_decode takes the frame payload as it came off the wire (dense, record framed or
// length-prefixed -- see pyrowave_framing.h) and decodes it.
static int pw_decode(const uint8_t *au, size_t len) {
    uint8_t *dense = (uint8_t *)malloc(len ? len : 1);
    if (!dense) return 0;
    size_t n = pw_unframe(au, len, dense);
    int ok = 0;
    if (n) ok = pw_decode_dense(dense, n);
    else pw_fail("malformed framing", (int)len);
    free(dense);
    return ok;
}

// ── decode thread ──────────────────────────────────────────────────────────────

static pthread_mutex_t g_pwq_mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t  g_pwq_cv = PTHREAD_COND_INITIALIZER;
static pthread_t       g_pwq_thread;
static int             g_pwq_running = 0, g_pwq_stop = 0, g_pwq_ready = 0;
static uint8_t        *g_pwq_slot = NULL, *g_pwq_work = NULL; // swapped under g_pwq_mu
static size_t          g_pwq_slot_len = 0, g_pwq_slot_cap = 0, g_pwq_work_cap = 0;
static uint64_t        g_pwq_skipped = 0;

static void *pw_worker(void *unused) {
    (void)unused;
    for (;;) {
        pthread_mutex_lock(&g_pwq_mu);
        while (!g_pwq_ready && !g_pwq_stop) pthread_cond_wait(&g_pwq_cv, &g_pwq_mu);
        if (g_pwq_stop) { pthread_mutex_unlock(&g_pwq_mu); break; }
        uint8_t *t = g_pwq_work; g_pwq_work = g_pwq_slot; g_pwq_slot = t;
        size_t tc = g_pwq_work_cap; g_pwq_work_cap = g_pwq_slot_cap; g_pwq_slot_cap = tc;
        size_t len = g_pwq_slot_len;
        g_pwq_ready = 0;
        pthread_mutex_unlock(&g_pwq_mu);

        pw_decode(g_pwq_work, len);
    }
    return NULL;
}

// pyrowave_darwin_slot_begin locks the decode thread's one-unit slot and returns room for
// a unit of `total` bytes (NULL when it cannot be allocated, slot unlocked). The caller
// copies the unit in and calls pyrowave_darwin_slot_commit, which replaces any unit the
// decode thread has not started on yet.
uint8_t *pyrowave_darwin_slot_begin(size_t total) {
    pthread_mutex_lock(&g_pwq_mu);
    if (!g_pwq_running) {
        g_pwq_stop = 0;
        if (pthread_create(&g_pwq_thread, NULL, pw_worker, NULL) != 0) {
            pthread_mutex_unlock(&g_pwq_mu);
            return NULL;
        }
        g_pwq_running = 1;
    }
    if (g_pwq_slot_cap < total) {
        free(g_pwq_slot);
        g_pwq_slot = malloc(total);
        g_pwq_slot_cap = g_pwq_slot ? total : 0;
        if (!g_pwq_slot) { pthread_mutex_unlock(&g_pwq_mu); return NULL; }
    }
    // A unit the decode thread never picked up is about to be overwritten.
    if (g_pwq_ready) { g_pwq_skipped++; g_pwq_ready = 0; }
    return g_pwq_slot;
}

void pyrowave_darwin_slot_commit(size_t total) {
    g_pwq_slot_len = total;
    g_pwq_ready = 1;
    pthread_cond_signal(&g_pwq_cv);
    pthread_mutex_unlock(&g_pwq_mu);
}

static void pw_stop_worker(void) {
    pthread_mutex_lock(&g_pwq_mu);
    int running = g_pwq_running;
    g_pwq_stop = 1;
    pthread_cond_signal(&g_pwq_cv);
    pthread_mutex_unlock(&g_pwq_mu);
    if (running) pthread_join(g_pwq_thread, NULL);
    pthread_mutex_lock(&g_pwq_mu);
    if (g_pwq_skipped) pw_log("pyrowave: %d frames skipped while the decoder was busy", (int)g_pwq_skipped, 0);
    g_pwq_running = 0;
    g_pwq_ready = 0;
    g_pwq_skipped = 0;
    pthread_mutex_unlock(&g_pwq_mu);
}

// pyrowave_darwin_frames counts the frames decoded since the stream started.
uint64_t pyrowave_darwin_frames(void) {
    pthread_mutex_lock(&g_pw_mu);
    uint64_t n = g_pw_frames;
    pthread_mutex_unlock(&g_pw_mu);
    return n;
}

// pyrowave_darwin_teardown drops the decoder and the pixel buffer pool. Called once the
// stream's threads are joined (platform_post_stop).
void pyrowave_darwin_teardown(void) {
    pw_stop_worker();
    pthread_mutex_lock(&g_pw_mu);
    pw_destroy_decoder();
    g_pw_frames = 0;
    g_pw_decode_us = 0;
    g_pw_gpu_us = 0;
    g_pw_failures = 0;
    if (g_pw_pool) { CVPixelBufferPoolRelease(g_pw_pool); g_pw_pool = NULL; }
    g_pw_pool_w = g_pw_pool_h = 0;
    g_pw_pool_full = -1;
    pthread_mutex_unlock(&g_pw_mu);
}
