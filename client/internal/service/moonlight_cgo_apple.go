//go:build (darwin || ios) && !android && cgo

package service

/*
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
extern void goMoonlightRumble(unsigned short controllerNumber, unsigned short lowFreq, unsigned short highFreq);
extern void goVTLog(char *msg);
extern void goVTFrame(uint8_t *rgba, int width, int height, int stride);
extern void goVideoFormatNegotiated(int videoFormat);
extern void goAIVisionOverlay(uint8_t *rgba, int width, int height, int stride);
extern int  goAIVisionShouldSample(void);
extern void goAIVisionSample(uint8_t *rgba, int width, int height, int stride);

// Metal overlay fast path (macOS: metal_video_darwin.go, iOS: metal_video_ios.go).
extern int metal_video_try_submit(CVImageBufferRef img);
#if TARGET_OS_MAC && !TARGET_OS_IPHONE
extern int metal_video_is_active(void);
// HDR toggle -- macOS-only (metal_video_impl_darwin.m is excluded from iOS
// builds entirely, see that file's own TARGET_OS_IPHONE guard), see
// platform_set_video_format below and that function's own doc comment.
extern void metal_video_set_hdr(int enabled);

// AVSampleBufferDisplayLayer video path -- macOS-only, see
// metal_video_impl_darwin.m's g_avsbdl doc comment. Takes over the main
// H.264/H.265 presentation path from platform_dr_submit below (the
// VTDecompressionSession route further down stays compiled in, just
// unreached for that codec pair now).
extern int metal_video_submit_compressed_sample(CMSampleBufferRef sample);
extern int  metal_video_avsbdl_needs_fresh_idr(void);
extern void metal_video_avsbdl_clear_needs_idr(void);

// PyroWave decode (pyrowave_decode_darwin.m) -- macOS-only, see that file's own
// TARGET_OS_IPHONE-equivalent build tag (darwin && !ios).
extern uint8_t *pyrowave_darwin_slot_begin(size_t total);
extern void pyrowave_darwin_slot_commit(size_t total);
extern void pyrowave_darwin_teardown(void);
extern uint64_t pyrowave_darwin_frames(void);
#endif

#include "moonlight_cgo_shared.h"
#include "h264_stream.h"

// Shared monotonic clock — used by both audio and video subsystems.
static double mono_sec(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}

// ═══════════════════════════════════════════════════════════════════════════════
// CoreAudio AudioQueue — platform audio implementation
// Each decoded Opus frame is written into one queue buffer and enqueued.
// The completion callback recycles the buffer back to the free pool.
// ═══════════════════════════════════════════════════════════════════════════════

#define CA_NUM_BUFFERS 32

// Frames buffered (enqueued, not yet started) before AudioQueueStart is
// actually called -- both on the very first frame AND after every
// was_empty/long_gap-triggered restart below. Without this, playback used
// to start (or resume) the instant a single frame arrived, so any
// FEC-recovery latency or ordinary jitter right after immediately drained
// the queue again, forcing another audible Stop/Start cycle -- confirmed
// live via a real packet-loss sweep: CoreAudio queue restarts climbed into
// the thousands well before any real data was actually lost (drops/plc
// stayed at 0), i.e. the crackling was a client-side buffering-depth
// problem, not a server/FEC one. 3 frames at this client's negotiated
// 10ms Opus duration (see moonlight_cgo_shared.h's CAPABILITY_SLOW_OPUS_DECODER)
// is a ~30ms one-time latency cost, deliberately kept small since this
// whole pipeline is tuned for low latency.
#define CA_PREBUFFER_FRAMES 3

static AudioQueueRef        g_ca_queue    = NULL;
static AudioQueueBufferRef  g_ca_bufs[CA_NUM_BUFFERS];  // all allocated buffers
static AudioQueueBufferRef  g_ca_free[CA_NUM_BUFFERS];  // free-pool stack
static int                  g_ca_free_cnt = 0;
static int                  g_ca_started  = 0;
static int                  g_ca_prebuffer_cnt = 0; // frames enqueued while waiting to (re)start
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
static int      g_ca_was_dropping     = 0; // set while free pool is empty (drop burst)
static volatile int g_ca_force_restart = 0; // suppress listener during forced stop/start
static double   g_ca_last_decode_ts   = 0.0; // wall-clock time of the previous decode call

// Log the macOS default audio output device name (macOS HAL APIs, not available on iOS).
#if TARGET_OS_MAC && !TARGET_OS_IPHONE
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
#endif

// Fires on AudioQueue's internal thread whenever kAudioQueueProperty_IsRunning
// changes — restarts the queue immediately if it stalled or was interrupted.
static void ca_running_listener(void *userData, AudioQueueRef aq, AudioQueuePropertyID prop) {
    (void)userData; (void)prop;
    if (!g_ca_started || g_ca_force_restart) return;
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
    pthread_mutex_lock(&g_ca_mu);
    int already = (g_ca_queue != NULL);
    pthread_mutex_unlock(&g_ca_mu);
    if (already) return;

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
    AudioQueueRef aq = NULL;
    OSStatus s = AudioQueueNewOutput(&fmt, ca_callback, NULL, NULL, NULL, 0, &aq);
    if (s != noErr) {
        char msg[64];
        snprintf(msg, sizeof(msg), "CoreAudio: AudioQueueNewOutput failed: %d", (int)s);
        goVTLog(msg);
        return;
    }

    // 20 ms per buffer — one Opus frame fits cleanly; 32 buffers = 640 ms pipeline.
    UInt32 buf_sz = (UInt32)(sample_rate / 1000 * 20 * channels * 2);
    pthread_mutex_lock(&g_ca_mu);
    g_ca_free_cnt    = 0;
    g_ca_started     = 0;
    g_ca_prebuffer_cnt = 0;
    g_ca_drop_count    = 0;
    g_ca_restart_count = 0;
    g_ca_frame_count   = 0;
    g_ca_stats_start   = 0.0;
    g_ca_was_dropping  = 0;
    for (int i = 0; i < CA_NUM_BUFFERS; i++) {
        g_ca_bufs[i] = NULL;
        if (AudioQueueAllocateBuffer(aq, buf_sz, &g_ca_bufs[i]) == noErr)
            g_ca_free[g_ca_free_cnt++] = g_ca_bufs[i];
    }
    g_ca_queue = aq;
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
    g_ca_prebuffer_cnt = 0;
    g_ca_drop_count    = 0;
    g_ca_restart_count = 0;
    g_ca_frame_count   = 0;
    g_ca_stats_start   = 0.0;
    g_ca_was_dropping  = 0;
}

void platform_ar_decode(const opus_int16 *pcm, int byte_count, int samples) {
    (void)samples;
    double now = mono_sec();
    // A long gap since the previous decode call means the network/decoder
    // stalled long enough to drain playback entirely, even if a buffer or two
    // was still technically outstanding when it happened (which would hide
    // from the was_empty check below). Treat that as starvation too.
    int long_gap = g_ca_started && g_ca_last_decode_ts > 0.0 &&
                   (now - g_ca_last_decode_ts) > 0.25;
    g_ca_last_decode_ts = now;

    pthread_mutex_lock(&g_ca_mu);
    int was_empty = (g_ca_free_cnt == CA_NUM_BUFFERS);
    AudioQueueRef aq = g_ca_queue;
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
        g_ca_frame_count++;
        // Buffer CA_PREBUFFER_FRAMES frames before actually starting
        // playback -- both the very first start and every restart below
        // route through this same "not started yet" branch (a restart
        // sets g_ca_started back to 0 instead of calling AudioQueueStart
        // immediately). See CA_PREBUFFER_FRAMES's doc comment for why.
        if (!g_ca_started) {
            AudioQueueEnqueueBuffer(g_ca_queue, buf, 0, NULL);
            g_ca_prebuffer_cnt++;
            if (g_ca_prebuffer_cnt < CA_PREBUFFER_FRAMES) {
                return; // still filling the cushion, not started yet
            }
            g_ca_started = 1;
            g_ca_prebuffer_cnt = 0;
            g_ca_stats_start = mono_sec();
            AudioQueueStart(g_ca_queue, NULL);
#if TARGET_OS_MAC && !TARGET_OS_IPHONE
            ca_log_device();
#endif
            goVTLog((char*)"CoreAudio: AudioQueue started (S16LE native output)");
        } else if (g_ca_was_dropping || was_empty || long_gap) {
            // Drop burst ended OR queue completely starved (underflow) OR a
            // long real-time gap passed since the last frame (network stall).
            // IsRunning stays 1 on macOS even when the queue plays silence after
            // exhausting all buffers, so we must force stop+restart to get a fresh
            // hardware timeline. Without this the queue plays silence indefinitely.
            //
            // Order matters here: AudioQueueStop(..., true) is an *immediate*
            // stop, which per Apple's docs returns every currently-queued
            // buffer through the completion callback right away, whether or
            // not it finished playing. Enqueueing this frame's buffer before
            // calling Stop (as this used to do) meant Stop immediately handed
            // it straight back to the free pool, unplayed -- so the very
            // buffer this recovery was trying to get onto the speaker was
            // discarded by the recovery itself. That left the queue looking
            // just as starved on the next call, triggering another restart,
            // which discarded that buffer too: a self-sustaining loop that
            // never actually played audio and clicked on every Stop/Start
            // transition. Restarting *before* enqueueing fixes it: Stop only
            // flushes stale buffers, and this frame's fresh audio goes onto
            // a queue that's already clean.
            g_ca_was_dropping = 0;
            g_ca_restart_count++;
            char rmsg[160];
            snprintf(rmsg, sizeof(rmsg),
                "CoreAudio: %s (total_drops=%llu), forcing restart",
                was_empty ? "queue starved" : (long_gap ? "long stall detected" : "drop burst ended"),
                (unsigned long long)g_ca_drop_count);
            goVTLog(rmsg);
#if TARGET_OS_MAC && !TARGET_OS_IPHONE
            ca_log_device();
#endif
            g_ca_force_restart = 1;            // suppress listener during stop/start
            AudioQueueStop(g_ca_queue, true);  // synchronous: flushes stale buffers only
            AudioQueueEnqueueBuffer(g_ca_queue, buf, 0, NULL); // this frame's audio, onto the clean queue
            g_ca_force_restart = 0;
            // Re-enter the pre-buffer cushion instead of calling
            // AudioQueueStart immediately with just this one frame queued --
            // restarting with zero cushion is exactly what made restarts
            // cascade under real jitter (this frame plays, queue empties
            // again before the next one arrives, restart again...). The
            // next `CA_PREBUFFER_FRAMES - 1` calls take the `!g_ca_started`
            // branch above and actually call AudioQueueStart once buffered.
            g_ca_started = 0;
            g_ca_prebuffer_cnt = 1; // this frame already enqueued, counts toward the cushion
        } else {
            AudioQueueEnqueueBuffer(g_ca_queue, buf, 0, NULL);
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

// Codec type set by platform_set_video_format(), called from dr_setup with
// moonlight-common-c's real NegotiatedVideoFormat (see moonlight_cgo_shared.h's
// dr_setup) -- despite this comment previously saying "before LiStartConnection",
// that's mid-connection, once RTSP negotiation has actually completed.
// 0x0001=H264, 0x0100=H265, 0x1000=AV1_MAIN8 (matches VIDEO_FORMAT_* constants)
static int g_video_format = 0x0001;

// VT_FORMAT_MASK_10BIT: the part of Limelight.h's VIDEO_FORMAT_MASK_10BIT
// that VideoToolbox decodes -- HEVC Main10, HEVC RExt10_444, AV1 Main10,
// AV1 High10_444: every format bit this project's RustShine HDR color
// upgrade (see docs/COLOR_MODES.md) can end up negotiating. Limelight's
// mask also carries PyroWave's HDR bits, which never reach a VT session.
#define VT_FORMAT_MASK_10BIT 0xAA00
// VIDEO_FORMAT_PYROWAVE_HDR | VIDEO_FORMAT_PYROWAVE_444_HDR: PyroWave's own
// HDR10 upgrades, decoded into the same 10-bit BT.2020 PQ surfaces
// (pyrowave_decode_darwin.m) and so shown through the same EDR layer.
#define VIDEO_FORMAT_MASK_PYROWAVE_HDR 0xC0000

void platform_set_video_format(int videoFormat) {
    g_video_format = videoFormat ? videoFormat : 0x0001;
    g_vps_len = 0; g_sps_len = 0; g_pps_len = 0; // clear old parameter sets
#if TARGET_OS_MAC && !TARGET_OS_IPHONE
    // Toggle EDR presentation the moment the negotiated format is known,
    // before vt_create_session ever builds a session (let alone before the
    // first frame reaches metal_render_main_with_buf) -- see
    // metal_video_set_hdr's own doc comment for why this is the only
    // render-side change HDR needs.
    metal_video_set_hdr((g_video_format & (VT_FORMAT_MASK_10BIT | VIDEO_FORMAT_MASK_PYROWAVE_HDR)) != 0);
#endif
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

// vt_copy_rgba converts img into g_vt_rgba_buf as tightly packed RGBA
// (stride w*4). Handles the two 8-bit formats vt_create_session asks for:
// 32BGRA (macOS SDR) and 4:2:0 biplanar YCbCr (iOS SDR, BT.709 video or
// full range). Returns 0 for anything else (10-bit HDR) or on OOM.
// Only runs for the occasional AI Vision / live-frame sample and the
// barely-reachable CPU fallback -- never per displayed frame.
static int vt_copy_rgba(CVImageBufferRef img, int w, int h) {
    OSType pf = CVPixelBufferGetPixelFormatType(img);
    int nv12 = pf == kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange ||
               pf == kCVPixelFormatType_420YpCbCr8BiPlanarFullRange;
    if (!nv12 && pf != kCVPixelFormatType_32BGRA) return 0;

    size_t needed = (size_t)w * (size_t)h * 4;
    if (needed > g_vt_rgba_buf_size) {
        free(g_vt_rgba_buf);
        g_vt_rgba_buf = (uint8_t *)malloc(needed);
        g_vt_rgba_buf_size = g_vt_rgba_buf ? needed : 0;
    }
    if (!g_vt_rgba_buf) return 0;

    CVPixelBufferLockBaseAddress(img, kCVPixelBufferLock_ReadOnly);
    if (!nv12) {
        size_t bpr = CVPixelBufferGetBytesPerRow(img);
        const uint8_t *src = (const uint8_t *)CVPixelBufferGetBaseAddress(img);
        for (int y = 0; y < h; y++) {
            const uint8_t *row = src + (size_t)y * bpr;
            uint8_t *dst = g_vt_rgba_buf + (size_t)y * (size_t)w * 4;
            for (int x = 0; x < w; x++, row += 4, dst += 4) {
                dst[0] = row[2]; dst[1] = row[1]; dst[2] = row[0]; dst[3] = row[3];
            }
        }
    } else {
        const uint8_t *yp  = (const uint8_t *)CVPixelBufferGetBaseAddressOfPlane(img, 0);
        const uint8_t *uvp = (const uint8_t *)CVPixelBufferGetBaseAddressOfPlane(img, 1);
        size_t ybpr  = CVPixelBufferGetBytesPerRowOfPlane(img, 0);
        size_t uvbpr = CVPixelBufferGetBytesPerRowOfPlane(img, 1);
        int full = pf == kCVPixelFormatType_420YpCbCr8BiPlanarFullRange;
        // BT.709 in 16.16 fixed point; video range expands Y by 255/219
        // and chroma by 255/224.
        int ys  = full ? 65536 : 76309;
        int yo  = full ? 0 : 16;
        int crv = full ? 103206 : 117489; // 1.5748
        int cgu = full ? 12276  : 13975;  // 0.1873
        int cgv = full ? 30679  : 34925;  // 0.4681
        int cbu = full ? 121609 : 138438; // 1.8556
        for (int y = 0; y < h; y++) {
            const uint8_t *yr  = yp + (size_t)y * ybpr;
            const uint8_t *uvr = uvp + (size_t)(y >> 1) * uvbpr;
            uint8_t *dst = g_vt_rgba_buf + (size_t)y * (size_t)w * 4;
            for (int x = 0; x < w; x++, dst += 4) {
                int c = (yr[x] - yo) * ys;
                int u = uvr[x & ~1] - 128;
                int v = uvr[(x & ~1) + 1] - 128;
                int r = (c + crv * v) >> 16;
                int g = (c - cgu * u - cgv * v) >> 16;
                int b = (c + cbu * u) >> 16;
                dst[0] = (uint8_t)(r < 0 ? 0 : r > 255 ? 255 : r);
                dst[1] = (uint8_t)(g < 0 ? 0 : g > 255 ? 255 : g);
                dst[2] = (uint8_t)(b < 0 ? 0 : b > 255 ? 255 : b);
                dst[3] = 255;
            }
        }
    }
    CVPixelBufferUnlockBaseAddress(img, kCVPixelBufferLock_ReadOnly);
    return 1;
}

static void vt_callback(
    void *ctx, void *frameRefCon,
    OSStatus status, VTDecodeInfoFlags flags,
    CVImageBufferRef img, CMTime pts, CMTime dur)
{
    (void)ctx; (void)frameRefCon; (void)flags; (void)pts; (void)dur;
    if (status != noErr || img == NULL) {
        // Was a silent drop -- no logging at all -- which meant a VT decode
        // error here was indistinguishable from network silence further up
        // the stack (see checkVideoSilence's watchdog, a few lines below in
        // this file): both just looked like "no frames for N seconds" and
        // forced a full reconnect, with zero evidence of *why* left behind.
        if (status != noErr) {
            char msg[96];
            snprintf(msg, sizeof(msg), "VT: decode callback status=%d (frame dropped)", (int)status);
            goVTLog(msg);
        }
        return;
    }

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

    // ── Metal fast path (zero CPU copy via IOSurface, macOS and iOS) ─────────
    if (metal_video_try_submit(img)) {
        // Still notify Go for frame counting/FPS/lastFrameTime stats, mirroring
        // Android's zero-copy path (see moonlight_cgo_android.go's dr_submit,
        // which always calls goVTFrame after android_vk_try_submit_hwbuffer).
        // Without this, returning here left goVTFrame — and therefore
        // VideoWidget.lastFrameTime — completely unvisited for the rest of the
        // session the instant Metal took over (confirmed live: NULL was never
        // reached, so it wasn't just skipping the image copy, it was skipping
        // the callback outright). checkVideoSilence's watchdog only reads
        // lastFrameTime, so a perfectly healthy 60fps Metal-rendered stream
        // still looked frozen to it a few seconds after connecting, forcing a
        // reconnect on a strict ~5s cadence (3.5-4.5s silence timeout + ~1s
        // reconnect) forever — indistinguishable from real silence in the log
        // but not: Metal's own fps counter kept reporting a steady 57-60fps
        // the entire time. NULL is safe: goVTFrame's Go side already checks
        // NativeVideoOverlayIsActive() and delivers nil to the callback
        // instead of touching the pointer, so this costs nothing beyond the
        // call itself.
        int w = (int)CVPixelBufferGetWidth(img);
        int h = (int)CVPixelBufferGetHeight(img);

        // AI Vision on the zero-copy Metal path: goAIVisionShouldSample is a
        // pure-atomics check (no pixel access), true on roughly one frame
        // every aiVisionInterval -- only then do we pay for a CPU readback
        // of this one frame, converted BGRA->RGBA into the same scratch
        // buffer the CPU-fallback path below reuses. The completed
        // detection reaches the screen via a separate compositor overlay
        // layer (see metal_video_impl_darwin.m's g_overlay_layer), not by
        // drawing into this buffer -- it's discarded right after.
        if (goAIVisionShouldSample() && vt_copy_rgba(img, w, h)) {
            goAIVisionSample(g_vt_rgba_buf, w, h, w * 4);
        }

        goVTFrame(NULL, w, h, 0);
        return;
    }

    // This fallback assumes a single-plane, 4-bytes/pixel BGRA buffer --
    // never true for the 10-bit biplanar YCbCr format an HDR session
    // decodes to (see vt_create_session): CVPixelBufferGetBaseAddress
    // returns NULL for a planar buffer (the per-plane accessors are a
    // different API), so blindly running the byte-swap loop below against
    // it would dereference NULL rather than merely produce wrong colors.
    // In practice this fallback is barely reachable at all (IOSurface
    // backing is unconditionally requested in vt_create_session's own
    // buffer attributes, so metal_video_try_submit essentially always
    // succeeds -- see that function's own doc comment) -- but "essentially
    // always" isn't "always", so this stays a clean dropped-frame instead
    // of a crash if it ever is.
    if (g_video_format & VT_FORMAT_MASK_10BIT) {
        goVTLog((char*)"VT: HDR frame missed the zero-copy IOSurface path -- dropping (CPU fallback doesn't support 10-bit YCbCr)");
        goVTFrame(NULL, (int)CVPixelBufferGetWidth(img), (int)CVPixelBufferGetHeight(img), 0);
        return;
    }

    // ── CPU fallback: convert into the pre-allocated RGBA buffer ──────────────
    // VT callbacks are serialised by VT's dispatch queue so no mutex is needed.
    int w = (int)CVPixelBufferGetWidth(img);
    int h = (int)CVPixelBufferGetHeight(img);
    if (vt_copy_rgba(img, w, h)) {
        // AI Vision overlay: no-op unless enabled (single atomic load in the
        // common case) -- burns detection boxes+ids into the buffer in
        // place before it's handed off. Only reachable on this CPU
        // fallback path, not the zero-copy Metal fast path above (see
        // goAIVisionOverlay's doc comment in moonlight_cgo_wrapper.go).
        goAIVisionOverlay(g_vt_rgba_buf, w, h, w * 4);
        goVTFrame(g_vt_rgba_buf, w, h, w * 4);
    }
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

        // Apple's VideoToolbox is extremely strict about the SPS Level IDC.
        // NVIDIA Gamestream often sends Level 1.0 (0x0A) regardless of the actual resolution (e.g., 1080p).
        // This causes CMVideoFormatDescriptionCreateFromH264ParameterSets to fail with -12710 (InvalidParameter).
        // We patch the Level IDC to 4.2 (0x2A) if it's too low, to satisfy VideoToolbox for 1080p60.
        if (g_sps_len >= 4 && (g_sps_data[0] & 0x1F) == 7) {
            if (g_sps_data[3] < 0x2A) {
                goVTLog((char*)"VT: Patching H.264 SPS Level IDC to 4.2");
                g_sps_data[3] = 0x2A; // Level 4.2
            }
        }

        const uint8_t *params[2] = { g_sps_data, g_pps_data };
        size_t         sizes[2]  = { g_sps_len,  g_pps_len  };
        s = CMVideoFormatDescriptionCreateFromH264ParameterSets(
            kCFAllocatorDefault, 2, params, sizes, 4, &g_vt_fmt_desc);
    }
    if (s != noErr) {
        char errBuf[256];
        snprintf(errBuf, sizeof(errBuf), "VT: CMVideoFormatDescription FAILED (err=%d, isHEVC=%d, spsLen=%zu, ppsLen=%zu)", (int)s, isHEVC, g_sps_len, g_pps_len);
        goVTLog(errBuf);
        
        char hexDump[512] = "SPS: ";
        for (size_t i = 0; i < (g_sps_len < 16 ? g_sps_len : 16); i++) {
            snprintf(hexDump + strlen(hexDump), sizeof(hexDump) - strlen(hexDump), "%02X ", g_sps_data[i]);
        }
        strcat(hexDump, "| PPS: ");
        for (size_t i = 0; i < (g_pps_len < 16 ? g_pps_len : 16); i++) {
            snprintf(hexDump + strlen(hexDump), sizeof(hexDump) - strlen(hexDump), "%02X ", g_pps_data[i]);
        }
        goVTLog(hexDump);
        
        return -1;
    }

    // 10-bit HDR sessions decode straight to the 10-bit 4:2:0 YCbCr format
    // (matches rust-shine's own HDR capture/encode pixel format, see
    // video-encode::videotoolbox's module docs) instead of BGRA -- still
    // IOSurface-backed (same kCVPixelBufferIOSurfacePropertiesKey below), so
    // this stays on the zero-copy metal_video_try_submit fast path exactly
    // like the SDR case, no extra CPU work. Core Animation's own compositor
    // does the YUV(BT.2020,PQ) -> display conversion using the color tags
    // VideoToolbox attaches from this session's format description (which
    // carries the stream's own signaled colorimetry) -- see
    // metal_video_set_hdr's doc comment for the full reasoning.
    //
    // iOS SDR decodes to the decoder's native 8-bit 4:2:0 too, not BGRA:
    // asking for BGRA makes VT run a full-frame YCbCr->BGRA pass after
    // every hardware decode (4 bytes/px out vs 1.5) -- ~33 MB written per
    // frame at 3840x2160. The compositor takes the biplanar IOSurface as
    // layer contents directly, the same way the 10-bit HDR format already
    // does.
#if TARGET_OS_IPHONE
    int32_t fmt = ((g_video_format & VT_FORMAT_MASK_10BIT) != 0)
        ? kCVPixelFormatType_420YpCbCr10BiPlanarVideoRange
        : kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange;
#else
    int32_t fmt = ((g_video_format & VT_FORMAT_MASK_10BIT) != 0)
        ? kCVPixelFormatType_420YpCbCr10BiPlanarVideoRange
        : kCVPixelFormatType_32BGRA;
#endif
    CFNumberRef cfFmt = CFNumberCreate(NULL, kCFNumberSInt32Type, &fmt);
    
    int32_t min_buf_cnt = 24;
    CFNumberRef cfMinBuf = CFNumberCreate(NULL, kCFNumberSInt32Type, &min_buf_cnt);

    // IOSurface-backed output buffers are required for metal_video_try_submit's
    // zero-copy fast path (see metal_video_impl_darwin.m:181, which gates on
    // CVPixelBufferGetIOSurface(img) != NULL) -- without this key VT is free
    // to hand back plain CPU-only CVPixelBuffers, which silently disables
    // that fast path on every frame, every session, forcing the slow
    // CPU-copy fallback in vt_callback below to run synchronously inside
    // VT's own serialized decode-callback queue. Confirmed live: without
    // this, goVTFrame's fec-debug trace showed nilBuf=false (CPU path) for
    // every single frame, and VT's callback stopped firing entirely after
    // ~6-8 frames despite VTDecompressionSessionDecodeFrame continuing to
    // accept and report success on new submissions -- consistent with VT's
    // internal buffer pool backing up behind a callback queue that can't
    // drain fast enough once every frame requires a lock+copy+synchronous
    // cgo round-trip before the next one can even start.
    CFDictionaryRef ioSurfaceProps = CFDictionaryCreate(NULL, NULL, NULL, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    const void *keys[] = { kCVPixelBufferPixelFormatTypeKey, kCVPixelBufferIOSurfacePropertiesKey, kCVPixelBufferPoolMinimumBufferCountKey };
    const void *vals[] = { cfFmt, ioSurfaceProps, cfMinBuf };
    CFDictionaryRef attrs = CFDictionaryCreate(NULL, keys, vals, 3,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFRelease(cfFmt);
    CFRelease(ioSurfaceProps);
    CFRelease(cfMinBuf);

    CFDictionaryRef decoderSpec = CFDictionaryCreate(NULL,
        (const void **)&kVTVideoDecoderSpecification_RequireHardwareAcceleratedVideoDecoder,
        (const void **)&kCFBooleanTrue,
        1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);

    VTDecompressionOutputCallbackRecord cb = { vt_callback, NULL };
    s = VTDecompressionSessionCreate(kCFAllocatorDefault, g_vt_fmt_desc, decoderSpec, attrs, &cb, &g_vt_session);
    CFRelease(attrs);
    CFRelease(decoderSpec);
    if (s != noErr) {
        char errBuf[128];
        snprintf(errBuf, sizeof(errBuf), "VT: VTDecompressionSessionCreate FAILED (status=%d)", (int)s);
        goVTLog(errBuf);
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

// vt_nal_ctx drives the zero-copy Annex-B -> AVCC rewrite: base/dataBuffer
// describe the flattened RTP-fragment buffer (see platform_dr_submit) that
// every PICDATA NAL's bytes still physically live in; frameBuffer is the
// AVCC-formatted result handed to VideoToolbox. Matches the official
// Moonlight client's approach (Limelight/Stream/VideoDecoderRenderer.m,
// -updateAnnexBBufferForRange:): a NAL's *payload* is never copied a second
// time -- only its freshly-synthesized 4-byte length prefix is a real
// allocation; the payload itself is attached to frameBuffer by reference
// via CMBlockBufferAppendBufferReference, which keeps dataBuffer (and the
// flattened bytes it owns) alive for exactly as long as VT needs them.
typedef struct {
    const uint8_t *base;
    CMBlockBufferRef dataBuffer;
    CMBlockBufferRef frameBuffer;
    int new_params;
    int error;
} vt_nal_ctx;

static void vt_handle_nal(const uint8_t *nal, int len, void *ptr) {
    vt_nal_ctx *ctx = (vt_nal_ctx *)ptr;
    if (len <= 0 || ctx->error) return;

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
    // Append a fresh 4-byte block for this NAL's AVCC length prefix (tiny
    // allocation, independent of NAL size) ...
    size_t priorLen = CMBlockBufferGetDataLength(ctx->frameBuffer);
    OSStatus st = CMBlockBufferAppendMemoryBlock(ctx->frameBuffer, NULL, 4,
        kCFAllocatorDefault, NULL, 0, 4, 0);
    if (st != noErr) {
        char msg[96];
        snprintf(msg, sizeof(msg), "VT: CMBlockBufferAppendMemoryBlock failed: %d", (int)st);
        goVTLog(msg);
        ctx->error = 1; return;
    }
    uint8_t lenBytes[4] = {
        (uint8_t)((len >> 24) & 0xFF), (uint8_t)((len >> 16) & 0xFF),
        (uint8_t)((len >>  8) & 0xFF), (uint8_t)( len        & 0xFF),
    };
    st = CMBlockBufferReplaceDataBytes(lenBytes, ctx->frameBuffer, priorLen, 4);
    if (st != noErr) {
        char msg[96];
        snprintf(msg, sizeof(msg), "VT: CMBlockBufferReplaceDataBytes failed: %d", (int)st);
        goVTLog(msg);
        ctx->error = 1; return;
    }

    // ... then attach this NAL's payload *by reference* -- no copy -- into
    // the flattened buffer it already lives in (ctx->dataBuffer wraps
    // ctx->base; CMBlockBuffer retains dataBuffer for as long as this
    // reference, or anything built from frameBuffer, is alive).
    int off = (int)(nal - ctx->base);
    st = CMBlockBufferAppendBufferReference(ctx->frameBuffer, ctx->dataBuffer,
        (size_t)off, (size_t)len, 0);
    if (st != noErr) {
        char msg[96];
        snprintf(msg, sizeof(msg), "VT: CMBlockBufferAppendBufferReference failed: %d", (int)st);
        goVTLog(msg);
        ctx->error = 1; return;
    }
}

#if TARGET_OS_MAC && !TARGET_OS_IPHONE
// pyrowave_submit hands one PyroWave access unit to the decode thread
// (pyrowave_decode_darwin.m); the frame reaches the renderer through
// pyrowave_darwin_deliver there.
static int pyrowave_submit(PDECODE_UNIT du) {
    size_t total = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) total += (size_t)e->length;
    if (total == 0) return DR_OK;
    uint8_t *slot = pyrowave_darwin_slot_begin(total);
    if (!slot) return DR_NEED_IDR;
    size_t off = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) {
        memcpy(slot + off, e->data, (size_t)e->length);
        off += (size_t)e->length;
    }
    pyrowave_darwin_slot_commit(total);
    return DR_OK;
}
#endif

// platform_post_stop tears down the VT session after LiStopConnection joins threads.
// Safe because no more dr_submit callbacks can fire after thread join.
void platform_post_stop(void) {
    vt_invalidate();
#if TARGET_OS_MAC && !TARGET_OS_IPHONE
    pyrowave_darwin_teardown();
#endif
}

int platform_dr_submit(PDECODE_UNIT du) {
#if TARGET_OS_MAC && !TARGET_OS_IPHONE
    if (g_video_format & VIDEO_FORMAT_MASK_PYROWAVE) return pyrowave_submit(du);

    // See metal_video_impl_darwin.m's g_avsbdl_needs_idr doc comment: a
    // freshly-(re)created AVSampleBufferDisplayLayer needs an actual IDR as
    // its first sample, but by the time it exists (reactively, off the
    // frameNum==1 bootstrap) the stream's real opening IDR is long gone --
    // every frame in between was silently dropped with g_avsbdl still nil.
    // Skip straight to requesting one instead of feeding it an ordinary
    // delta frame it has no reference state for, which otherwise left it
    // waiting on whatever periodic refresh the host happened to be running
    // -- confirmed live as "took several minutes to show a picture".
    if (metal_video_avsbdl_needs_fresh_idr()) {
        if (du->frameType == FRAME_TYPE_IDR) {
            metal_video_avsbdl_clear_needs_idr(); // this one's good -- fall through and feed it below
        } else {
            return DR_NEED_IDR;
        }
    }
#endif

    int total = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) total += e->length;
    if (total <= 0) return DR_OK;

    // One gather copy is unavoidable -- du->bufferList's RTP fragments are
    // scattered across separate packet buffers, and Annex-B start codes can
    // straddle a fragment boundary, so NAL scanning needs one contiguous
    // range. This matches the official Moonlight client's own
    // DrSubmitDecodeUnit (Connection.m) exactly: one malloc+memcpy to
    // flatten, then zero-copy from there on (see vt_handle_nal's own doc
    // comment). `ab`'s ownership transfers to dataBuffer below -- it must
    // stay a fresh malloc every call, not a reused buffer, since VT's
    // asynchronous decode can still be reading from it well after this
    // function returns.
    uint8_t *ab = (uint8_t *)malloc(total);
    if (!ab) return DR_NEED_IDR;
    int off = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) {
        memcpy(ab + off, e->data, e->length); off += e->length;
    }

    CMBlockBufferRef dataBuffer = NULL;
    OSStatus s0 = CMBlockBufferCreateWithMemoryBlock(
        kCFAllocatorDefault, ab, total, kCFAllocatorMalloc, NULL, 0, total, 0, &dataBuffer);
    if (s0 != noErr) {
        char msg[96];
        snprintf(msg, sizeof(msg), "VT: CMBlockBufferCreateWithMemoryBlock(data) failed: %d", (int)s0);
        goVTLog(msg);
        free(ab); return DR_NEED_IDR;
    }
    // dataBuffer now owns `ab` -- it (and whatever retains it, e.g. the
    // buffer references appended into frameBuffer below) will free() it
    // once its last reference drops, which can be well after this
    // function returns for an async decode.

    CMBlockBufferRef frameBuffer = NULL;
    OSStatus s1 = CMBlockBufferCreateEmpty(kCFAllocatorDefault, 0, 0, &frameBuffer);
    if (s1 != noErr) {
        char msg[96];
        snprintf(msg, sizeof(msg), "VT: CMBlockBufferCreateEmpty failed: %d", (int)s1);
        goVTLog(msg);
        CFRelease(dataBuffer); return DR_NEED_IDR;
    }

    vt_nal_ctx ctx = { .base = ab, .dataBuffer = dataBuffer, .frameBuffer = frameBuffer, .new_params = 0, .error = 0 };
    walk_annexb(ab, total, vt_handle_nal, &ctx);

    if ((ctx.new_params || !g_vt_session) && g_sps_len > 0 && g_pps_len > 0) {
        if (vt_create_session() != 0) { CFRelease(frameBuffer); CFRelease(dataBuffer); return DR_NEED_IDR; }
    }
    if (!g_vt_session) { CFRelease(frameBuffer); CFRelease(dataBuffer); return DR_NEED_IDR; }

    int ret = DR_OK;
    size_t frameLen = CMBlockBufferGetDataLength(frameBuffer);
    if (!ctx.error && frameLen > 0 && g_vt_fmt_desc) {
        // Real presentation time from the server, not a synthetic
        // frame-count timestamp -- matches the official Moonlight client
        // (VideoDecoderRenderer.m's own CMSampleTimingInfo). Doesn't drive
        // scheduling here (see g_avsbdl's own doc comment: no
        // controlTimebase is attached, same as official), but AVFoundation
        // still uses valid monotonic timestamps internally for its own
        // bookkeeping, so feed it the real one rather than a fake ~60Hz
        // counter left over from the old VTDecompressionSession path.
        g_vt_frame_count++; // kept for any other reader of this counter
        CMSampleTimingInfo timing = {
            .duration              = kCMTimeInvalid,
            .presentationTimeStamp = CMTimeMake((int64_t)du->presentationTimeUs, 1000000),
            .decodeTimeStamp       = kCMTimeInvalid,
        };
        CMSampleBufferRef sample = NULL;
        OSStatus s = CMSampleBufferCreate(kCFAllocatorDefault, frameBuffer, TRUE, NULL, NULL,
            g_vt_fmt_desc, 1, 1, &timing, 1, &frameLen, &sample);
        if (s == noErr) {
#if TARGET_OS_MAC && !TARGET_OS_IPHONE
            // Without this, AVSampleBufferDisplayLayer (metal_video_submit_compressed_sample
            // below) does NOT just show samples the instant they're decoded despite no
            // controlTimebase being attached (see g_avsbdl's own doc comment) -- it still paces
            // display against each sample's presentationTimeStamp using its own internally
            // created timebase, the same as a normal video player. That PTS is the host's real
            // RTP encode timestamp (see this function's own comment above), which inherits
            // network/encode jitter frame to frame; pacing display to it reproduces that jitter
            // on screen as judder even while decode/submit itself holds a rock-steady average
            // fps -- exactly the "steady 60fps but micro-stutters vs. official Moonlight"
            // symptom this was added to fix. kCMSampleAttachmentKey_DisplayImmediately tells the
            // layer to show each sample as soon as it's ready instead, which is what official
            // Moonlight's own AVSampleBufferDisplayLayer path (moonlight-ios's
            // VideoDecoderRenderer.m) actually relies on for this -- not merely omitting a
            // controlTimebase, which alone isn't enough.
            CFArrayRef attachments = CMSampleBufferGetSampleAttachmentsArray(sample, true);
            if (attachments != NULL && CFArrayGetCount(attachments) > 0) {
                CFMutableDictionaryRef attachment = (CFMutableDictionaryRef)CFArrayGetValueAtIndex(attachments, 0);
                CFDictionarySetValue(attachment, kCMSampleAttachmentKey_DisplayImmediately, kCFBooleanTrue);
            }
            // video_widget_ui.go's frameNum==1 bootstrap (which creates the
            // Metal/AVSBDL overlay in the first place -- see
            // metal_video_create) fires off *this* callback, the same one
            // metal_video_try_submit's own zero-copy IOSurface branch always
            // calls on decode success, REGARDLESS of whether an overlay
            // happens to exist yet -- that's what lets the very first frame
            // bootstrap the overlay into existence at all. Gating this on
            // metal_video_submit_compressed_sample's own return value was
            // exactly backwards: that call can only succeed *after* the
            // overlay exists, so frameNum got stuck at 0 forever, no
            // overlay ever got created, g_avsbdl stayed NULL, and the
            // no-frames-ever watchdog just reconnected in a loop -- confirmed
            // live (zero "overlay created" log lines across three straight
            // reconnects before this fix).
            CMVideoDimensions dim = CMVideoFormatDescriptionGetDimensions(g_vt_fmt_desc);
            goVTFrame(NULL, (int)dim.width, (int)dim.height, 0);
            int sr = metal_video_submit_compressed_sample(sample);
            if (sr == 2) {
                ret = DR_NEED_IDR; // layer had failed; flushed, needs a fresh IDR to resume
            } else if (sr == 0) {
                ret = DR_OK; // overlay inactive/torn down -- dropped, not a decode failure
            }
#else
            // iOS keeps the VTDecompressionSession path -- no AVSampleBufferDisplayLayer
            // wiring there (see metal_video_submit_compressed_sample's own macOS-only guard).
            VTDecodeFrameFlags df = kVTDecodeFrame_EnableAsynchronousDecompression;
            VTDecodeInfoFlags  info = 0;
            s = VTDecompressionSessionDecodeFrame(g_vt_session, sample, df, NULL, &info);
            if (s != noErr) {
                char msg[96];
                snprintf(msg, sizeof(msg), "VT: VTDecompressionSessionDecodeFrame failed: %d", (int)s);
                goVTLog(msg);
                ret = DR_NEED_IDR;
            }
#endif
            CFRelease(sample);
        } else {
            char msg[96];
            snprintf(msg, sizeof(msg), "VT: CMSampleBufferCreate failed: %d", (int)s);
            goVTLog(msg);
            ret = DR_NEED_IDR;
        }
    } else if (ctx.error) {
        ret = DR_NEED_IDR;
    }
    CFRelease(frameBuffer);
    CFRelease(dataBuffer);

    // Tear down VT session after stop (called from do_li_stop context via vt_invalidate).
    return ret;
}

*/
import "C"
