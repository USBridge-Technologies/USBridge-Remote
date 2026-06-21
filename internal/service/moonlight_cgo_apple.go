//go:build (darwin || ios) && !android && cgo

package service

/*
#cgo pkg-config: opus openssl
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build -L${SRCDIR}/../../moonlight-common-c/build/enet -lmoonlight-common-c -lenet
#cgo LDFLAGS: -framework VideoToolbox -framework CoreMedia -framework CoreFoundation -framework CoreVideo -framework AudioToolbox -framework CoreAudio

#include <VideoToolbox/VideoToolbox.h>
#include <CoreMedia/CoreMedia.h>
#include <CoreVideo/CoreVideo.h>
#include <AudioToolbox/AudioToolbox.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

// Go callbacks (defined in moonlight_cgo_wrapper.go, same package).
extern void goMoonlightStage(int stage, int result, int errCode);
extern void goMoonlightConnected(void);
extern void goMoonlightTerminated(int errCode);
extern void goVTLog(char *msg);
extern void goVTFrame(uint8_t *rgba, int width, int height, int stride);

// Metal overlay fast path (defined in metal_video_darwin.go, macOS only).
#if TARGET_OS_MAC && !TARGET_OS_IPHONE
extern int metal_video_is_active(void);
extern int metal_video_try_submit(CVImageBufferRef img);
#endif

#include "moonlight_cgo_shared.h"

// Shared monotonic clock — used by both audio and video subsystems.
static double mono_sec(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}

// ═══════════════════════════════════════════════════════════════════════════════
// CoreAudio AudioQueue — platform audio implementation
// ═══════════════════════════════════════════════════════════════════════════════

#define CA_NUM_BUFFERS 16

static AudioQueueRef        g_ca_queue    = NULL;
static AudioQueueBufferRef  g_ca_free[CA_NUM_BUFFERS];
static int                  g_ca_free_cnt = 0;
static int                  g_ca_started  = 0;
static pthread_mutex_t      g_ca_mu       = PTHREAD_MUTEX_INITIALIZER;

static void ca_callback(void *userData, AudioQueueRef aq, AudioQueueBufferRef buf) {
    (void)userData; (void)aq;
    pthread_mutex_lock(&g_ca_mu);
    if (g_ca_free_cnt < CA_NUM_BUFFERS)
        g_ca_free[g_ca_free_cnt++] = buf;
    pthread_mutex_unlock(&g_ca_mu);
}

static uint64_t g_ca_drop_count    = 0; // frames dropped due to empty free pool
static uint64_t g_ca_restart_count = 0; // queue restarts (stall/interruption)
static uint64_t g_ca_frame_count   = 0; // frames decoded successfully
static double   g_ca_stats_start   = 0.0;
static int      g_ca_was_dropping  = 0; // set while free pool is empty (drop burst)

// Log the macOS default audio output device name.
static void ca_log_device(void) {
    AudioObjectPropertyAddress addr = {
        kAudioHardwarePropertyDefaultOutputDevice,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain
    };
    AudioDeviceID devID = kAudioObjectUnknown;
    UInt32 sz = sizeof(devID);
    if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL, &sz, &devID) != noErr)
        return;
    addr.mSelector = kAudioObjectPropertyName;
    addr.mScope    = kAudioObjectPropertyScopeGlobal;
    CFStringRef name = NULL;
    sz = sizeof(name);
    if (AudioObjectGetPropertyData(devID, &addr, 0, NULL, &sz, &name) != noErr)
        return;
    char buf[128] = {0};
    CFStringGetCString(name, buf, sizeof(buf), kCFStringEncodingUTF8);
    CFRelease(name);
    char msg[180];
    snprintf(msg, sizeof(msg), "CoreAudio: output device = \"%s\" (id=%u)", buf, (unsigned)devID);
    goVTLog(msg);
}

// Fires on AudioQueue's internal thread whenever kAudioQueueProperty_IsRunning
// changes — restarts the queue immediately if it stalled or was interrupted.
static void ca_running_listener(void *userData, AudioQueueRef aq, AudioQueuePropertyID prop) {
    (void)userData; (void)prop;
    if (!g_ca_started) return;
    UInt32 running = 0, sz = sizeof(running);
    if (AudioQueueGetProperty(aq, kAudioQueueProperty_IsRunning, &running, &sz) == noErr && !running) {
        g_ca_restart_count++;
        char msg[128];
        snprintf(msg, sizeof(msg), "CoreAudio: queue stalled — restart #%llu (drops=%llu)",
                 (unsigned long long)g_ca_restart_count, (unsigned long long)g_ca_drop_count);
        goVTLog(msg);
        AudioQueueStart(aq, NULL);
    }
}

void platform_ar_init(int channels, int sample_rate) {
    if (g_ca_queue) return;
    AudioStreamBasicDescription fmt = {
        .mSampleRate       = (Float64)sample_rate,
        .mFormatID         = kAudioFormatLinearPCM,
        .mFormatFlags      = kLinearPCMFormatFlagIsSignedInteger | kLinearPCMFormatFlagIsPacked,
        .mBytesPerPacket   = (UInt32)(channels * 2),
        .mFramesPerPacket  = 1,
        .mBytesPerFrame    = (UInt32)(channels * 2),
        .mChannelsPerFrame = (UInt32)channels,
        .mBitsPerChannel   = 16,
    };
    OSStatus s = AudioQueueNewOutput(&fmt, ca_callback, NULL, NULL, NULL, 0, &g_ca_queue);
    if (s != noErr) { g_ca_queue = NULL; return; }

    UInt32 buf_sz = (UInt32)(sample_rate / 1000 * 120 * channels * 2);
    pthread_mutex_lock(&g_ca_mu);
    g_ca_free_cnt    = 0;
    g_ca_started     = 0;
    g_ca_drop_count    = 0;
    g_ca_restart_count = 0;
    g_ca_frame_count   = 0;
    g_ca_stats_start   = 0.0;
    g_ca_was_dropping  = 0;
    for (int i = 0; i < CA_NUM_BUFFERS; i++) {
        if (AudioQueueAllocateBuffer(g_ca_queue, buf_sz, &g_ca_free[i]) == noErr)
            g_ca_free[g_ca_free_cnt++] = g_ca_free[i];
    }
    pthread_mutex_unlock(&g_ca_mu);
    // Restart queue automatically on any stop (underrun, OS interruption, etc.)
    AudioQueueAddPropertyListener(g_ca_queue, kAudioQueueProperty_IsRunning, ca_running_listener, NULL);
    // AudioQueueStart is deferred to the first platform_ar_decode call so the
    // queue isn't started empty (it would stall before audio frames arrive).
}

void platform_ar_cleanup(void) {
    if (!g_ca_queue) return;
    char msg[128];
    snprintf(msg, sizeof(msg), "CoreAudio: AudioQueue disposing (restarts=%llu drops=%llu)",
             (unsigned long long)g_ca_restart_count, (unsigned long long)g_ca_drop_count);
    goVTLog(msg);
    AudioQueueStop(g_ca_queue, true);
    AudioQueueDispose(g_ca_queue, true);
    g_ca_queue         = NULL;
    g_ca_free_cnt      = 0;
    g_ca_started       = 0;
    g_ca_drop_count    = 0;
    g_ca_restart_count = 0;
    g_ca_frame_count   = 0;
    g_ca_stats_start   = 0.0;
    g_ca_was_dropping  = 0;
}

void platform_ar_decode(const opus_int16 *pcm, int byte_count, int samples) {
    (void)samples;
    if (!g_ca_queue) return;
    pthread_mutex_lock(&g_ca_mu);
    AudioQueueBufferRef buf = (g_ca_free_cnt > 0) ? g_ca_free[--g_ca_free_cnt] : NULL;
    pthread_mutex_unlock(&g_ca_mu);
    if (!buf) {
        g_ca_drop_count++;
        g_ca_was_dropping = 1;
        return;
    }
    if (byte_count <= (int)buf->mAudioDataBytesCapacity) {
        memcpy(buf->mAudioData, pcm, byte_count);
        buf->mAudioDataByteSize = (UInt32)byte_count;
        AudioQueueEnqueueBuffer(g_ca_queue, buf, 0, NULL);
        g_ca_frame_count++;
        // Lazy start on first frame; property listener handles restarts after that.
        if (!g_ca_started) {
            g_ca_started = 1;
            g_ca_stats_start = mono_sec();
            AudioQueueStart(g_ca_queue, NULL);
            ca_log_device();
            goVTLog((char*)"CoreAudio: AudioQueue started (S16LE native output)");
        } else if (g_ca_was_dropping) {
            // Drop burst just ended — the queue drained while we were dropping.
            // IsRunning stays 1 on macOS even when the queue plays silence after
            // exhausting all buffers, so we must force stop+restart to get a fresh
            // hardware timeline. Without this the queue plays silence indefinitely.
            g_ca_was_dropping = 0;
            g_ca_restart_count++;
            char rmsg[160];
            snprintf(rmsg, sizeof(rmsg),
                "CoreAudio: drop burst ended (total_drops=%llu), forcing restart",
                (unsigned long long)g_ca_drop_count);
            goVTLog(rmsg);
            ca_log_device();
            AudioQueueStop(g_ca_queue, true);  // synchronous: wait for current buf
            AudioQueueStart(g_ca_queue, NULL); // fresh hardware timeline
        }
        // Periodic audio health log every 5 seconds.
        {
            double now = mono_sec();
            if (g_ca_stats_start > 0.0 && (now - g_ca_stats_start) >= 5.0) {
                pthread_mutex_lock(&g_ca_mu);
                int free_cnt = g_ca_free_cnt;
                pthread_mutex_unlock(&g_ca_mu);
                UInt32 running = 0, propSz2 = sizeof(running);
                AudioQueueGetProperty(g_ca_queue, kAudioQueueProperty_IsRunning, &running, &propSz2);
                char msg[256];
                snprintf(msg, sizeof(msg),
                    "CoreAudio: frames=%llu drops=%llu restarts=%llu free=%d/%d "
                    "running=%u muted=%d plc=%llu opus_err=%llu",
                    (unsigned long long)g_ca_frame_count,
                    (unsigned long long)g_ca_drop_count,
                    (unsigned long long)g_ca_restart_count,
                    free_cnt, CA_NUM_BUFFERS,
                    (unsigned)running,
                    g_audio_muted,
                    (unsigned long long)g_ar_plc_count,
                    (unsigned long long)g_ar_err_count);
                goVTLog(msg);
                g_ca_stats_start = now;
            }
        }
    } else {
        g_ca_drop_count++;
        pthread_mutex_lock(&g_ca_mu);
        if (g_ca_free_cnt < CA_NUM_BUFFERS) g_ca_free[g_ca_free_cnt++] = buf;
        pthread_mutex_unlock(&g_ca_mu);
    }
}

// ═══════════════════════════════════════════════════════════════════════════════
// VideoToolbox hardware decoder — platform video implementation
// ═══════════════════════════════════════════════════════════════════════════════

static VTDecompressionSessionRef   g_vt_session  = NULL;
static CMVideoFormatDescriptionRef g_vt_fmt_desc = NULL;
static uint8_t g_vps_data[2048]; static size_t g_vps_len = 0; // H.265 VPS
static uint8_t g_sps_data[1024]; static size_t g_sps_len = 0;
static uint8_t g_pps_data[256];  static size_t g_pps_len = 0;
static uint64_t g_vt_frame_count = 0;

// Codec type set by platform_set_video_format() before LiStartConnection.
// 0x0001=H264, 0x0100=H265, 0x1000=AV1_MAIN8 (matches VIDEO_FORMAT_* constants)
static int g_video_format = 0x0001;

void platform_set_video_format(int videoFormat) {
    g_video_format = videoFormat ? videoFormat : 0x0001;
    g_vps_len = 0; g_sps_len = 0; g_pps_len = 0; // clear old parameter sets
}

// FPS counter for VT decode delivery.
static uint64_t g_vt_fps_frames = 0;
static double   g_vt_fps_start  = 0.0;

// Pre-allocated RGBA conversion buffer — avoids per-frame malloc.
// Reallocated only on resolution changes; VT callbacks are serialised by VT's queue.
static uint8_t *g_vt_rgba_buf      = NULL;
static size_t   g_vt_rgba_buf_size = 0;

static void vt_invalidate(void) {
    if (g_vt_session) {
        VTDecompressionSessionWaitForAsynchronousFrames(g_vt_session);
        VTDecompressionSessionInvalidate(g_vt_session);
        CFRelease(g_vt_session);
        g_vt_session = NULL;
    }
    if (g_vt_fmt_desc) { CFRelease(g_vt_fmt_desc); g_vt_fmt_desc = NULL; }
    g_vps_len = 0; g_sps_len = 0; g_pps_len = 0; g_vt_frame_count = 0;
    g_vt_fps_frames = 0; g_vt_fps_start = 0.0;
    free(g_vt_rgba_buf); g_vt_rgba_buf = NULL; g_vt_rgba_buf_size = 0;
}

static void vt_callback(
    void *ctx, void *frameRefCon,
    OSStatus status, VTDecodeInfoFlags flags,
    CVImageBufferRef img, CMTime pts, CMTime dur)
{
    (void)ctx; (void)frameRefCon; (void)flags; (void)pts; (void)dur;
    if (status != noErr || img == NULL) return;

    // ── VT FPS counter (runs before routing so it always counts) ─────────────
    {
        double now = mono_sec();
        if (g_vt_fps_start == 0.0) g_vt_fps_start = now;
        g_vt_fps_frames++;
        double elapsed = now - g_vt_fps_start;
        if (elapsed >= 2.0) {
            char msg[128];
            snprintf(msg, sizeof(msg),
                     "VT decode: fps=%.1f  frames=%llu  (%.1fs window)",
                     (double)g_vt_fps_frames / elapsed,
                     (unsigned long long)g_vt_fps_frames, elapsed);
            goVTLog(msg);
            g_vt_fps_start  = now;
            g_vt_fps_frames = 0;
        }
    }

    // ── Metal fast path (zero CPU copy via IOSurface) ─────────────────────────
#if TARGET_OS_MAC && !TARGET_OS_IPHONE
    if (metal_video_try_submit(img)) return;
#endif

    // ── CPU fallback: BGRA→RGBA conversion into pre-allocated buffer ──────────
    CVPixelBufferLockBaseAddress(img, kCVPixelBufferLock_ReadOnly);
    int w   = (int)CVPixelBufferGetWidth(img);
    int h   = (int)CVPixelBufferGetHeight(img);
    size_t bpr = CVPixelBufferGetBytesPerRow(img);
    const uint8_t *src = (const uint8_t *)CVPixelBufferGetBaseAddress(img);

    // VT outputs kCVPixelFormatType_32BGRA (native HW format, always supported).
    // Convert BGRA→RGBA into a pre-allocated buffer to avoid per-frame malloc.
    // VT callbacks are serialised by VT's dispatch queue so no mutex is needed.
    size_t needed = (size_t)w * (size_t)h * 4;
    if (needed > g_vt_rgba_buf_size) {
        free(g_vt_rgba_buf);
        g_vt_rgba_buf = (uint8_t *)malloc(needed);
        g_vt_rgba_buf_size = g_vt_rgba_buf ? needed : 0;
    }
    if (g_vt_rgba_buf) {
        for (int y = 0; y < h; y++) {
            const uint8_t *row = src + (size_t)y * bpr;
            uint8_t *dst = g_vt_rgba_buf + (size_t)y * (size_t)w * 4;
            for (int x = 0; x < w; x++, row += 4, dst += 4) {
                dst[0] = row[2]; dst[1] = row[1]; dst[2] = row[0]; dst[3] = row[3];
            }
        }
        goVTFrame(g_vt_rgba_buf, w, h, w * 4);
    }
    CVPixelBufferUnlockBaseAddress(img, kCVPixelBufferLock_ReadOnly);
}

static int vt_create_session(void) {
    if (g_vt_session) {
        VTDecompressionSessionWaitForAsynchronousFrames(g_vt_session);
        VTDecompressionSessionInvalidate(g_vt_session);
        CFRelease(g_vt_session); g_vt_session = NULL;
    }
    if (g_vt_fmt_desc) { CFRelease(g_vt_fmt_desc); g_vt_fmt_desc = NULL; }

    OSStatus s;
    int isHEVC = (g_video_format & 0x0F00) != 0; // VIDEO_FORMAT_MASK_H265
    if (isHEVC) {
        // H.265: need VPS + SPS + PPS (VPS may be absent for some encoders)
        if (g_sps_len == 0 || g_pps_len == 0) {
            goVTLog((char*)"VT[HEVC]: waiting for SPS/PPS");
            return -1;
        }
        const uint8_t *params[3]; size_t sizes[3]; int count;
        if (g_vps_len > 0) {
            params[0] = g_vps_data; sizes[0] = g_vps_len;
            params[1] = g_sps_data; sizes[1] = g_sps_len;
            params[2] = g_pps_data; sizes[2] = g_pps_len;
            count = 3;
        } else {
            params[0] = g_sps_data; sizes[0] = g_sps_len;
            params[1] = g_pps_data; sizes[1] = g_pps_len;
            count = 2;
        }
        s = CMVideoFormatDescriptionCreateFromHEVCParameterSets(
            kCFAllocatorDefault, count, params, sizes, 4, NULL, &g_vt_fmt_desc);
    } else {
        // H.264
        if (g_sps_len == 0 || g_pps_len == 0) {
            goVTLog((char*)"VT[H264]: waiting for SPS/PPS");
            return -1;
        }
        const uint8_t *params[2] = { g_sps_data, g_pps_data };
        size_t         sizes[2]  = { g_sps_len,  g_pps_len  };
        s = CMVideoFormatDescriptionCreateFromH264ParameterSets(
            kCFAllocatorDefault, 2, params, sizes, 4, &g_vt_fmt_desc);
    }
    if (s != noErr) { goVTLog((char*)"VT: CMVideoFormatDescription FAILED"); return -1; }

    int32_t fmt = kCVPixelFormatType_32BGRA;
    CFNumberRef cfFmt = CFNumberCreate(NULL, kCFNumberSInt32Type, &fmt);
    const void *keys[] = { kCVPixelBufferPixelFormatTypeKey };
    const void *vals[] = { cfFmt };
    CFDictionaryRef attrs = CFDictionaryCreate(NULL, keys, vals, 1,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFRelease(cfFmt);

    VTDecompressionOutputCallbackRecord cb = { vt_callback, NULL };
    s = VTDecompressionSessionCreate(kCFAllocatorDefault, g_vt_fmt_desc, NULL, attrs, &cb, &g_vt_session);
    CFRelease(attrs);
    if (s != noErr) {
        goVTLog((char*)"VT: VTDecompressionSessionCreate FAILED");
        CFRelease(g_vt_fmt_desc); g_vt_fmt_desc = NULL;
        return -1;
    }
    VTSessionSetProperty(g_vt_session, kVTDecompressionPropertyKey_RealTime, kCFBooleanTrue);
    goVTLog((char*)"VT: VTDecompressionSession created OK");
    return 0;
}

static void walk_annexb(
    const uint8_t *data, int size,
    void (*fn)(const uint8_t *, int, void *), void *ctx)
{
    int pos = -1;
    for (int i = 0; i <= size - 3; i++) {
        if (data[i] == 0 && data[i+1] == 0 && data[i+2] == 1) { pos = i + 3; break; }
    }
    if (pos < 0) return;
    int nal_start = pos;
    while (nal_start < size) {
        int next_sc = -1;
        for (int i = nal_start; i <= size - 3; i++) {
            if (data[i] == 0 && data[i+1] == 0 && data[i+2] == 1) { next_sc = i; break; }
        }
        int nal_end, next_nal_start;
        if (next_sc < 0) {
            nal_end = size; next_nal_start = size;
        } else {
            nal_end = next_sc;
            while (nal_end > nal_start && data[nal_end - 1] == 0) nal_end--;
            next_nal_start = next_sc + 3;
        }
        int nal_len = nal_end - nal_start;
        if (nal_len > 0) fn(data + nal_start, nal_len, ctx);
        nal_start = next_nal_start;
    }
}

typedef struct { uint8_t *avcc; int avcc_len; int avcc_cap; int new_params; } vt_nal_ctx;

static void vt_handle_nal(const uint8_t *nal, int len, void *ptr) {
    vt_nal_ctx *ctx = (vt_nal_ctx *)ptr;
    if (len <= 0) return;

    int isHEVC = (g_video_format & 0x0F00) != 0;
    if (isHEVC) {
        // H.265: NAL header is 2 bytes; type = (nal[0] >> 1) & 0x3F
        if (len < 2) return;
        int nal_type = (nal[0] >> 1) & 0x3F;
        if (nal_type == 32) { // VPS
            if (len <= (int)sizeof(g_vps_data) &&
                (g_vps_len != (size_t)len || memcmp(g_vps_data, nal, len) != 0)) {
                memcpy(g_vps_data, nal, len); g_vps_len = len; ctx->new_params = 1;
            }
            return;
        }
        if (nal_type == 33) { // SPS
            if (len <= (int)sizeof(g_sps_data) &&
                (g_sps_len != (size_t)len || memcmp(g_sps_data, nal, len) != 0)) {
                memcpy(g_sps_data, nal, len); g_sps_len = len; ctx->new_params = 1;
            }
            return;
        }
        if (nal_type == 34) { // PPS
            if (len <= (int)sizeof(g_pps_data) &&
                (g_pps_len != (size_t)len || memcmp(g_pps_data, nal, len) != 0)) {
                memcpy(g_pps_data, nal, len); g_pps_len = len; ctx->new_params = 1;
            }
            return;
        }
    } else {
        // H.264: NAL type = nal[0] & 0x1F
        int nal_type = nal[0] & 0x1F;
        if (nal_type == 7) { // SPS
            if (len <= (int)sizeof(g_sps_data) &&
                (g_sps_len != (size_t)len || memcmp(g_sps_data, nal, len) != 0)) {
                memcpy(g_sps_data, nal, len); g_sps_len = len; ctx->new_params = 1;
            }
            return;
        }
        if (nal_type == 8) { // PPS
            if (len <= (int)sizeof(g_pps_data) &&
                (g_pps_len != (size_t)len || memcmp(g_pps_data, nal, len) != 0)) {
                memcpy(g_pps_data, nal, len); g_pps_len = len; ctx->new_params = 1;
            }
            return;
        }
    }
    int needed = ctx->avcc_len + 4 + len;
    if (needed > ctx->avcc_cap) {
        int nc = needed * 2 + 64;
        uint8_t *nb = (uint8_t *)realloc(ctx->avcc, nc);
        if (!nb) return;
        ctx->avcc = nb; ctx->avcc_cap = nc;
    }
    uint8_t *p = ctx->avcc + ctx->avcc_len;
    p[0] = (uint8_t)((len >> 24) & 0xFF); p[1] = (uint8_t)((len >> 16) & 0xFF);
    p[2] = (uint8_t)((len >>  8) & 0xFF); p[3] = (uint8_t)( len        & 0xFF);
    memcpy(p + 4, nal, len);
    ctx->avcc_len += 4 + len;
}

// platform_post_stop tears down the VT session after LiStopConnection joins threads.
// Safe because no more dr_submit callbacks can fire after thread join.
void platform_post_stop(void) {
    vt_invalidate();
}

int platform_dr_submit(PDECODE_UNIT du) {
    int total = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) total += e->length;
    if (total <= 0) return DR_OK;

    uint8_t *ab = (uint8_t *)malloc(total);
    if (!ab) return DR_NEED_IDR;
    int off = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) {
        memcpy(ab + off, e->data, e->length); off += e->length;
    }

    vt_nal_ctx ctx = {
        .avcc = (uint8_t *)malloc(total + 64), .avcc_len = 0,
        .avcc_cap = total + 64, .new_params = 0,
    };
    if (!ctx.avcc) { free(ab); return DR_NEED_IDR; }

    walk_annexb(ab, total, vt_handle_nal, &ctx);
    free(ab);

    if ((ctx.new_params || !g_vt_session) && g_sps_len > 0 && g_pps_len > 0) {
        if (vt_create_session() != 0) { free(ctx.avcc); return DR_NEED_IDR; }
    }
    if (!g_vt_session) { free(ctx.avcc); return DR_NEED_IDR; }

    int ret = DR_OK;
    if (ctx.avcc_len > 0 && g_vt_fmt_desc) {
        int avcc_len = ctx.avcc_len;
        CMBlockBufferRef block = NULL;
        OSStatus s = CMBlockBufferCreateWithMemoryBlock(
            kCFAllocatorDefault, ctx.avcc, avcc_len, kCFAllocatorMalloc,
            NULL, 0, avcc_len, 0, &block);
        if (s == noErr) {
            ctx.avcc = NULL;
            CMSampleTimingInfo timing = {
                .duration              = kCMTimeInvalid,
                .presentationTimeStamp = CMTimeMake((int64_t)g_vt_frame_count++, 60),
                .decodeTimeStamp       = kCMTimeInvalid,
            };
            size_t sample_sz = (size_t)avcc_len;
            CMSampleBufferRef sample = NULL;
            s = CMSampleBufferCreate(kCFAllocatorDefault, block, TRUE, NULL, NULL,
                g_vt_fmt_desc, 1, 1, &timing, 1, &sample_sz, &sample);
            CFRelease(block);
            if (s == noErr) {
                VTDecodeFrameFlags df = kVTDecodeFrame_EnableAsynchronousDecompression;
                VTDecodeInfoFlags  info = 0;
                s = VTDecompressionSessionDecodeFrame(g_vt_session, sample, df, NULL, &info);
                CFRelease(sample);
                if (s != noErr) ret = DR_NEED_IDR;
            } else { ret = DR_NEED_IDR; }
        } else { ret = DR_NEED_IDR; }
    }
    if (ctx.avcc) free(ctx.avcc);

    // Tear down VT session after stop (called from do_li_stop context via vt_invalidate).
    return ret;
}

*/
import "C"
