//go:build android && cgo

package service

/*
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo android CFLAGS: -D__ANDROID_UNAVAILABLE_SYMBOLS_ARE_WEAK__
// Per-ABI static libs from scripts/build_moonlight.sh (MOONLIGHT_ANDROID_ABI).
#cgo arm64 CFLAGS: -I${SRCDIR}/../../moonlight-common-c/build/android/opus/include/opus
#cgo arm64 LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build/android -L${SRCDIR}/../../moonlight-common-c/build/android/opus/lib
#cgo arm CFLAGS: -I${SRCDIR}/../../moonlight-common-c/build/android-armeabi-v7a/opus/include/opus
#cgo arm LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build/android-armeabi-v7a -L${SRCDIR}/../../moonlight-common-c/build/android-armeabi-v7a/opus/lib
#cgo LDFLAGS: -lmoonlight-common-c -lenet -lssl -lcrypto -lopus
#cgo LDFLAGS: -lmediandk -laaudio -lm -ldl -landroid -lEGL -lGLESv2

#include <media/NdkMediaCodec.h>
#include <media/NdkMediaFormat.h>
#include <media/NdkImageReader.h>
#include <aaudio/AAudio.h>
#include <android/log.h>
#include <android/native_window_jni.h>
#include <jni.h>
#include <Limelight.h>
#include "bench_frames.h"
#include "audio_shed.h"
#include <opus_multistream.h>
#include <pthread.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include <sys/system_properties.h>

#define ALOGI(...) __android_log_print(ANDROID_LOG_INFO,  "Moonlight/CGO", __VA_ARGS__)
#define ALOGE(...) __android_log_print(ANDROID_LOG_ERROR, "Moonlight/CGO", __VA_ARGS__)

extern void goMoonlightStage(int stage, int result, int errCode);
extern void goMoonlightConnected(void);
extern void goMoonlightTerminated(int errCode);
extern void goVideoFormatNegotiated(int videoFormat);
extern void goVTLog(char *msg);
extern void goVTFrame(uint8_t *rgba, int width, int height, int stride);
extern void goNetGraphOverlay(uint8_t *rgba, int width, int height, int stride);

// Declared in gl_video_impl_android.c
extern void         android_gl_set_jvm(JavaVM *jvm, jobject ctx);
extern ANativeWindow *android_gl_init(int width, int height);
extern uint8_t      *android_gl_get_frame(int width, int height);
extern void         *android_gl_get_frame_hwbuffer(int width, int height);
extern void          android_gl_release(void);
extern void          android_gl_set_low_latency(int on);
extern void          android_gl_log_vendor_params(const char *name);
extern int           android_decoder_best_size(const char *mime, int w, int h, int fps, int *out);
extern int           android_display_max_refresh(void);
extern void          android_gl_detach_thread(void);
extern uint8_t      *android_gl_get_frame_from_ahb(void *ahb, int crop_x, int crop_y, int crop_w, int crop_h);

// Declared in video_sps_android.c
extern int vsps_dims(const uint8_t *nal, size_t len, int hevc, int *w, int *h);
extern size_t vsps_h264_fixup(const uint8_t *nal, size_t len, uint8_t *out, size_t cap, int *had_vui, int *had_restr);

// Declared in video_latency_android.c
extern void vlat_note_submit(uint64_t pts, uint64_t receive_us, uint64_t now_moonlight_us);
extern void vlat_note_output(uint64_t pts);
extern void vlat_note_handoff(void);
extern void vlat_note_dropped(void);
extern void vlat_reset(void);

// Declared in vk_video_impl_android.c
extern void android_vk_set_jvm(JavaVM *jvm, jobject ctx);
extern int  android_vk_is_active(void);
extern int  android_vk_try_submit(uint8_t *rgba, int width, int height, int stride);
extern int  android_vk_try_submit_hwbuffer(void *ahb, int width, int height);
extern int  android_vk_hwbuffer_supported(void);
extern int  android_vk_aimage_supported(void);
extern int  android_vk_try_submit_aimage(void *img);
extern void android_vk_release_aimages(void);
extern void android_vk_set_pacing(int level);

// Bodies in net_graph_stats_android.c (not inline here: this file has
// //export, so a non-static function body in this preamble would be
// duplicated into _cgo_export.c). g_last_host_latency_tenths_ms is
// defined there for the same reason.
extern volatile uint16_t g_last_host_latency_tenths_ms;
extern volatile int g_amc_negotiated_fmt; // VIDEO_FORMAT_* of the running decoder, -1 = none
void do_get_rtp_video_stats(uint32_t *out);
int do_get_estimated_rtt_info(uint32_t *out);
uint16_t do_get_last_host_latency_tenths_ms(void);
uint64_t do_get_playout_jitter_us(void);
uint64_t do_get_playout_applied_delay_us(void);

// ── Shared state ──────────────────────────────────────────────────────────────

#include <stdatomic.h>
// g_li_active uses a proper C11 atomic to ensure cross-thread visibility
// on ARM64 (plain volatile is insufficient for inter-thread signalling).
static _Atomic int    g_li_active      = 0;
static volatile int   g_audio_muted    = 0;
static AAudioStream  *g_aa_stream      = NULL;
// Sticks at AAUDIO_PERFORMANCE_MODE_NONE once ar_decode gives up on
// LOW_LATENCY (see the XRun-at-capacity escalation below) so every
// subsequent aaudio_open() -- including the ones triggered by write
// errors/stuck-stream recovery elsewhere in this file -- keeps using the
// mode that's actually sustainable on this device instead of reverting
// back to the one that was causing continuous underruns.
// Starts at NONE: over Wi-Fi, LOW_LATENCY's ~32 ms capacity underran 5-15
// times a second (387 XRuns in the first 35 s of a session, audible as
// crackle) until the escalation finally fired; the latency bound comes from
// audio_should_shed() instead.
static aaudio_performance_mode_t g_aa_perf_mode = AAUDIO_PERFORMANCE_MODE_NONE;
static int            g_audio_channels = 2;
static int            g_samples_per_frame = 240; // 5ms @ 48kHz default, overwritten by ar_init
static OpusMSDecoder *g_opus_decoder   = NULL;
static AMediaCodec   *g_amc            = NULL;
static int            g_amc_w          = 0;
static int            g_amc_h          = 0;
// Decoder input timestamps: the host's capture time (the RTP timestamp,
// unwrapped), so the output AImage carries it to the display-side jitter
// buffer. Strictly increasing; reset per decoder.
static uint64_t       g_amc_pts        = 0;
static uint32_t       g_amc_last_rtp   = 0;
static uint64_t       g_amc_rtp_ticks  = 0;
static int            g_amc_hevc       = 0; // stream is HEVC (for SPS parsing)
static int            g_sps_w = 0, g_sps_h = 0; // last coded size seen in an SPS

// ── Connection callbacks ──────────────────────────────────────────────────────

static void cl_stage_starting(int s) {
    ALOGI("cl_stage_starting: %d", s);
    goMoonlightStage(s, 0, 0);
}
static void cl_stage_complete(int s) {
    ALOGI("cl_stage_complete: %d", s);
    goMoonlightStage(s, 1, 0);
}
static void cl_stage_failed(int s, int ec) {
    ALOGE("cl_stage_failed: stage=%d, error=%d", s, ec);
    goMoonlightStage(s, -1, ec);
}

// ── Audio: Opus → AAudio ─────────────────────────────────────────────────────

static void aaudio_open(int channels, int sample_rate) {
    if (g_aa_stream) {
        // A stream from a previous session is still around — this happens when
        // ar_init fires again before that session's ar_cleanup ran (overlapping
        // reconnects, a second client racing the first, etc). Reusing it here
        // used to be a silent no-op: opus keeps decoding fine and
        // AAudioStream_write keeps returning success into a stream that may not
        // match this session's channel count/rate (or may just be a dead/
        // orphaned handle), so playback goes silent with zero errors anywhere
        // in the pipeline. Always tear down and rebuild fresh instead of trusting
        // a stream we didn't just create.
        ALOGI("aaudio_open: closing stale stream from a previous session before opening a fresh one");
        AAudioStream_requestStop(g_aa_stream);
        AAudioStream_close(g_aa_stream);
        g_aa_stream = NULL;
    }
    AAudioStreamBuilder *builder = NULL;
    AAudio_createStreamBuilder(&builder);
    AAudioStreamBuilder_setDirection(builder,       AAUDIO_DIRECTION_OUTPUT);
    AAudioStreamBuilder_setSharingMode(builder,     AAUDIO_SHARING_MODE_SHARED);
    AAudioStreamBuilder_setFormat(builder,          AAUDIO_FORMAT_PCM_I16);
    AAudioStreamBuilder_setChannelCount(builder,    channels);
    AAudioStreamBuilder_setSampleRate(builder,      (int32_t)sample_rate);
    AAudioStreamBuilder_setPerformanceMode(builder, g_aa_perf_mode);
    aaudio_result_t status = AAudioStreamBuilder_openStream(builder, &g_aa_stream);
    AAudioStreamBuilder_delete(builder);
    if (status == AAUDIO_OK) {
        int32_t burst = AAudioStream_getFramesPerBurst(g_aa_stream);
        // Set a larger initial buffer (e.g. 4x burst size) to prevent crackling
        // (XRuns) during the first few seconds of connection before Android's
        // internal adaptive buffer algorithm kicks in.
        // At least the slider's prebuffer though (audio_shed.h, 40 ms at the
        // low-latency end): a mixer-mode burst is ~20 ms, and 4 of them is
        // lag nobody asked for -- ar_decode grows it on actual XRuns.
        int32_t initial = sample_rate * audio_prebuffer_ms() / 1000;
        if (initial < burst * 2) initial = burst * 2;
        AAudioStream_setBufferSizeInFrames(g_aa_stream, initial);

        AAudioStream_requestStart(g_aa_stream);
        ALOGI("AAudio stream started: ch=%d rate=%d burst=%d perf_mode=%d",
              channels, sample_rate, burst, (int)g_aa_perf_mode);
    } else {
        ALOGE("AAudio openStream failed: %d", (int)status);
        g_aa_stream = NULL;
    }
}

// ar_init: called by Moonlight with Opus multistream config.
static int ar_init(int audioConfig, const POPUS_MULTISTREAM_CONFIGURATION cfg, void *ctx, int arFlags) {
    (void)audioConfig; (void)ctx; (void)arFlags;
    ALOGI("ar_init: ch=%d rate=%d streams=%d coupled=%d",
          cfg->channelCount, cfg->sampleRate, cfg->streams, cfg->coupledStreams);
    g_audio_channels = cfg->channelCount;
    g_samples_per_frame = cfg->samplesPerFrame > 0 ? cfg->samplesPerFrame : 240;
    if (g_opus_decoder) { opus_multistream_decoder_destroy(g_opus_decoder); g_opus_decoder = NULL; }
    int err = OPUS_OK;
    g_opus_decoder = opus_multistream_decoder_create(
        cfg->sampleRate, cfg->channelCount,
        cfg->streams, cfg->coupledStreams, cfg->mapping, &err);
    if (err != OPUS_OK) { ALOGE("opus create failed: %d", err); return -1; }
    aaudio_open(cfg->channelCount, (int)cfg->sampleRate);
    return 0;
}

static void ar_cleanup(void) {
    ALOGI("ar_cleanup");
    if (g_aa_stream) {
        AAudioStream_requestStop(g_aa_stream);
        AAudioStream_close(g_aa_stream);
        g_aa_stream = NULL;
    }
    if (g_opus_decoder) { opus_multistream_decoder_destroy(g_opus_decoder); g_opus_decoder = NULL; }
}

static void ar_start(void) {}
static void ar_stop(void)  {}

// ar_decode: called with Opus-encoded packet; decode → AAudio.
static void ar_decode(char *data, int len) {
    if (!g_opus_decoder || !g_aa_stream) return;
    opus_int16 pcm[5760 * 8];
    // For a real packet, frame_size is just the max output buffer capacity —
    // opus infers the actual decoded sample count from the packet itself, so
    // keep this generous (the full buffer) to avoid OPUS_BUFFER_TOO_SMALL (and
    // therefore total silence) if a packet ever encodes more than one nominal
    // frame's worth of audio.
    // For concealment (data==NULL, on packet loss) opus has no packet to infer
    // length from and will generate exactly frame_size samples, so that case
    // must use the host's real samplesPerFrame — passing the 5760 buffer cap
    // there made every lost packet produce ~120ms of concealment audio instead
    // of the real ~5-10ms frame, throwing off AAudio's timeline and causing
    // periodic crackling on lossy networks.
    int frameSize = data ? 5760 : g_samples_per_frame;
    int samples = opus_multistream_decode(g_opus_decoder,
        (const unsigned char *)data, len, pcm, frameSize, 0);
    static int s_decode_calls = 0;
    static int s_decode_errs  = 0;
    static int s_write_errs   = 0;
    s_decode_calls++;
    if (samples <= 0) {
        s_decode_errs++;
        if (s_decode_errs <= 5 || (s_decode_errs % 200) == 0) {
            ALOGE("ar_decode: opus decode failed ret=%d (call #%d, %d failures so far, muted=%d)",
                  samples, s_decode_calls, s_decode_errs, g_audio_muted);
        }
        return;
    }
    if (g_audio_muted) memset(pcm, 0, (size_t)(samples * g_audio_channels * 2));

    // Adaptive buffer size to prevent crackling (XRuns) during Wi-Fi lag.
    //
    // Growing the buffer on every XRun is a losing game if XRuns keep piling
    // up regardless: on some devices/loads AAudio's own internal tuning walks
    // the buffer size back down between our checks (observed via
    // AAudioStream_getBufferSizeInFrames() never actually reading back as
    // "stuck at capacity" even though the resize we issue is clearly not
    // helping — it just re-grows to the same ceiling forever, logged on every
    // single call). So detection here does NOT depend on the resize becoming
    // a no-op; it tracks the XRun *rate* directly over a fixed window of
    // decode calls (~1s of audio at typical 5ms Opus frames) and escalates
    // once that rate crosses a threshold, however the buffer size happens to
    // be reading at the time.
    //
    // Escalation drops from AAUDIO_PERFORMANCE_MODE_LOW_LATENCY (exclusive
    // MMAP, tightest possible timing) to AAUDIO_PERFORMANCE_MODE_NONE (goes
    // through AudioFlinger's mixer, far more forgiving of scheduling jitter)
    // -- the standard Android fallback for exactly this case: LOW_LATENCY
    // can't keep its deadline when the same cores are also busy with video
    // decode + GL/Vulkan rendering. g_aa_perf_mode is process-global and only
    // ever moves LOW_LATENCY -> NONE, never back, so this can fire at most
    // once per app run: once we know LOW_LATENCY doesn't work on this device
    // under real load, there's no reason to keep re-trying it every session.
    #define AUDIO_XRUN_WINDOW_CALLS  200 // ~1s of audio at 5ms/frame
    #define AUDIO_XRUN_WINDOW_LIMIT   20 // >10% of frames underrunning in that window
    static int32_t s_previous_xruns      = 0;
    static int      s_window_start_call  = 0;
    static int      s_window_xruns       = 0;
    int32_t xruns = AAudioStream_getXRunCount(g_aa_stream);
    if (xruns > s_previous_xruns) {
        s_previous_xruns = xruns;
        s_window_xruns++;

        int32_t current_size = AAudioStream_getBufferSizeInFrames(g_aa_stream);
        int32_t burst = AAudioStream_getFramesPerBurst(g_aa_stream);
        int32_t capacity = AAudioStream_getBufferCapacityInFrames(g_aa_stream);
        int32_t new_size = current_size + burst * 2; // Increase aggressively
        if (new_size > capacity) new_size = capacity;
        if (new_size > current_size) {
            AAudioStream_setBufferSizeInFrames(g_aa_stream, new_size);
            ALOGI("ar_decode: XRun detected (%d total, %d in current window), increased buffer to %d frames to prevent crackling",
                  xruns, s_window_xruns, new_size);
        }

        if (s_window_xruns > AUDIO_XRUN_WINDOW_LIMIT && g_aa_perf_mode == AAUDIO_PERFORMANCE_MODE_LOW_LATENCY) {
            ALOGE("ar_decode: %d XRuns in the last %d decode calls (buffer resizing isn't helping) — "
                  "falling back to AAUDIO_PERFORMANCE_MODE_NONE", s_window_xruns, s_decode_calls - s_window_start_call);
            g_aa_perf_mode = AAUDIO_PERFORMANCE_MODE_NONE;
            int rate = AAudioStream_getSampleRate(g_aa_stream);
            aaudio_open(g_audio_channels, rate);
            s_previous_xruns = 0; // reset xrun tracker for the new stream
            s_window_start_call = s_decode_calls;
            s_window_xruns = 0;
        }
    }
    if (s_decode_calls - s_window_start_call >= AUDIO_XRUN_WINDOW_CALLS) {
        s_window_start_call = s_decode_calls;
        s_window_xruns = 0;
    }

    // See audio_shed.h. Floor: the stream's buffer size -- a shared-mode
    // stream only starts playing once that much is queued, so shedding below
    // it kept playback from ever starting (silence a moment after connecting).
    {
        int64_t read = AAudioStream_getFramesRead(g_aa_stream);
        if (read > 0 && audio_should_shed(AAudioStream_getFramesWritten(g_aa_stream) - read,
                                          AAudioStream_getSampleRate(g_aa_stream), AAudioStream_getBufferSizeInFrames(g_aa_stream))) {
            return;
        }
    }

    // A single AAudioStream_write() call can return fewer frames than requested
    // (or 0) if the audio pipeline is briefly backed up (buffer momentarily
    // full) — this is normal on some devices/chipsets and does not mean the
    // frames were rejected forever, just that they didn't fit in this attempt.
    // The old code treated a short write as "done" and silently dropped the
    // remainder, which is audible as a stutter/skip. Retry the remaining
    // frames (advancing the buffer pointer by whole interleaved samples) until
    // everything is written or we give up after a few attempts.
    int written = 0;
    int attempts = 0;
    while (written < samples && attempts < 8) {
        aaudio_result_t wret = AAudioStream_write(g_aa_stream,
            pcm + (size_t)written * g_audio_channels, samples - written, 5000000LL);
        attempts++;
        if (wret < 0) {
            ALOGE("ar_decode: AAudioStream_write fatal error %d, reopening stream", (int)wret);
            int rate = AAudioStream_getSampleRate(g_aa_stream);
            aaudio_open(g_audio_channels, rate);
            break; // Will retry writing on the next audio packet
        }
        if (wret == 0) continue;
        written += (int)wret;
    }

    static int s_consec_short_writes = 0;
    if (written < samples) {
        s_consec_short_writes++;
        s_write_errs++;
        if (s_write_errs <= 5 || (s_write_errs % 200) == 0) {
            ALOGE("ar_decode: AAudioStream_write short after retries: wrote=%d requested=%d attempts=%d (call #%d, %d failures so far)",
                  written, samples, attempts, s_decode_calls, s_write_errs);
        }
        // If the stream is completely stuck (buffer full and never draining), restart it
        if (s_consec_short_writes > 10) {
            ALOGE("ar_decode: Stream is stuck (10 short writes), forcing restart to recover audio");
            int rate = AAudioStream_getSampleRate(g_aa_stream);
            aaudio_open(g_audio_channels, rate);
            s_consec_short_writes = 0; // reset to avoid infinite restart loop
            s_previous_xruns = 0; // reset xrun tracker for the new stream
        }
    } else {
        s_consec_short_writes = 0;
    }

    if ((s_decode_calls % 500) == 0) {
        ALOGI("ar_decode: heartbeat calls=%d decode_errs=%d write_errs=%d muted=%d",
              s_decode_calls, s_decode_errs, s_write_errs, g_audio_muted);
    }
}

// ── Video implementation (AMediaCodec) ──────────────────────────────────────

static void amc_present_output(int64_t pts);
static void amc_present_image(AImage *img);
static void amc_stop_output_thread(void);
static void amc_start_output_thread(void);
static void amc_teardown(void);

// Low-latency decode (default on). `adb shell setprop debug.usbridge.lowlat 0`
// before a stream starts restores the old pipeline -- plain MediaCodec
// format, outputs collected inline by dr_submit, frame-available polling --
// so the two can be compared with the same build (logcat USBridge/VideoLat).
static int g_amc_lowlat = 1;

// H.264 SPS fixup (default on, see vsps_h264_fixup). `adb shell setprop
// debug.usbridge.spsfix 0` passes the host's SPS through unchanged.
static int g_amc_spsfix = 1;
static int g_amc_spsfix_logged;


static int amc_read_prop_flag(const char *name) {
    char v[PROP_VALUE_MAX] = {0};
    if (__system_property_get(name, v) > 0 && v[0] == '0') return 0;
    return 1;
}

// ── Decoder images (low-latency path) ────────────────────────────────────────
//
// MediaCodec decodes into an AImageReader; each decoded buffer goes to
// Vulkan as is (sampled through a YCbCr conversion, see
// vk_video_impl_android.c) -- no GL copy, no glFinish. GL only reads a
// buffer back when CPU pixels are needed (Net Graph HUD, no Vulkan overlay).
// `adb shell setprop debug.usbridge.zerocopy 0` keeps the SurfaceTexture/GL
// copy instead.
static AImageReader   *g_air = NULL;
static pthread_mutex_t g_air_mu   = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t  g_air_cond = PTHREAD_COND_INITIALIZER;
static int             g_air_seq  = 0; // bumped by every new image

static void air_on_image(void *ctx, AImageReader *r) {
    (void)ctx; (void)r;
    pthread_mutex_lock(&g_air_mu);
    g_air_seq++;
    pthread_cond_broadcast(&g_air_cond);
    pthread_mutex_unlock(&g_air_mu);
}

// Images the reader can hand out at once: the one on screen and the one
// waiting in Vulkan, up to 3 held by the display-side jitter buffer
// (android_vk_set_pacing), the one being acquired, and slack for
// acquireLatestImage.
#define AIR_MAX_IMAGES 8

static ANativeWindow *air_create(int width, int height) {
    media_status_t st = AImageReader_newWithUsage(width, height, AIMAGE_FORMAT_PRIVATE,
        AHARDWAREBUFFER_USAGE_GPU_SAMPLED_IMAGE, AIR_MAX_IMAGES, &g_air);
    if (st != AMEDIA_OK || !g_air) {
        ALOGE("AImageReader_newWithUsage failed (%d) — using SurfaceTexture", (int)st);
        g_air = NULL;
        return NULL;
    }
    AImageReader_ImageListener l = { NULL, air_on_image };
    AImageReader_setImageListener(g_air, &l);
    ANativeWindow *win = NULL;
    if (AImageReader_getWindow(g_air, &win) != AMEDIA_OK || !win) {
        ALOGE("AImageReader_getWindow failed — using SurfaceTexture");
        AImageReader_delete(g_air);
        g_air = NULL;
        return NULL;
    }
    ANativeWindow_acquire(win); // the reader owns its window; amc_init releases ours
    return win;
}

// air_acquire waits (up to timeout_ms) for the image of the output buffer
// just released and returns the newest one, or NULL.
static AImage *air_acquire(int timeout_ms) {
    struct timespec deadline;
    clock_gettime(CLOCK_REALTIME, &deadline);
    deadline.tv_nsec += (long)timeout_ms * 1000000L;
    deadline.tv_sec += deadline.tv_nsec / 1000000000L;
    deadline.tv_nsec %= 1000000000L;
    for (;;) {
        pthread_mutex_lock(&g_air_mu);
        int seq = g_air_seq;
        pthread_mutex_unlock(&g_air_mu);
        AImage *img = NULL;
        media_status_t st = AImageReader_acquireLatestImage(g_air, &img);
        if (st == AMEDIA_OK && img) return img;
        if (st == AMEDIA_IMGREADER_MAX_IMAGES_ACQUIRED) {
            ALOGE("air_acquire: all %d images held", AIR_MAX_IMAGES);
            return NULL;
        }
        pthread_mutex_lock(&g_air_mu);
        int rc = 0;
        while (g_air_seq == seq && rc == 0)
            rc = pthread_cond_timedwait(&g_air_cond, &g_air_mu, &deadline);
        pthread_mutex_unlock(&g_air_mu);
        if (rc != 0) return NULL; // timed out
    }
}

// amc_low_latency_format adds the keys that make decoders hand each frame
// out as soon as it is decoded instead of holding a few for smoothness:
// the standard KEY_LOW_LATENCY (Android 11+) and realtime priority, plus
// the vendor switches older Qualcomm / Exynos / MediaTek / Amlogic
// decoders read instead (MediaTek c2.mtk ignores KEY_LOW_LATENCY: Helio
// G36, HEVC 1080p, held ~10 frames / ~100 ms without its own key).
// Decoders ignore keys they don't know.
static void amc_low_latency_format(AMediaFormat *fmt) {
    AMediaFormat_setInt32(fmt, "low-latency", 1);
    AMediaFormat_setInt32(fmt, "priority", 0); // realtime
    AMediaFormat_setInt32(fmt, "vendor.qti-ext-dec-low-latency.enable", 1);
    AMediaFormat_setInt32(fmt, "vendor.qti-ext-dec-picture-order.enable", 1);
    AMediaFormat_setInt32(fmt, "vendor.rtc-ext-dec-low-latency.enable", 1);
    AMediaFormat_setInt32(fmt, "vendor.low-latency.enable", 1);
    AMediaFormat_setInt32(fmt, "vdec-lowlatency", 1); // MediaTek (Moonlight-Android uses it)
    // MediaTek c2 (from its supportedVendorParameters): no post-processing
    // (quality tuner / motion interpolation hold frames), CPU boost.
    AMediaFormat_setInt32(fmt, "vendor.mtk.ext.vdec.vpp.disabled.value", 1);
    AMediaFormat_setInt32(fmt, "vendor.mtk.vdec.cpu.boost.mode.value", 1);
}

// amc_is_qualcomm: Qualcomm parts get their own operating rate (see
// amc_new_format) and low-latency component (amc_create_decoder).
static int amc_is_qualcomm(const char *name) {
    return strncmp(name, "OMX.qcom.", 9) == 0 || strncmp(name, "c2.qti.", 7) == 0;
}

// Format tiers amc_init tries in turn, dropping what a decoder refused.
enum { AMC_FMT_PLAIN, AMC_FMT_LOWLAT, AMC_FMT_LOWLAT_RATE };

static AMediaFormat *amc_new_format(const char *mime, int width, int height, int fps,
                                    const char *decoder, int tier) {
    AMediaFormat *fmt = AMediaFormat_new();
    AMediaFormat_setString(fmt, "mime", mime);
    AMediaFormat_setInt32(fmt,  "width",  width);
    AMediaFormat_setInt32(fmt,  "height", height);
    if (tier >= AMC_FMT_LOWLAT) amc_low_latency_format(fmt);
    if (tier >= AMC_FMT_LOWLAT_RATE) {
        // Without these the platform sizes decoder clocks for 30 fps:
        // 4K at 60-120 fps then decodes at ~15-20 fps and backs up.
        if (fps > 0) AMediaFormat_setInt32(fmt, "frame-rate", fps);
        // Qualcomm (Snapdragon 695, HEVC 1080p60): twice the stream rate cut
        // decode from ~17 to ~13 ms; the maximum made it fall behind (53-56
        // of 60 fps decoded). `setprop debug.usbridge.oprate N` overrides.
        char v[PROP_VALUE_MAX] = {0};
        int oprate = __system_property_get("debug.usbridge.oprate", v) > 0 ? atoi(v) : -1;
        if (oprate <= 0) oprate = amc_is_qualcomm(decoder) ? (fps > 0 ? fps * 2 : 0) : 32767;
        if (oprate > 0) AMediaFormat_setInt32(fmt, "operating-rate", oprate);
    }
    return fmt;
}

// amc_create_decoder creates the platform's decoder for mime, or with
// lowlat on a Qualcomm part its separate low-latency component
// ("<name>.low_latency", listed next to the regular one): the regular one
// holds a frame back (output delay 1) even with the low-latency keys.
// name receives the component created.
static AMediaCodec *amc_create_decoder(const char *mime, int lowlat, char *name, size_t name_len) {
    AMediaCodec *c = AMediaCodec_createDecoderByType(mime);
    if (!c) return NULL;
    char *n = NULL;
    snprintf(name, name_len, "?");
    if (AMediaCodec_getName(c, &n) == AMEDIA_OK && n) {
        snprintf(name, name_len, "%s", n);
        AMediaCodec_releaseName(c, n);
    }
    if (lowlat && amc_is_qualcomm(name) && !strstr(name, ".low_latency")) {
        char ll[160];
        snprintf(ll, sizeof(ll), "%s.low_latency", name);
        AMediaCodec *l = AMediaCodec_createCodecByName(ll);
        if (l) {
            AMediaCodec_delete(c);
            snprintf(name, name_len, "%s", ll);
            return l;
        }
    }
    return c;
}

static void amc_init(int fmt_codec, int width, int height, int fps) {
    ALOGI("amc_init: fmt=0x%x %dx%d", fmt_codec, width, height);
    // Safety: clean up stale codec and EGL if dr_cleanup wasn't called.
    if (g_amc) {
        ALOGE("amc_init: stale g_amc found — cleaning up");
        amc_teardown();
    }
    g_amc_w = width; g_amc_h = height;
    g_amc_pts = 0;
    g_amc_lowlat = amc_read_prop_flag("debug.usbridge.lowlat");
    g_amc_spsfix = amc_read_prop_flag("debug.usbridge.spsfix");
    g_amc_spsfix_logged = 0;
    g_amc_negotiated_fmt = fmt_codec;
    vlat_reset();
    android_gl_set_low_latency(g_amc_lowlat);

    // GL is set up either way: it is the fallback for CPU pixels.
    ANativeWindow *nwin = android_gl_init(width, height);
    if (!nwin) { ALOGE("amc_init: no native window"); return; }
    if (g_amc_lowlat && amc_read_prop_flag("debug.usbridge.zerocopy")) {
        ANativeWindow *rwin = air_create(width, height);
        if (rwin) {
            ANativeWindow_release(nwin);
            nwin = rwin;
        }
    }

    const char *mime = "video/avc";
    g_amc_hevc = 0; g_sps_w = 0; g_sps_h = 0;
    if (fmt_codec & 0x0F00) { // VIDEO_FORMAT_MASK_H265
        mime = "video/hevc";
        g_amc_hevc = 1;
    } else if (fmt_codec & 0xF000) { // VIDEO_FORMAT_MASK_AV1
        mime = "video/av01";
    }

    // A decoder refuses the whole configure -- or, Qualcomm, the start --
    // over one key it doesn't take (e.g. a frame rate above its rating), so
    // step down until one sticks, on a fresh codec each time.
    int started = 0;
    char name[128] = "?";
    int tier = g_amc_lowlat ? AMC_FMT_LOWLAT_RATE : AMC_FMT_PLAIN;
    for (; tier >= AMC_FMT_PLAIN && !started; tier--) {
        g_amc = amc_create_decoder(mime, tier >= AMC_FMT_LOWLAT, name, sizeof(name));
        if (!g_amc) break;
        AMediaFormat *fmt = amc_new_format(mime, width, height, fps, name, tier);
        media_status_t st = AMediaCodec_configure(g_amc, fmt, nwin, NULL, 0);
        AMediaFormat_delete(fmt);
        const char *step = "configure";
        if (st == AMEDIA_OK) {
            step = "start";
            st = AMediaCodec_start(g_amc);
        }
        if (st == AMEDIA_OK) {
            started = 1;
            android_gl_log_vendor_params(name);
            ALOGI("AMediaCodec started (%s, %s, %dx%d@%d, lowlat=%d, format tier=%d, imagereader=%d)",
                  mime, name, width, height, fps, g_amc_lowlat, tier, g_air != NULL);
            if (g_amc_lowlat) amc_start_output_thread();
        } else {
            ALOGE("AMediaCodec %s failed (%s, %s, %dx%d@%d, tier %d, err %d)%s", step,
                  mime, name, width, height, fps, tier, (int)st,
                  tier > AMC_FMT_PLAIN ? " — retrying with fewer keys" : "");
            AMediaCodec_delete(g_amc);
            g_amc = NULL;
        }
    }

    if (nwin) ANativeWindow_release(nwin);
}

// ── Output thread (low-latency path) ─────────────────────────────────────────
//
// Waits on the decoder's output queue and shows each frame the moment it is
// decoded. The old inline path only polled for output right after queueing
// the next input, so every frame sat in the decoder until the following one
// arrived -- a whole frame interval of added latency. When several outputs
// are ready at once (a burst after a network stall), all but the newest are
// dropped instead of shown late.
static pthread_t   g_amc_out_thread;
static _Atomic int g_amc_out_run = 0;
static int         g_amc_out_started = 0;

static void *amc_output_thread(void *arg) {
    (void)arg;
    ALOGI("amc output thread started");
    while (atomic_load(&g_amc_out_run)) {
        AMediaCodecBufferInfo info;
        ssize_t idx = AMediaCodec_dequeueOutputBuffer(g_amc, &info, 20000);
        if (idx < 0) {
            // Timeout or format/buffers changed: just poll again. Anything
            // else is a codec error (e.g. a stream larger than it was
            // configured for), and the call then fails at once: back off
            // instead of spinning until the stream is torn down.
            if (idx < AMEDIACODEC_INFO_OUTPUT_BUFFERS_CHANGED) {
                struct timespec ts = {0, 20000000}; // 20 ms
                nanosleep(&ts, NULL);
            }
            continue;
        }
        for (;;) {
            AMediaCodecBufferInfo next;
            ssize_t n = AMediaCodec_dequeueOutputBuffer(g_amc, &next, 0);
            if (n < 0) break;
            AMediaCodec_releaseOutputBuffer(g_amc, (size_t)idx, 0);
            vlat_note_dropped();
            idx = n; info = next;
        }
        vlat_note_output((uint64_t)info.presentationTimeUs);
        AMediaCodec_releaseOutputBuffer(g_amc, (size_t)idx, 1);
        if (g_air) {
            AImage *img = air_acquire(30);
            if (img) amc_present_image(img);
        } else {
            amc_present_output(info.presentationTimeUs);
        }
    }
    // get_env() in the GL bridge attached this thread to the JVM; a thread
    // that exits attached aborts the process.
    android_gl_detach_thread();
    ALOGI("amc output thread stopped");
    return NULL;
}

static void amc_start_output_thread(void) {
    atomic_store(&g_amc_out_run, 1);
    if (pthread_create(&g_amc_out_thread, NULL, amc_output_thread, NULL) == 0) {
        g_amc_out_started = 1;
    } else {
        atomic_store(&g_amc_out_run, 0);
        g_amc_lowlat = 0; // fall back to collecting outputs in dr_submit
        ALOGE("amc output thread: pthread_create failed — inline output");
    }
}

static void amc_stop_output_thread(void) {
    if (!g_amc_out_started) return;
    atomic_store(&g_amc_out_run, 0);
    pthread_join(g_amc_out_thread, NULL);
    g_amc_out_started = 0;
}

static int dr_setup(int fmt, int w, int h, int rate, void *ctx, int flags) {
    ALOGI("dr_setup: %dx%d@%d", w, h, rate);
    goVideoFormatNegotiated(fmt);
    amc_init(fmt, w, h, rate);
    return 0;
}

// amc_log_stream_size logs the coded size in an IDR's SPS whenever it
// changes, next to what the decoder was configured for.
static void amc_log_stream_size(PDECODE_UNIT du) {
    if (du->frameType != FRAME_TYPE_IDR) return;
    for (PLENTRY e = du->bufferList; e; e = e->next) {
        if (e->bufferType != BUFFER_TYPE_SPS || e->length < 5) continue;
        const uint8_t *d = (const uint8_t *)e->data;
        size_t off = (d[2] == 1) ? 3 : 4; // 00 00 01 or 00 00 00 01
        int w = 0, h = 0;
        if (vsps_dims(d + off, (size_t)e->length - off, g_amc_hevc, &w, &h) && (w != g_sps_w || h != g_sps_h)) {
            g_sps_w = w; g_sps_h = h;
            ALOGI("stream SPS: coded %dx%d (decoder configured %dx%d)%s", w, h, g_amc_w, g_amc_h,
                  (w > g_amc_w + 64 || h > g_amc_h + 64) ? " — LARGER than the decoder expects" : "");
        }
        return;
    }
}

static int dr_submit(PDECODE_UNIT du) {
    g_last_host_latency_tenths_ms = du->frameHostProcessingLatency;
    amc_log_stream_size(du);
    bench_frames_note(du);
    if (!g_amc) return DR_NEED_IDR;

    // Never silently drop a decode unit: if the codec has no free input buffer
    // (e.g. momentarily backed up), returning DR_OK here would discard this
    // frame's bitstream data with no recovery. Since H.264 P-frames predict off
    // prior frames, a silently dropped frame corrupts every subsequent frame
    // until the next IDR — which never comes because nothing signaled loss.
    // DR_NEED_IDR tells moonlight-common-c to ask the host for a fresh IDR so
    // the stream self-heals, matching what the other platform backends
    // (apple/windows/linux, all using ffmpeg/VideoToolbox) already do on
    // decode failure.
    ssize_t idx = AMediaCodec_dequeueInputBuffer(g_amc, 10000);
    if (idx < 0) {
        ALOGE("dr_submit: no input buffer available (idx=%zd) — requesting IDR", idx);
        return DR_NEED_IDR;
    }
    size_t sz = 0;
    uint8_t *buf = AMediaCodec_getInputBuffer(g_amc, idx, &sz);
    if (!buf) {
        ALOGE("dr_submit: getInputBuffer failed — requesting IDR");
        return DR_NEED_IDR;
    }
    size_t written = 0;
    for (PLENTRY e = du->bufferList; e && written < sz; e = e->next) {
        if (e->bufferType == BUFFER_TYPE_SPS && !g_amc_hevc && g_amc_spsfix && e->length > 5 && sz - written > 8) {
            const uint8_t *d = (const uint8_t *)e->data;
            size_t sc = (d[2] == 1) ? 3 : 4;
            int had_vui = 0, had_restr = 0;
            size_t fixed = vsps_h264_fixup(d + sc, (size_t)e->length - sc, buf + written + sc,
                                           sz - written - sc, &had_vui, &had_restr);
            if (!g_amc_spsfix_logged) {
                g_amc_spsfix_logged = 1;
                ALOGI("SPS fixup: %s (host VUI=%d bitstream_restriction=%d, %d -> %zu bytes)",
                      fixed ? "applied" : "parse failed, passing through", had_vui, had_restr,
                      e->length, fixed + sc);
            }
            if (fixed) {
                memcpy(buf + written, d, sc);
                written += sc + fixed;
                continue;
            }
        }
        size_t n = (e->length < sz - written) ? e->length : sz - written;
        memcpy(buf + written, e->data, n);
        written += n;
    }
    if (g_amc_pts == 0) g_amc_rtp_ticks = du->rtpTimestamp;
    else g_amc_rtp_ticks += (uint64_t)(int64_t)(int32_t)(du->rtpTimestamp - g_amc_last_rtp);
    g_amc_last_rtp = du->rtpTimestamp;
    uint64_t pts = g_amc_rtp_ticks * 100 / 9 + 1000000; // 90 kHz -> us, never 0
    g_amc_pts = (pts > g_amc_pts) ? pts : g_amc_pts + 1;
    vlat_note_submit(g_amc_pts, du->receiveTimeUs, LiGetMicroseconds());
    AMediaCodec_queueInputBuffer(g_amc, idx, 0, written, g_amc_pts, 0);

    if (g_amc_out_started) return DR_OK; // amc_output_thread shows it

    AMediaCodecBufferInfo info;
    ssize_t out_idx = AMediaCodec_dequeueOutputBuffer(g_amc, &info, 0);
    if (out_idx >= 0) {
        vlat_note_output((uint64_t)info.presentationTimeUs);
        AMediaCodec_releaseOutputBuffer(g_amc, out_idx, 1);
        amc_present_output(info.presentationTimeUs);
    }
    return DR_OK;
}

// amc_present_output copies the output just released to the SurfaceTexture
// on to the screen. Called by whichever thread collects decoder output.
static void amc_present_output(int64_t pts) {
    (void)pts;
    {
        // Net Graph is drawn by the overlay itself (vk_cmd_draw_hud /
        // GLES), so it never forces the CPU path; only the Fyne fallback
        // (no native overlay) burns it into the pixels.
        if (android_vk_is_active() && android_vk_hwbuffer_supported()) {
            // Zero-copy path: GL renders straight into an AHardwareBuffer
            // that Vulkan imports and blits from directly -- no glReadPixels,
            // no staging-buffer upload, no CPU touches the pixels at all.
            // See gl_video_impl_android.c/vk_video_impl_android.c.
            void *ahb = android_gl_get_frame_hwbuffer(g_amc_w, g_amc_h);
            if (ahb) {
                vlat_note_handoff();
                android_vk_try_submit_hwbuffer(ahb, g_amc_w, g_amc_h);
                // NULL: frame counting/FPS only, goVTFrame never dereferences
                // the pointer while Vulkan is active (see its Go side).
                goVTFrame(NULL, g_amc_w, g_amc_h, g_amc_w * 4);
            }
        } else {
            uint8_t *rgba = android_gl_get_frame(g_amc_w, g_amc_h);
            if (rgba) {
                if (android_vk_is_active()) {
                    vlat_note_handoff();
                    // Vulkan overlay active but this device/driver lacks
                    // AHardwareBuffer import support -- fall back to the CPU
                    // staging-buffer path, which works everywhere.
                    android_vk_try_submit(rgba, g_amc_w, g_amc_h, g_amc_w * 4);
                } else {
                    goNetGraphOverlay(rgba, g_amc_w, g_amc_h, g_amc_w * 4);
                }
                // Always notify Go for frame counting and FPS stats. goVTFrame detects
                // NativeVideoOverlayIsActive() and delivers nil to the callback so no
                // Go image allocation happens while Vulkan is rendering.
                goVTFrame(rgba, g_amc_w, g_amc_h, g_amc_w * 4);
            }
        }
    }
}

// amc_present_image shows a decoder image: straight to Vulkan when it can
// sample it, else read back through GL like amc_present_output's CPU path.
// Takes ownership of img.
static void amc_present_image(AImage *img) {
    if (android_vk_is_active() && android_vk_aimage_supported()) {
        vlat_note_handoff();
        if (android_vk_try_submit_aimage(img)) {
            goVTFrame(NULL, g_amc_w, g_amc_h, g_amc_w * 4);
            return;
        }
    }
    AHardwareBuffer *ahb = NULL;
    AImageCropRect c = {0, 0, g_amc_w, g_amc_h};
    uint8_t *rgba = NULL;
    int w = g_amc_w, h = g_amc_h;
    if (AImage_getHardwareBuffer(img, &ahb) == AMEDIA_OK && ahb) {
        if (AImage_getCropRect(img, &c) != AMEDIA_OK || c.right <= c.left || c.bottom <= c.top) {
            c.left = 0; c.top = 0; c.right = g_amc_w; c.bottom = g_amc_h;
        }
        w = c.right - c.left; h = c.bottom - c.top;
        rgba = android_gl_get_frame_from_ahb(ahb, c.left, c.top, w, h);
    }
    AImage_delete(img);
    if (!rgba) return;
    if (android_vk_is_active()) {
        vlat_note_handoff();
        android_vk_try_submit(rgba, w, h, w * 4);
    } else {
        goNetGraphOverlay(rgba, w, h, w * 4);
    }
    goVTFrame(rgba, w, h, w * 4);
}

// amc_teardown stops decoding and frees the codec, the image reader (once
// Vulkan gave its images back) and the GL context.
static void amc_teardown(void) {
    g_amc_negotiated_fmt = -1;
    if (g_amc) {
        amc_stop_output_thread();
        AMediaCodec_stop(g_amc);
        AMediaCodec_delete(g_amc);
        g_amc = NULL;
    }
    if (g_air) {
        android_vk_release_aimages();
        AImageReader_delete(g_air);
        g_air = NULL;
    }
    android_gl_release();  // always release EGL, even if codec creation failed
}

static void dr_cleanup(void) {
    ALOGI("dr_cleanup");
    amc_teardown();
}

// ── Start/Stop ────────────────────────────────────────────────────────────────

static void cl_connected(void)    { goMoonlightConnected(); }
static void cl_terminated(int ec) { goMoonlightTerminated(ec); }
static void cl_log(const char *fmt, ...) {
    char buf[256];
    va_list ap; va_start(ap, fmt); vsnprintf(buf, sizeof(buf), fmt, ap); va_end(ap);
    int n = (int)strlen(buf); if (n > 0 && buf[n-1] == '\n') buf[n-1] = '\0';
    if (buf[0]) ALOGI("[Moonlight] %s", buf);
}

static int tailnet_packet_size(const char *address) {
    // Tailnet (100.64/10, or the 127.0.0.1 proxy the client reaches it by): the
    // tunnel MTU is raised to 1440 (TS_DEBUG_MTU, on both ends), so 1392 + RTP
    // header + IP/UDP fits; 1200 elsewhere.
    unsigned a = 0, b = 0;
    if (address && sscanf(address, "%u.%u.", &a, &b) == 2 && (a == 127 || (a == 100 && b >= 64 && b < 128))) return 1392;
    return 1200;
}

int do_li_start(const char *address, const char *appV, const char *gfeV, const char *rtsp,
                int codec, int videoFmt, int w, int h, int fps, int bit,
                const unsigned char *key, int kid, int pipeFd) {
    (void)pipeFd;
    if (videoFmt == 0) videoFmt = VIDEO_FORMAT_H264;
    ALOGI("do_li_start: addr=%s rtsp=%s codec=%d videoFmt=%d %dx%d@%d bit=%d", address, rtsp, codec, videoFmt, w, h, fps, bit);

    // Safety: unconditionally stop any lingering C-level connection before
    // starting a new one.  LiStopConnection is a no-op when stage==STAGE_NONE,
    // so this is safe even when the Go layer already called do_li_force_stop.
    atomic_store(&g_li_active, 0);
    LiStopConnection();

    g_amc_w = w; g_amc_h = h;

    SERVER_INFORMATION srv;
    LiInitializeServerInformation(&srv);
    srv.address                = address;
    srv.serverInfoAppVersion   = appV;
    srv.serverInfoGfeVersion   = gfeV;
    srv.rtspSessionUrl         = rtsp;
    srv.serverCodecModeSupport = codec;

    STREAM_CONFIGURATION cfg;
    LiInitializeStreamConfiguration(&cfg);
    cfg.width                  = w;
    cfg.height                 = h;
    cfg.fps                    = fps;
    cfg.bitrate                = bit;
    cfg.packetSize             = tailnet_packet_size(address);
    cfg.streamingRemotely      = STREAM_CFG_AUTO;
    cfg.audioConfiguration     = AUDIO_CONFIGURATION_STEREO;
    cfg.supportedVideoFormats  = videoFmt;
    cfg.clientRefreshRateX100  = fps * 100;
    // Opt into audio encryption exactly like the official Moonlight clients do
    // (moonlight-android/moonlight-qt default to ENCFLG_AUDIO). Hosts that
    // require encrypted audio turn it on regardless of this flag, so leaving it
    // at ENCFLG_NONE only ever led to a negotiation mismatch, never to plaintext
    // audio we could actually play.
    cfg.encryptionFlags        = ENCFLG_AUDIO;
    if (key) {
        memcpy(cfg.remoteInputAesKey, key, 16);
        // remoteInputAesIv holds the rikeyid in BIG-endian (network) byte
        // order — that is what we sent as "rikeyid" in /launch and what
        // AudioStream.c reads back via BE32() to build the per-packet audio
        // AES-CBC IV. Writing it little-endian made the client's IV differ from
        // the host's, and because CBC only feeds the IV into the *first* block,
        // every audio packet decrypted with a corrupted first 16 bytes (the
        // Opus TOC byte) and an otherwise intact tail: opus_multistream_decode
        // still returned "success" but played garbled/alien noise. Video was
        // unaffected (unencrypted) and remote input was unaffected too (the
        // Sunshine control stream derives its own GCM IV from the sequence
        // number), which is why only audio was broken.
        cfg.remoteInputAesIv[0] = (char)((kid >> 24) & 0xff);
        cfg.remoteInputAesIv[1] = (char)((kid >> 16) & 0xff);
        cfg.remoteInputAesIv[2] = (char)((kid >>  8) & 0xff);
        cfg.remoteInputAesIv[3] = (char)( kid        & 0xff);
    }

    DECODER_RENDERER_CALLBACKS dr;
    LiInitializeVideoCallbacks(&dr);
    dr.setup            = dr_setup;
    dr.submitDecodeUnit = dr_submit;
    dr.cleanup          = dr_cleanup;
    // See moonlight_cgo_shared.h's identical assignment for why the RFI
    // bits are added here too -- both sides (host DESCRIBE flag + this
    // capability bit) are required before moonlight-common-c actually uses
    // reference-frame-invalidation recovery instead of a full IDR request.
    dr.capabilities     = CAPABILITY_DIRECT_SUBMIT | CAPABILITY_REFERENCE_FRAME_INVALIDATION_AVC | CAPABILITY_REFERENCE_FRAME_INVALIDATION_HEVC | CAPABILITY_REFERENCE_FRAME_INVALIDATION_AV1;

    AUDIO_RENDERER_CALLBACKS ar;
    LiInitializeAudioCallbacks(&ar);
    ar.init                = ar_init;
    ar.start               = ar_start;
    ar.stop                = ar_stop;
    ar.cleanup             = ar_cleanup;
    ar.decodeAndPlaySample = ar_decode;
    // See moonlight_cgo_shared.h's identical assignment for why -- requests
    // AudioPacketDuration=10ms (protocol-native branch) so a host's Opus
    // inband FEC (5ms is CELT-only, can never carry it) actually works.
    ar.capabilities        = CAPABILITY_SLOW_OPUS_DECODER;

    CONNECTION_LISTENER_CALLBACKS cl;
    LiInitializeConnectionCallbacks(&cl);
    cl.stageStarting       = cl_stage_starting;
    cl.stageComplete       = cl_stage_complete;
    cl.stageFailed         = cl_stage_failed;
    cl.connectionStarted   = cl_connected;
    cl.connectionTerminated = cl_terminated;
    cl.logMessage          = cl_log;

    ALOGI("do_li_start: calling LiStartConnection");
    int ret = LiStartConnection(&srv, &cfg, &cl, &dr, &ar, NULL, 0, NULL, 0);
    ALOGI("do_li_start: LiStartConnection returned %d", ret);
    if (ret != 0) return ret;
    atomic_store(&g_li_active, 1);
    ALOGI("do_li_start: g_li_active set to 1");
    return 0;
}

void do_li_stop(void) {
    int was_active = atomic_load(&g_li_active);
    ALOGI("do_li_stop (active=%d)", was_active);
    if (!was_active) return;
    atomic_store(&g_li_active, 0);
    ALOGI("do_li_stop: calling LiStopConnection");
    LiStopConnection();
    ALOGI("do_li_stop: LiStopConnection returned");
}

// do_li_force_stop unconditionally calls LiStopConnection regardless of
// g_li_active.  Go code that reliably tracks connection state calls this
// instead of do_li_stop to avoid the g_li_active-is-mysteriously-zero bug.
//
// Limelight.h documents LiStartConnection()/LiStopConnection() as NOT
// thread-safe against each other -- only LiInterruptConnection() is safe to
// call while a LiStartConnection() is in flight on another thread ("this
// interruption happens asynchronously so it is not safe to start another
// connection before the first LiStartConnection() call returns"). The Go
// side must call do_li_interrupt() immediately (safe, unsynchronized) and
// then wait for any in-flight do_li_start() to actually return (by taking
// the same liStartCallMu it runs under) before calling this function --
// calling LiStopConnection() while LiStartConnection() is still running
// races on moonlight-common-c's process-global `stage`/`initialized`
// statics (Connection.c, InputStream.c), which can leave `initialized`
// clobbered to false for a session that's actually still current: every
// LiSendMousePositionEvent/LiSendKeyboardEvent2/etc. call after that
// returns -2 and is silently dropped (callers here never checked the
// return value), producing exactly "video/audio fine, no input reaches the
// host, zero errors anywhere" -- confirmed against a live gamestream-server
// with a raw ENet client: an out-of-band test packet decoded and injected
// correctly every time, proving the server side was never the problem.
void do_li_force_stop(void) {
    int prev = atomic_exchange(&g_li_active, 0);
    ALOGI("do_li_force_stop: was active=%d, calling LiStopConnection", prev);
    LiStopConnection();
    ALOGI("do_li_force_stop: LiStopConnection returned");
}

// See do_li_force_stop's docs -- call this immediately (before waiting for
// any lock) to make an in-flight do_li_start() abort promptly, then wait
// for that call to actually return before calling do_li_force_stop().
void do_li_interrupt(void) {
    ALOGI("do_li_interrupt: calling LiInterruptConnection");
    LiInterruptConnection();
}

void set_audio_muted(int muted) { g_audio_muted = muted; }

// force_av_shutdown unconditionally stops and releases the AAudio stream and
// AMediaCodec decoder, independent of LiStopConnection ever actually calling
// our registered ar.cleanup/dr.cleanup callbacks.
//
// Those callbacks only fire if moonlight-common-c's own internal `stage`
// (Connection.c, a process-global with no generation awareness -- see
// do_li_force_stop's docs for the sibling bug this same root cause already
// produced on the *input* side) is not already STAGE_NONE by the time
// LiStopConnection() runs. Observed live: hitting disconnect (or the host
// stopping the stream) sometimes left audio/video still actively playing
// afterward, exactly as if the stop had silently no-op'd -- with the Go
// side's own liConnected/generation bookkeeping showing everything torn down
// correctly, meaning the mismatch was inside the library's own state, not
// ours. Calling this directly, unconditionally, right alongside every
// do_li_force_stop() call, means the actual hardware output is silenced the
// instant the client decides to stop, whatever moonlight-common-c's internal
// stage happens to be.
void force_av_shutdown(void) {
    ALOGI("force_av_shutdown: unconditionally releasing AAudio + AMediaCodec");
    if (g_aa_stream) {
        AAudioStream_requestStop(g_aa_stream);
        AAudioStream_close(g_aa_stream);
        g_aa_stream = NULL;
    }
    if (g_amc) {
        AMediaCodec_stop(g_amc);
        AMediaCodec_delete(g_amc);
        g_amc = NULL;
    }
    android_gl_release();
}

void set_audio_pipe_fd(int fd)  { (void)fd; }

// ── Input ─────────────────────────────────────────────────────────────────────

void do_send_key(short code, char act, char mod) { LiSendKeyboardEvent(code, act, mod); }
void do_send_utf8_text(const char *text, unsigned int len) { LiSendUtf8TextEvent(text, len); }
void do_send_mouse_move(short dx, short dy) {
    int ret = LiSendMouseMoveEvent(dx, dy);
    if (ret != 0) ALOGE("LiSendMouseMoveEvent FAILED ret=%d dx=%d dy=%d", ret, (int)dx, (int)dy);
}
void do_send_mouse_position(short x, short y, short w, short h) {
    int ret = LiSendMousePositionEvent(x, y, w, h);
    if (ret != 0) ALOGE("LiSendMousePositionEvent FAILED ret=%d x=%d y=%d w=%d h=%d", ret, (int)x, (int)y, (int)w, (int)h);
}
void do_send_mouse_button(char act, int btn) {
    int ret = LiSendMouseButtonEvent(act, btn);
    if (ret != 0) ALOGE("LiSendMouseButtonEvent FAILED ret=%d act=%d btn=%d", ret, (int)act, btn);
}
void do_send_scroll(signed char c) { LiSendScrollEvent(c); }
void do_send_multi_controller(unsigned short cn, unsigned short am, unsigned int b, unsigned char lt, unsigned char rt, short lx, short ly, short rx, short ry) {
    LiSendMultiControllerEvent(cn, am, b, lt, rt, lx, ly, rx, ry);
}
void do_send_controller_arrival(unsigned short cn, unsigned short am, unsigned char ctype,
    unsigned int supportedButtonFlags, unsigned short capabilities) {
    LiSendControllerArrivalEvent((uint8_t)cn, am, ctype, supportedButtonFlags, capabilities);
}
void do_send_pen(unsigned char eventType, unsigned char toolType, unsigned char penButtons,
                  float x, float y, float pressureOrDistance,
                  unsigned short rotation, unsigned char tilt) {
    LiSendPenEvent(eventType, toolType, penButtons, x, y, pressureOrDistance, 0.0f, 0.0f, rotation, tilt);
}
*/
import "C"

import (
	"fmt"
	"image"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

var (
	jniInitOnce             sync.Once
	liStartConnectionActive atomic.Bool
	activeStreamDone        chan struct{}
	activeStreamOnce        sync.Once
	activeStreamTermErr     error
	vtFrameCallback         func(image.Image)
	vtFrameCallbackMu       sync.Mutex

	// liStreamMu serializes LiStopConnection / LiStartConnection so they never
	// run concurrently. liStreamGen is a generation counter that lets the goroutine
	// detect whether it is still the "current" stream before touching shared state.
	liStreamMu  sync.Mutex
	liStreamGen atomic.Uint64

	// liStartCallMu serializes entry into C.do_li_start (LiStartConnection).
	// moonlight-common-c keeps its RTSP encryption/decryption crypto contexts
	// (RtspConnection.c: `static PPLT_CRYPTO_CONTEXT encryptionCtx/decryptionCtx`)
	// as process-global statics with no internal locking — the library assumes
	// exactly one LiStartConnection call is in flight at a time.
	//
	// liConnected only flips true once do_li_start() *returns* successfully, so
	// it cannot detect "a previous do_li_start() call is still mid-handshake".
	// A rapid reconnect (StartStream called again before the first call reaches
	// liConnected.Store(true)) used to slip past the liConnected guard and run
	// a second do_li_start() concurrently with the first — both racing on the
	// same static crypto contexts. Confirmed by device testing
	// (tests/test_android_ui_stress.sh, aggressive connect/disconnect hammering):
	// SIGSEGV null-deref in PltDestroyCryptoContext called from
	// performRtspHandshake, plus a handful of JNI CallVoidMethod SIGABRTs from
	// the resulting doubled-up native render/JNI threads.
	//
	// liStartCallMu wraps do_li_start() (LiStartConnection). do_li_force_stop()
	// (LiStopConnection) must ALSO go through this same mutex now -- see
	// stopConnectionSafely()'s docs. The previous design let do_li_force_stop()
	// run concurrently with an in-flight do_li_start() on the theory that
	// LiStopConnection() would "interrupt" it; Limelight.h actually documents
	// LiStartConnection()/LiStopConnection() as NOT thread-safe against each
	// other (only LiInterruptConnection() is safe to call concurrently), and
	// that mismatch was a real bug: Connection.c's `stage`/InputStream.c's
	// `initialized` are process-global statics with no generation awareness,
	// so a stale session's concurrent LiStopConnection() could clobber
	// `initialized` back to false for a session that had just legitimately
	// started, silently breaking every mouse/keyboard send from that point on
	// (confirmed live: server-side ENet decode/inject proven correct with a
	// raw test packet, yet real client input never arrived after a reconnect).
	liStartCallMu sync.Mutex

	// liConnected tracks whether LiStartConnection succeeded and LiStopConnection
	// has NOT yet been called.  Unlike C's g_li_active (which is mysteriously
	// cleared before StopStream reaches it), this is a reliable Go-level atomic
	// that is set by the goroutine and cleared atomically by whoever calls
	// LiStopConnection first (goroutine or StopStream).
	liConnected atomic.Bool
)

type MoonlightCgoWrapper struct {
	host       string
	audioMuted bool
}

func NewMoonlightCgoWrapper(host string) *MoonlightCgoWrapper {
	initVideoJNI()
	return &MoonlightCgoWrapper{host: host}
}

func initVideoJNI() {
	jniInitOnce.Do(func() {
		_ = driver.RunNative(func(ctx any) error {
			if ac, ok := ctx.(*driver.AndroidContext); ok && ac.VM != 0 && ac.Ctx != 0 {
				C.android_gl_set_jvm((*C.JavaVM)(unsafe.Pointer(ac.VM)), (C.jobject)(unsafe.Pointer(ac.Ctx)))
				C.android_vk_set_jvm((*C.JavaVM)(unsafe.Pointer(ac.VM)), (C.jobject)(unsafe.Pointer(ac.Ctx)))
				logrus.Info("🎬 [Moonlight/Android] JNI Initialized (VER: V4_FINAL_TRACE)")
			}
			return nil
		})
	})
}

// displayMaxFPS is the highest refresh rate this phone's display offers
// (0 if unknown): frames beyond it are decoded only to be dropped.
func displayMaxFPS() int {
	initVideoJNI()
	return int(C.android_display_max_refresh())
}

// decoderFitSize returns the largest size up to w x h, same aspect ratio,
// that this device's hardware decoder for videoMode accepts (it refuses to
// configure above its maximum, so asking the host for more means no video)
// and is rated to decode at fps -- a smaller picture at the requested rate
// beats 4K at 30 on a 1080p phone -- plus the decoder's advertised
// frame-rate ceiling there. ok is false when the platform gave no answer;
// then w x h is used as is.
func decoderFitSize(videoMode string, w, h, fps int) (fw, fh, maxFPS int, ok bool) {
	initVideoJNI()
	mime := "video/avc"
	switch strings.ToLower(videoMode) {
	case "h265", "hevc":
		mime = "video/hevc"
	case "av1":
		mime = "video/av01"
	}
	cMime := C.CString(mime)
	defer C.free(unsafe.Pointer(cMime))
	var out [3]C.int
	if C.android_decoder_best_size(cMime, C.int(w), C.int(h), C.int(fps), &out[0]) == 0 {
		return w, h, 0, false
	}
	return int(out[0]), int(out[1]), int(out[2]), true
}

// stopConnectionSafely calls LiInterruptConnection() immediately (documented
// safe to call while a LiStartConnection() is in flight on another
// goroutine), then waits for liStartCallMu -- i.e. for that in-flight
// do_li_start() to actually return -- before calling LiStopConnection()
// itself. Every do_li_force_stop() call site must go through this instead
// of calling it directly; see liStartCallMu's doc comment for why calling
// LiStopConnection() concurrently with LiStartConnection() corrupts
// moonlight-common-c's shared `stage`/`initialized` statics.
func stopConnectionSafely() {
	C.do_li_interrupt()
	liStartCallMu.Lock()
	defer liStartCallMu.Unlock()
	C.do_li_force_stop()
	// Belt-and-suspenders: do_li_force_stop only guarantees our own
	// ar.cleanup/dr.cleanup callbacks fire if moonlight-common-c's internal
	// `stage` isn't already STAGE_NONE by the time LiStopConnection() runs
	// (see force_av_shutdown's doc comment) -- calling this directly here
	// means audio/video hardware output actually stops the instant anything
	// calls stopConnectionSafely, regardless of that internal state.
	C.force_av_shutdown()
}

func (w *MoonlightCgoWrapper) StartStream(url string, key []byte, appV, gfeV string, codec, videoFormat, width, height, fps, bit int, pw, apw *os.File, onStop func(error)) error {
	// Acquire the mutex and cleanly stop any in-progress connection before
	// resetting state.  We use liConnected (Go-level atomic) as the reliable
	// signal because C's g_li_active may be zero by the time we reach here
	// even though the connection is technically still active.
	liStreamMu.Lock()
	if liConnected.Swap(false) {
		logrus.Info("🌕 [Moonlight/Android] StartStream: stopping previous connection")
		stopConnectionSafely()
	}
	myGen := liStreamGen.Add(1)
	activeStreamDone = make(chan struct{})
	activeStreamOnce = sync.Once{}
	activeStreamTermErr = nil // clear stale terminated-during-startup callbacks
	liStreamMu.Unlock()

	cHost := C.CString(w.host)
	cAppV := C.CString(appV)
	cGfeV := C.CString(gfeV)
	cRtsp := C.CString(url)
	var cKey *C.uchar
	if len(key) == 16 {
		cKey = (*C.uchar)(C.CBytes(key))
	}

	go func() {
		defer C.free(unsafe.Pointer(cHost))
		defer C.free(unsafe.Pointer(cAppV))
		defer C.free(unsafe.Pointer(cGfeV))
		defer C.free(unsafe.Pointer(cRtsp))
		if cKey != nil {
			defer C.free(unsafe.Pointer(cKey))
		}
		if pw != nil {
			defer pw.Close()
		}
		if apw != nil {
			defer apw.Close()
		}

		logrus.Infof("🌕 [Moonlight/CGO/Android] CALLING_C_LI_START: host=%s rtsp=%s codec=%d videoFmt=%d %dx%d@%d",
			w.host, url, codec, videoFormat, width, height, fps)

		// See liStartCallMu's doc comment: only one do_li_start (LiStartConnection)
		// call may run at a time — moonlight-common-c's crypto contexts are
		// unsynchronized process-global statics. Lock wait time is logged because
		// a nonzero wait here means a previous connect attempt was still tearing
		// down when this one arrived (expected under rapid reconnects, not a bug
		// by itself — the bug was letting both calls run at once).
		lockWaitStart := time.Now()
		liStartCallMu.Lock()
		if waited := time.Since(lockWaitStart); waited > 5*time.Millisecond {
			logrus.Infof("🌕 [Moonlight/CGO/Android] do_li_start waited %v for previous call to release liStartCallMu", waited)
		}
		// The jitter buffer (playout_buffer.go): Android paces at the
		// display instead of before decode (android_vk_set_pacing), so the
		// decoder's own spread is evened out too; moonlight-common-c's
		// pre-decode copy stays off rather than adding a second margin.
		os.Setenv("USBRIDGE_PLAYOUT_BUFFER", "0")
		C.android_vk_set_pacing(C.int(PlayoutBufferLevel()))

		callStart := time.Now()
		ret := C.do_li_start(cHost, cAppV, cGfeV, cRtsp,
			C.int(codec), C.int(videoFormat), C.int(width), C.int(height), C.int(fps), C.int(bit),
			cKey, C.int(1), C.int(-1))
		callDur := time.Since(callStart)
		liStartCallMu.Unlock()
		logrus.Infof("🌕 [Moonlight/CGO/Android] do_li_start returned ret=%d in %v", int(ret), callDur)

		if int(ret) != 0 {
			logrus.Errorf("🌕 [Moonlight/CGO/Android] LI_START_ERROR_CODE: %d", int(ret))
			if onStop != nil && liStreamGen.Load() == myGen {
				onStop(fmt.Errorf("LiStartConnection error %d", int(ret)))
			}
			return
		}

		// Mark that LiStartConnection succeeded — from this point on whoever
		// atomically swaps liConnected from true→false owns calling
		// do_li_force_stop (i.e. LiStopConnection).
		liConnected.Store(true)
		logrus.Info("🌕 [Moonlight/CGO/Android] ✅ streams active")
		// Unconditionally true, unlike the liStreamGen-gated reset below: this
		// goroutine's own do_li_start really did just succeed, so marking
		// input active is always correct even if a newer generation started
		// concurrently (harmless idempotent write -- the newer generation's
		// own do_li_start sets it true again once it succeeds). Gating this
		// the same way as the reset caused a real bug: under the reconnect
		// races this app can hit (rapid StartStream calls, a process restart
		// mid-handshake), this branch could run for a stale generation and
		// skip the store, leaving IsInputActive() stuck false even though
		// video/audio kept streaming fine -- every mouse/keyboard send (all
		// gated on this flag) was then silently dropped with no visible
		// error anywhere.
		liStartConnectionActive.Store(true)
		<-activeStreamDone

		logrus.Infof("🌕 [Moonlight/Android] goroutine woke from activeStreamDone — gen=%d current=%d connected=%v",
			myGen, liStreamGen.Load(), liConnected.Load())

		// Whoever wins the liConnected.Swap(false) race calls LiStopConnection.
		// This is reliable regardless of g_li_active state.
		liStreamMu.Lock()
		if liConnected.Swap(false) {
			logrus.Info("🌕 [Moonlight/Android] goroutine calling do_li_force_stop")
			stopConnectionSafely()
		} else {
			logrus.Info("🌕 [Moonlight/Android] goroutine: StopStream already called LiStopConnection")
		}
		liStreamMu.Unlock()

		// Only clear shared state if we are still the current generation.
		if liStreamGen.Load() == myGen {
			vtFrameCallbackMu.Lock()
			vtFrameCallback = nil
			vtFrameCallbackMu.Unlock()
			liStartConnectionActive.Store(false)
			if onStop != nil {
				onStop(activeStreamTermErr)
			}
		}
	}()
	return nil
}

func (w *MoonlightCgoWrapper) StopStream() {
	liStreamMu.Lock()
	// Always call stopConnectionSafely to trigger LiStopConnection, which will
	// interrupt LiStartConnection if it is blocking/looping inside C code.
	logrus.Info("🌕 [Moonlight/Android] StopStream: calling do_li_force_stop")
	stopConnectionSafely()

	// Reset connected flag so next start can proceed cleanly.
	liConnected.Store(false)
	liStreamMu.Unlock()

	// Unconditional, not gated on liStreamGen == myGen like the streaming
	// goroutine's own cleanup below -- an explicit StopStream() call is
	// authoritative regardless of which generation is "current": the user
	// (or the app) asked for playback to stop, so the callback that would
	// otherwise keep painting whatever frames are still in flight must be
	// cleared right now, not only once that goroutine's generation check
	// happens to agree.
	vtFrameCallbackMu.Lock()
	vtFrameCallback = nil
	vtFrameCallbackMu.Unlock()

	if activeStreamDone != nil {
		activeStreamOnce.Do(func() { close(activeStreamDone) })
	}
}

func (w *MoonlightCgoWrapper) SetAudioMuted(m bool) {
	w.audioMuted = m
	if m {
		C.set_audio_muted(1)
	} else {
		C.set_audio_muted(0)
	}
}
func (w *MoonlightCgoWrapper) GetAudioMuted() bool { return w.audioMuted }
func (w *MoonlightCgoWrapper) IsInputActive() bool { return liStartConnectionActive.Load() }

// Raw HID (moonlight_rawhid.go) is not wired on Android: its USB devices go
// through USB/IP.

func (w *MoonlightCgoWrapper) RawHIDEpoch() uint64 { return 0 }

// The Android client reads no pad touchpads or motion sensors yet.
func (w *MoonlightCgoWrapper) HostSupportsControllerExtensions() bool { return false }
func (w *MoonlightCgoWrapper) SendMoonlightControllerTouch(controller, eventType uint8, pointerID uint32, x, y, pressure float32) {
}
func (w *MoonlightCgoWrapper) SendMoonlightControllerMotion(controller, motionType uint8, x, y, z float32) {
}
func (w *MoonlightCgoWrapper) SendMoonlightControllerBattery(controller, state, percent uint8) {}
func (w *MoonlightCgoWrapper) SendMoonlightRawHID(kind, slot, endpoint uint8, total, offset uint16, data []byte, reliable bool) bool {
	return false
}

// negotiatedVideoFormat holds the VIDEO_FORMAT_* value dr_setup was given --
// the server's actual codec for this session (the benchmark's Codec row reads
// it). -1 until a session reports one.
var negotiatedVideoFormat atomic.Int32

func init() { negotiatedVideoFormat.Store(-1) }

//export goVideoFormatNegotiated
func goVideoFormatNegotiated(format C.int) {
	negotiatedVideoFormat.Store(int32(format))
}

func (w *MoonlightCgoWrapper) NegotiatedVideoCodecName() (string, bool) {
	if !liStartConnectionActive.Load() {
		return "", false
	}
	return videoFormatCodecName(negotiatedVideoFormat.Load())
}

func (w *MoonlightCgoWrapper) SendMoonlightKey(c int16, a, m int8) {
	if liStartConnectionActive.Load() {
		C.do_send_key(C.short(c), C.char(a), C.char(m))
	}
}
func (w *MoonlightCgoWrapper) SendMoonlightMouseMove(dx, dy int16) {
	if liStartConnectionActive.Load() {
		C.do_send_mouse_move(C.short(dx), C.short(dy))
	}
}
func (w *MoonlightCgoWrapper) SendMoonlightMousePosition(x, y, rw, rh int16) {
	if liStartConnectionActive.Load() {
		C.do_send_mouse_position(C.short(x), C.short(y), C.short(rw), C.short(rh))
	}
}
func (w *MoonlightCgoWrapper) SendMoonlightMouseButton(a int8, b int) {
	if liStartConnectionActive.Load() {
		C.do_send_mouse_button(C.char(a), C.int(b))
	}
}
func (w *MoonlightCgoWrapper) SendMoonlightScroll(c int8) {
	if liStartConnectionActive.Load() {
		C.do_send_scroll(C.schar(c))
	}
}
func (w *MoonlightCgoWrapper) SendMoonlightControllerEvent(cn uint16, am uint16, b uint32, lt uint8, rt uint8, lx int16, ly int16, rx int16, ry int16) {
	if liStartConnectionActive.Load() {
		C.do_send_multi_controller(C.ushort(cn), C.ushort(am), C.uint(b), C.uchar(lt), C.uchar(rt), C.short(lx), C.short(ly), C.short(rx), C.short(ry))
	}
}

func (w *MoonlightCgoWrapper) SendMoonlightControllerArrival(cn uint16, am uint16, ctype uint8, supportedButtonFlags uint32, capabilities uint16) {
	if liStartConnectionActive.Load() {
		C.do_send_controller_arrival(C.ushort(cn), C.ushort(am), C.uchar(ctype), C.uint(supportedButtonFlags), C.ushort(capabilities))
	}
}

func (w *MoonlightCgoWrapper) SendMoonlightUtf8Text(text string) {
	if !liStartConnectionActive.Load() || len(text) == 0 {
		return
	}
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	C.do_send_utf8_text(cs, C.uint(len(text)))
}

func (w *MoonlightCgoWrapper) SendMoonlightPenEvent(
	eventType, toolType, penButtons uint8,
	x, y, pressureOrDistance float32,
	rotation uint16, tilt uint8,
) {
	if liStartConnectionActive.Load() {
		C.do_send_pen(
			C.uchar(eventType), C.uchar(toolType), C.uchar(penButtons),
			C.float(x), C.float(y), C.float(pressureOrDistance),
			C.ushort(rotation), C.uchar(tilt),
		)
	}
}

//export goMoonlightStage
func goMoonlightStage(s, r, e C.int) {
	stages := []string{"none", "platform-init", "name-resolution", "audio-stream-init", "rtsp-handshake", "control-stream-init", "video-stream-init", "input-stream-init", "control-stream-start", "video-stream-start", "audio-stream-start", "input-stream-start"}
	name := "unknown"
	if int(s) < len(stages) {
		name = stages[s]
	}
	if r == 0 {
		logrus.Infof("🌕 [Moonlight] ► %s …", name)
	} else if r == 1 {
		logrus.Infof("🌕 [Moonlight] ✅ %s", name)
	} else {
		logrus.Errorf("🌕 [Moonlight] ❌ %s failed (%d)", name, int(e))
	}
}

//export goMoonlightConnected
func goMoonlightConnected() {
	logrus.Info("🌕 [Moonlight] connected ✅")
	notifyMoonlightStreamReady()
}

//export goMoonlightTerminated
func goMoonlightTerminated(e C.int) {
	logrus.Errorf("🌕 [Moonlight/Android] ❌ terminated: code=%d active=%v", int(e), liStartConnectionActive.Load())
	activeStreamTermErr = fmt.Errorf("terminated %d", int(e))
	negotiatedVideoFormat.Store(-1)
	if liStartConnectionActive.Load() {
		activeStreamOnce.Do(func() { close(activeStreamDone) })
	}
}

//export goVTLog
func goVTLog(m *C.char) { logrus.Infof("🎬 [Moonlight/CGO] %s", C.GoString(m)) }

// vtFramePool reuses image.RGBA buffers to avoid per-frame allocation.
var vtFramePool sync.Pool

//export goVTFrame
func goVTFrame(rgba *C.uint8_t, w, h, s C.int) {
	noteNativeFrameSize(int(w), int(h))
	vtFrameCallbackMu.Lock()
	cb := vtFrameCallback
	vtFrameCallbackMu.Unlock()
	if cb == nil {
		return
	}
	// A nil rgba means the C caller (dr_submit in this file) already decided
	// the frame was presented via the zero-copy AHardwareBuffer/Vulkan path
	// and passed NULL on purpose — just notify Go to increment frame counters.
	//
	// This used to re-derive that same decision here by calling
	// android_vk_is_active() again instead of trusting the pointer. That is
	// a TOCTOU race: dr_submit's own android_vk_is_active() check and this
	// one read that same flag at two different times with no synchronization
	// between them. During disconnect, the Vulkan renderer can be torn down
	// (setting the flag to inactive) in the gap between dr_submit deciding
	// "zero-copy, pass NULL" and this function re-checking the flag — which
	// then read "not active" and fell through to unsafe.Slice(nil, n) with
	// n > 0, crashing the process (SIGABRT) on every disconnect that raced
	// with an in-flight frame. Trusting rgba's nil-ness directly removes the
	// race: it's the same value the caller committed to in this one call.
	if rgba == nil {
		cb(nil)
		return
	}
	n := int(w) * int(h) * 4
	// Reuse or allocate image buffer.
	img, _ := vtFramePool.Get().(*image.RGBA)
	if img == nil || len(img.Pix) != n {
		img = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	} else {
		img.Rect = image.Rect(0, 0, int(w), int(h))
		img.Stride = int(w) * 4
	}
	// C buffer orientation is already correct (Y-flipped in kVerts, then glReadPixels un-flips).
	// One copy via unsafe.Slice avoids C.GoBytes intermediate allocation.
	src := unsafe.Slice((*byte)(unsafe.Pointer(rgba)), n)
	copy(img.Pix[:n], src)
	cb(img)
	vtFramePool.Put(img)
}
func SetVTFrameCallback(cb func(image.Image)) {
	vtFrameCallbackMu.Lock()
	vtFrameCallback = cb
	vtFrameCallbackMu.Unlock()
}
