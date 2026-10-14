// pyrowave_decode_darwin.m
// PyroWave decode for the macOS client: the intra-only wavelet codec a USBridge host
// (rust-shine, Punktfunk) sends when the session negotiated VIDEO_FORMAT_PYROWAVE.
//
// macOS has no native Vulkan. The inverse wavelet transform still runs as Vulkan compute
// (third_party/pyrowave, built by scripts/build_pyrowave_macos.sh) but over MoltenVK
// (Vulkan-over-Metal translation) instead of a real Vulkan driver -- see moltenvk_preload
// below for how that dependency is resolved at runtime with no link-time libvulkan/
// libMoltenVK dependency at all.
//
// This client only ever DECODES: rust-shine hosts encode on their own (non-Mac) GPU.
// That matters because PyroWave's ENCODER requires a Vulkan feature MoltenVK does not
// implement on Apple Silicon (VK_EXT_subgroup_size_control's fixed/required-size pipeline
// mode -- MoltenVK only supports the more common "varying subgroup size" mode). The
// DECODER's own device gate is deliberately more lenient (any subgroup size in [4,128] via
// the varying-size path is enough) and MoltenVK satisfies it -- validated directly against a
// real Apple M1 via MoltenVK 1.4.2: pyrowave_decoder_create succeeds at 1920x1080.
//
// Unlike the Linux decode path (pyrowave_decode_linux.c), there is no separate GPU-picking
// step here: MoltenVK exposes exactly one Vulkan "device" for the Mac's one Metal GPU, so
// pyrowave_create_default_device's own "first Vulkan 1.2+ device" pick is already correct.
//
// Decoded frames are handed to the Metal renderer as an IOSurface-backed CVPixelBuffer
// (8-bit 4:2:0 biplanar, BT.709 limited range) through the SAME metal_video_try_submit path
// VideoToolbox's own H.26x/AV1 decode uses (metal_video_impl_darwin.m) -- Core Animation
// already knows how to composite that pixel format directly, so the plain (bilinear) render
// path needs no new Metal kernel. A non-bilinear UpscaleMode (FSR1/bicubic/Lanczos) falls
// back to CA's own default scaling for PyroWave frames specifically, same as any other format
// metal_spike_render doesn't special-case -- see that function's own doc comment.
//
// A decode unit is one whole PyroWave packet: every frame is a keyframe, and a frame with a
// lost shard never reaches here (moonlight-common-c drops it).
//
// Decoding runs on its own thread, same reasoning as pyrowave_decode_linux.c's own doc
// comment: moonlight-common-c submits decode units on the thread draining the video socket,
// and a few milliseconds of decode there is enough for a 100+ Mb/s stream to overflow the
// socket buffer. The receive side only copies the unit into a one-frame slot, latest wins.

//go:build darwin && !ios && cgo

#import <Foundation/Foundation.h>
#import <CoreVideo/CoreVideo.h>
#include <vulkan/vulkan.h>
#include <pyrowave.h>
#include "pyrowave_framing.h"

#include <dlfcn.h>
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

// ── MoltenVK preload ────────────────────────────────────────────────────────────
//
// volk (PyroWave/Granite's Vulkan function loader) already dlopen's "libMoltenVK.dylib"
// directly as a macOS fallback with no Vulkan Loader in between (Granite/third_party/volk/
// volk.c) -- but a bare dlopen(name) only searches default locations (DYLD_LIBRARY_PATH,
// DYLD_FALLBACK_LIBRARY_PATH, which excludes an app bundle's own Frameworks/ dir). This
// preloads the SAME dylib by an explicit path first, once, so volk's later bare-name dlopen
// finds the already-loaded image (dyld de-dupes a dlopen of an already-resident image
// regardless of path spelling) instead of failing to find it at all.
//
// @rpath/libMoltenVK.dylib resolves via the main binary's own @executable_path/../Frameworks
// rpath (added by build_macos.sh's `install_name_tool -add_rpath`, same mechanism every other
// bundled Homebrew dylib relies on -- see that script's bundle_homebrew_dylibs). The absolute
// Homebrew paths are a dev-build fallback for running straight off `go build`, outside any
// .app bundle, where no rpath has been added.
static void moltenvk_preload(void) {
    static int tried = 0;
    if (tried) return;
    tried = 1;

    static const char *candidates[] = {
        "@rpath/libMoltenVK.dylib",
        "/opt/homebrew/lib/libMoltenVK.dylib",
        "/usr/local/lib/libMoltenVK.dylib",
    };
    for (size_t i = 0; i < sizeof(candidates) / sizeof(candidates[0]); i++) {
        if (dlopen(candidates[i], RTLD_NOW | RTLD_GLOBAL)) {
            char msg[160];
            snprintf(msg, sizeof(msg), "pyrowave: preloaded MoltenVK via %s", candidates[i]);
            goVTLog(msg);
            return;
        }
    }
    goVTLog((char*)"pyrowave: could not preload libMoltenVK.dylib -- PyroWave decode will be unavailable this session");
}

static pthread_mutex_t  g_pw_mu  = PTHREAD_MUTEX_INITIALIZER;
static pyrowave_device  g_pw_dev = NULL;
static pyrowave_decoder g_pw_dec = NULL;
static int              g_pw_w = 0, g_pw_h = 0;
// 0 = not tried, 1 = usable, -1 = no Vulkan (MoltenVK) device PyroWave can run on (don't retry).
static int              g_pw_dev_state = 0;
static uint8_t         *g_pw_planes = NULL; // Y, then Cb, then Cr, then interleaved CbCr
static size_t           g_pw_planes_cap = 0;
static uint64_t         g_pw_frames = 0;
static uint64_t         g_pw_decode_us = 0;

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
    char msg[160];
    snprintf(msg, sizeof(msg), "pyrowave: frame not decoded (%s, %d); %d so far", why, value, (int)g_pw_failures);
    goVTLog(msg);
}

static void pw_destroy_decoder(void) {
    if (g_pw_dec) { pyrowave_decoder_destroy(g_pw_dec); g_pw_dec = NULL; }
    g_pw_w = g_pw_h = 0;
}

static int pw_ensure_device(void) {
    if (g_pw_dev_state) return g_pw_dev_state > 0;
    moltenvk_preload();
    pyrowave_result r = pyrowave_create_default_device(&g_pw_dev);
    if (r != PYROWAVE_SUCCESS || !g_pw_dev) {
        g_pw_dev = NULL;
        g_pw_dev_state = -1;
        pw_log("pyrowave: no usable Vulkan device via MoltenVK (result %d) -- cannot decode this stream", (int)r, 0);
        return 0;
    }
    g_pw_dev_state = 1;
    goVTLog((char*)"pyrowave: MoltenVK device ready");
    return 1;
}

static int pw_ensure_decoder(int w, int h) {
    if (g_pw_dec && g_pw_w == w && g_pw_h == h) return 1;
    pw_destroy_decoder();
    if (!pw_ensure_device()) return 0;

    pyrowave_decoder_create_info info;
    memset(&info, 0, sizeof(info));
    info.device = g_pw_dev;
    info.width = w;
    info.height = h;
    info.chroma = PYROWAVE_CHROMA_SUBSAMPLING_420;
    info.fragment_path = pyrowave_decoder_device_prefers_fragment_path(g_pw_dev);
    pyrowave_result r = pyrowave_decoder_create(&info, &g_pw_dec);
    if (r != PYROWAVE_SUCCESS || !g_pw_dec) {
        g_pw_dec = NULL;
        pw_log("pyrowave: decoder create failed for %dx%d", w, h);
        return 0;
    }

    size_t luma = (size_t)w * (size_t)h;
    size_t chroma = (size_t)(w / 2) * (size_t)(h / 2);
    size_t need = luma + chroma * 4;
    if (g_pw_planes_cap < need) {
        free(g_pw_planes);
        g_pw_planes = malloc(need);
        g_pw_planes_cap = g_pw_planes ? need : 0;
        if (!g_pw_planes) { pw_destroy_decoder(); return 0; }
    }
    g_pw_w = w; g_pw_h = h;
    pw_log("pyrowave: Vulkan (MoltenVK) compute decoder ready %dx%d", w, h);
    return 1;
}

// pw_decode decodes one access unit. On success (return 1) *y points at a
// tightly packed w*h luma plane and *uv at an interleaved CbCr plane of (w/2)*2 bytes
// per row and h/2 rows, both valid until the next call; *full_range reports whether
// the stream signals full-range YCbCr. Returns 0 for a frame that cannot be decoded.
static int pw_decode_dense(const uint8_t *au, size_t len,
                     const uint8_t **y, const uint8_t **uv,
                     int *w_out, int *h_out, int *full_range) {
    // BitstreamSequenceHeader (pyrowave_common.hpp), little endian: width-1 in bits
    // 0..13, height-1 in 14..27, `extended` in bit 31 of the first word; chroma
    // resolution in bit 26 and the YCbCr range in bit 30 of the second.
    if (len < 8) { pw_fail("too short", (int)len); return 0; }
    uint32_t w0 = (uint32_t)au[0] | (uint32_t)au[1] << 8 | (uint32_t)au[2] << 16 | (uint32_t)au[3] << 24;
    uint32_t w1 = (uint32_t)au[4] | (uint32_t)au[5] << 8 | (uint32_t)au[6] << 16 | (uint32_t)au[7] << 24;
    if (!(w0 >> 31)) { pw_fail("no sequence header", (int)len); return 0; }
    int w = (int)(w0 & 0x3fff) + 1;
    int h = (int)((w0 >> 14) & 0x3fff) + 1;
    if ((w1 >> 26) & 1) { pw_fail("4:4:4 stream", (int)len); return 0; } // not negotiated here
    if ((w | h) & 1) { pw_fail("odd size", w); return 0; }

    int ok = 0;
    pthread_mutex_lock(&g_pw_mu);
    if (!pw_ensure_decoder(w, h)) goto out;

    // Anything still queued belongs to a frame that never completed.
    pyrowave_decoder_clear(g_pw_dec);
    if (pyrowave_decoder_push_packet(g_pw_dec, au, len) != PYROWAVE_SUCCESS) { pw_fail("packet rejected", (int)len); goto out; }
    if (!pyrowave_decoder_decode_is_ready(g_pw_dec, false)) { pw_fail("frame incomplete", (int)len); goto out; }

    int cw = w / 2, ch = h / 2;
    size_t luma = (size_t)w * (size_t)h;
    size_t chroma = (size_t)cw * (size_t)ch;
    uint8_t *py = g_pw_planes, *pcb = py + luma, *pcr = pcb + chroma, *puv = pcr + chroma;

    pyrowave_cpu_buffer buf;
    memset(&buf, 0, sizeof(buf));
    buf.format = PYROWAVE_CPU_BUFFER_FORMAT_YUV420P;
    buf.width = w;
    buf.height = h;
    buf.data[0] = py;  buf.row_stride_in_bytes[0] = (size_t)w;  buf.plane_size_in_bytes[0] = luma;
    buf.data[1] = pcb; buf.row_stride_in_bytes[1] = (size_t)cw; buf.plane_size_in_bytes[1] = chroma;
    buf.data[2] = pcr; buf.row_stride_in_bytes[2] = (size_t)cw; buf.plane_size_in_bytes[2] = chroma;
    struct timespec t0, t1;
    clock_gettime(CLOCK_MONOTONIC, &t0);
    if (pyrowave_decoder_decode_cpu_buffer_synchronous(g_pw_dec, &buf) != PYROWAVE_SUCCESS) { pw_fail("decode failed", (int)len); goto out; }
    clock_gettime(CLOCK_MONOTONIC, &t1);
    g_pw_decode_us += (uint64_t)((t1.tv_sec - t0.tv_sec) * 1000000 + (t1.tv_nsec - t0.tv_nsec) / 1000);

    if ((w1 >> 30) & 1) {
        for (size_t i = 0; i < chroma; i++) {
            puv[i * 2]     = pcb[i];
            puv[i * 2 + 1] = pcr[i];
        }
    } else {
        // Full-range stream (rust-shine's scaled encode): the renderer samples limited
        // range, so compress the levels here, fused with the interleave.
        static uint8_t ly[256], lc[256];
        static int luts = 0;
        if (!luts) { pw_range_luts(ly, lc); luts = 1; }
        for (size_t i = 0; i < luma; i++) py[i] = ly[py[i]];
        for (size_t i = 0; i < chroma; i++) {
            puv[i * 2]     = lc[pcb[i]];
            puv[i * 2 + 1] = lc[pcr[i]];
        }
    }
    *y = py;
    *uv = puv;
    *w_out = w;
    *h_out = h;
    *full_range = 0; // levels are limited range by now (compressed above if the stream was full)
    if (++g_pw_frames == 1) pw_log("pyrowave: first frame decoded (%dx%d)", w, h);
    if (g_pw_frames % 600 == 0) {
        pw_log("pyrowave: decode %d us per frame (average of the last %d)",
               (int)(g_pw_decode_us / 600), 600);
        g_pw_decode_us = 0;
    }
    ok = 1;
out:
    pthread_mutex_unlock(&g_pw_mu);
    return ok;
}

// ── CVPixelBuffer delivery ───────────────────────────────────────────────────────
//
// A pool (keyed by w,h) backs every decoded frame with a reused IOSurface instead of
// allocating one per frame -- same intent as VideoToolbox's own 24-buffer pool
// (moonlight_cgo_apple.go's vt_create_session), just driven by hand since we build the
// buffer ourselves rather than getting one from a VT session.
static CVPixelBufferPoolRef g_pw_pool = NULL;
static int g_pw_pool_w = 0, g_pw_pool_h = 0;

static CVPixelBufferPoolRef pw_ensure_pool(int w, int h) {
    if (g_pw_pool && g_pw_pool_w == w && g_pw_pool_h == h) return g_pw_pool;
    if (g_pw_pool) { CVPixelBufferPoolRelease(g_pw_pool); g_pw_pool = NULL; }

    NSDictionary *pixelAttrs = @{
        (id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange),
        (id)kCVPixelBufferWidthKey: @(w),
        (id)kCVPixelBufferHeightKey: @(h),
        (id)kCVPixelBufferIOSurfacePropertiesKey: @{},
    };
    NSDictionary *poolAttrs = @{ (id)kCVPixelBufferPoolMinimumBufferCountKey: @(8) };
    CVPixelBufferPoolRef pool = NULL;
    CVReturn cr = CVPixelBufferPoolCreate(kCFAllocatorDefault,
                                           (__bridge CFDictionaryRef)poolAttrs,
                                           (__bridge CFDictionaryRef)pixelAttrs,
                                           &pool);
    if (cr != kCVReturnSuccess || !pool) return NULL;
    g_pw_pool = pool;
    g_pw_pool_w = w; g_pw_pool_h = h;
    return g_pw_pool;
}

// pyrowave_darwin_deliver renders a decoded PyroWave frame (called on the decode thread).
// PyroWave has no software/RGBA fallback here: without the Metal renderer's CVPixelBuffer
// path there is nowhere to put the frame.
static void pyrowave_darwin_deliver(const uint8_t *y, const uint8_t *uv, int w, int h, int full_range) {
    static int warned_range = 0;
    if (full_range && !warned_range) {
        warned_range = 1;
        goVTLog((char*)"pyrowave: stream signals full-range YCbCr -- rendered as limited range");
    }

    CVPixelBufferPoolRef pool = pw_ensure_pool(w, h);
    if (!pool) { pw_fail("no pixel buffer pool", 0); goVTFrame(NULL, w, h, 0); return; }

    CVPixelBufferRef buf = NULL;
    if (CVPixelBufferPoolCreatePixelBuffer(kCFAllocatorDefault, pool, &buf) != kCVReturnSuccess || !buf) {
        pw_fail("pixel buffer pool exhausted", 0);
        goVTFrame(NULL, w, h, 0);
        return;
    }

    CVPixelBufferLockBaseAddress(buf, 0);
    uint8_t *dstY = (uint8_t *)CVPixelBufferGetBaseAddressOfPlane(buf, 0);
    size_t strideY = CVPixelBufferGetBytesPerRowOfPlane(buf, 0);
    uint8_t *dstUV = (uint8_t *)CVPixelBufferGetBaseAddressOfPlane(buf, 1);
    size_t strideUV = CVPixelBufferGetBytesPerRowOfPlane(buf, 1);
    int cw = w / 2, ch = h / 2;
    for (int row = 0; row < h; row++)
        memcpy(dstY + (size_t)row * strideY, y + (size_t)row * (size_t)w, (size_t)w);
    for (int row = 0; row < ch; row++)
        memcpy(dstUV + (size_t)row * strideUV, uv + (size_t)row * (size_t)cw * 2, (size_t)cw * 2);
    CVPixelBufferUnlockBaseAddress(buf, 0);

    // PyroWave hosts convert to BT.709 (moonlight_cgo_apple.go's platform_set_video_format
    // comment); always tagged limited range -- matches the Linux renderer's own
    // VK_SAMPLER_YCBCR_RANGE_ITU_NARROW choice (vk_video_impl_linux.c), full_range is only
    // ever a one-time warning, never a different matrix/range on either platform.
    CVBufferSetAttachment(buf, kCVImageBufferYCbCrMatrixKey, kCVImageBufferYCbCrMatrix_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);
    CVBufferSetAttachment(buf, kCVImageBufferColorPrimariesKey, kCVImageBufferColorPrimaries_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);
    CVBufferSetAttachment(buf, kCVImageBufferTransferFunctionKey, kCVImageBufferTransferFunction_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);

    metal_video_try_submit(buf);
    CVPixelBufferRelease(buf);

    // Reported whether or not it was shown, same convention as vt_callback's zero-copy
    // path and pyrowave_decode_linux.c's own pyrowave_linux_deliver.
    goVTFrame(NULL, w, h, 0);
}

// pw_decode takes the frame payload as it came off the wire (dense, record framed or
// length-prefixed -- see pyrowave_framing.h) and decodes it.
static int pw_decode(const uint8_t *au, size_t len,
                     const uint8_t **y, const uint8_t **uv,
                     int *w_out, int *h_out, int *full_range) {
    uint8_t *dense = (uint8_t *)malloc(len ? len : 1);
    if (!dense) return 0;
    size_t n = pw_unframe(au, len, dense);
    int ok = 0;
    if (n) ok = pw_decode_dense(dense, n, y, uv, w_out, h_out, full_range);
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

        const uint8_t *y = NULL, *uv = NULL;
        int w = 0, h = 0, full_range = 0;
        if (pw_decode(g_pwq_work, len, &y, &uv, &w, &h, &full_range))
            pyrowave_darwin_deliver(y, uv, w, h, full_range);
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

// pyrowave_darwin_teardown drops the decoder, its device and the pixel buffer pool. Called
// once the stream's threads are joined (platform_post_stop).
void pyrowave_darwin_teardown(void) {
    pw_stop_worker();
    pthread_mutex_lock(&g_pw_mu);
    pw_destroy_decoder();
    if (g_pw_dev) { pyrowave_device_destroy(g_pw_dev); g_pw_dev = NULL; }
    g_pw_dev_state = 0;
    g_pw_frames = 0;
    g_pw_decode_us = 0;
    g_pw_failures = 0;
    if (g_pw_pool) { CVPixelBufferPoolRelease(g_pw_pool); g_pw_pool = NULL; }
    g_pw_pool_w = g_pw_pool_h = 0;
    pthread_mutex_unlock(&g_pw_mu);
}
