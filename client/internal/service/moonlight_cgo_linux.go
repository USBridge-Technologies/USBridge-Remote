//go:build linux && !android && cgo

package service

/*
#cgo pkg-config: opus openssl libavcodec libavutil libswscale libpulse-simple
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build -L${SRCDIR}/../../moonlight-common-c/build/enet -lmoonlight-common-c -lenet
#cgo LDFLAGS: -lpthread -lm -ldl

#include <libavcodec/avcodec.h>
#include <libavutil/hwcontext.h>
#include <libavutil/frame.h>
#include <libavutil/imgutils.h>
#include <libavutil/pixdesc.h>
#include <libswscale/swscale.h>
#include <pulse/simple.h>
#include <pulse/error.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

// Go callbacks (defined in moonlight_cgo_wrapper.go).
extern void goMoonlightStage(int stage, int result, int errCode);
extern void goMoonlightConnected(void);
extern void goMoonlightTerminated(int errCode);
extern void goVTLog(char *msg);
extern void goVTFrame(uint8_t *rgba, int width, int height, int stride);
extern void goVideoFormatNegotiated(int videoFormat);

// GL overlay fast path (defined in gl_video_impl_linux.c).
extern int gl_video_is_active(void);
extern int gl_video_try_submit(uint8_t *rgba, int width, int height, int stride);

// Vulkan overlay fast path (defined in vk_video_impl_linux.c).
extern int vk_video_is_active(void);
extern int vk_video_try_submit(uint8_t *rgba, int width, int height, int stride);

#include "moonlight_cgo_shared.h"

// ═══════════════════════════════════════════════════════════════════════════════
// PulseAudio simple API audio output — platform audio implementation
// pa_simple handles clock drift, buffer management and PipeWire compat natively.
// ═══════════════════════════════════════════════════════════════════════════════

static pa_simple      *g_pa_s        = NULL;
static pthread_mutex_t g_pa_mu       = PTHREAD_MUTEX_INITIALIZER;
static int             g_pa_channels = 2;
static int             g_pa_rate     = 48000;

void platform_ar_init(int channels, int sample_rate) {
    g_pa_channels = channels;
    g_pa_rate     = sample_rate;
    pthread_mutex_lock(&g_pa_mu);
    if (g_pa_s) { pthread_mutex_unlock(&g_pa_mu); return; }

    pa_sample_spec ss = {
        .format   = PA_SAMPLE_S16LE,
        .rate     = (uint32_t)sample_rate,
        .channels = (uint8_t)channels,
    };
    // 50 ms target latency: low enough for real-time feel, large enough to
    // absorb per-frame jitter without underruns.
    pa_buffer_attr attr = {
        .maxlength = (uint32_t)-1,
        .tlength   = (uint32_t)(sample_rate * channels * 2 / 20), // 50 ms
        .prebuf    = (uint32_t)-1,
        .minreq    = (uint32_t)-1,
        .fragsize  = (uint32_t)-1,
    };
    int err = 0;
    g_pa_s = pa_simple_new(NULL, "usbridge", PA_STREAM_PLAYBACK,
                           NULL, "stream", &ss, NULL, &attr, &err);
    pthread_mutex_unlock(&g_pa_mu);
    if (!g_pa_s) {
        char msg[128];
        snprintf(msg, sizeof(msg), "PulseAudio: pa_simple_new failed: %s", pa_strerror(err));
        goVTLog(msg);
        return;
    }
    goVTLog((char*)"PulseAudio: audio stream opened (S16LE 50ms latency)");
}

void platform_ar_cleanup(void) {
    pthread_mutex_lock(&g_pa_mu);
    pa_simple *s = g_pa_s;
    g_pa_s = NULL;
    pthread_mutex_unlock(&g_pa_mu);
    if (!s) return;
    int err = 0;
    pa_simple_drain(s, &err);
    pa_simple_free(s);
    goVTLog((char*)"PulseAudio: audio stream closed");
}

void platform_ar_decode(const opus_int16 *pcm_data, int byte_count, int samples) {
    (void)samples;
    pthread_mutex_lock(&g_pa_mu);
    pa_simple *s = g_pa_s;
    pthread_mutex_unlock(&g_pa_mu);
    if (!s) return;
    int err = 0;
    if (pa_simple_write(s, pcm_data, (size_t)byte_count, &err) < 0) {
        // The PulseAudio stream can end up in a broken/corked state after a
        // prolonged network stall drains it; pa_simple never recovers on its
        // own, so every subsequent write would silently no-op forever and
        // audio would stay dead even after the network (and video) recover.
        // Recreate the stream from scratch to get a fresh playback timeline.
        char msg[128];
        snprintf(msg, sizeof(msg), "PulseAudio: write failed (%s), reopening stream", pa_strerror(err));
        goVTLog(msg);
        pthread_mutex_lock(&g_pa_mu);
        if (g_pa_s == s) {
            g_pa_s = NULL;
        }
        pthread_mutex_unlock(&g_pa_mu);
        pa_simple_free(s);
        platform_ar_init(g_pa_channels, g_pa_rate);
    }
}

// ═══════════════════════════════════════════════════════════════════════════════
// libavcodec H.264 decoder — platform video implementation
// Hardware acceleration order: VA-API (Intel/AMD) → NVDEC (NVIDIA) → software
// ═══════════════════════════════════════════════════════════════════════════════

static AVCodecContext    *g_avctx        = NULL;
static struct SwsContext *g_sws          = NULL;
static AVBufferRef       *g_hw_dev_ctx   = NULL;
static enum AVPixelFormat g_hw_pix_fmt   = AV_PIX_FMT_NONE;
static int                g_av_w         = 0;
static int                g_av_h         = 0;
static pthread_mutex_t    g_av_mu        = PTHREAD_MUTEX_INITIALIZER;
static uint64_t           g_av_frame_cnt = 0;

// Codec negotiated by moonlight-common-c for the current session, set by
// platform_set_video_format (called from dr_setup with NegotiatedVideoFormat).
// VIDEO_FORMAT_* bitmask from Limelight.h: 0x0001=H264, 0x0100=H265/HEVC,
// 0x1000=AV1_MAIN8. linux_av_init() reads this to pick a matching decoder --
// it used to be ignored entirely (avcodec_find_decoder_by_name only ever
// tried h264_vaapi/h264_nvdec, hardcoded AV_CODEC_ID_H264 as the software
// fallback), so an HEVC/AV1 session got HEVC/AV1 bitstream fed into an H264
// decoder and silently produced no frames.
static int g_video_format = 0x0001;

static enum AVPixelFormat av_get_hw_format_cb(AVCodecContext *ctx,
                                               const enum AVPixelFormat *fmts) {
    (void)ctx;
    for (const enum AVPixelFormat *p = fmts; *p != AV_PIX_FMT_NONE; p++) {
        if (*p == g_hw_pix_fmt) return *p;
    }
    return AV_PIX_FMT_NONE;
}

// Try to create a hardware decoder; returns codec on success, NULL on failure.
static const AVCodec *try_hw_decoder(const char *name,
                                      enum AVHWDeviceType hw_type,
                                      enum AVPixelFormat  hw_fmt) {
    const AVCodec *codec = avcodec_find_decoder_by_name(name);
    if (!codec) return NULL;

    AVBufferRef *hw_ctx = NULL;
    if (av_hwdevice_ctx_create(&hw_ctx, hw_type, NULL, NULL, 0) < 0) return NULL;

    // Quick open-and-close test.
    AVCodecContext *test = avcodec_alloc_context3(codec);
    test->hw_device_ctx = av_buffer_ref(hw_ctx);
    g_hw_pix_fmt = hw_fmt;
    test->get_format = av_get_hw_format_cb;
    int ok = (avcodec_open2(test, codec, NULL) == 0);
    avcodec_free_context(&test);
    if (!ok) { av_buffer_unref(&hw_ctx); return NULL; }

    if (g_hw_dev_ctx) av_buffer_unref(&g_hw_dev_ctx);
    g_hw_dev_ctx = hw_ctx;
    return codec;
}

static void linux_av_init(void) {
    if (g_avctx) return;

    // Pick the decoder family to match what was actually negotiated for
    // this session (g_video_format, set by platform_set_video_format).
    const char *hw_vaapi_name, *hw_nvdec_name;
    enum AVCodecID sw_id;
    const char *codec_label;
    if (g_video_format & 0x0F00) { // VIDEO_FORMAT_MASK_H265
        hw_vaapi_name = "hevc_vaapi"; hw_nvdec_name = "hevc_nvdec"; sw_id = AV_CODEC_ID_HEVC; codec_label = "hevc";
    } else if (g_video_format & 0xF000) { // VIDEO_FORMAT_MASK_AV1
        hw_vaapi_name = "av1_vaapi"; hw_nvdec_name = "av1_nvdec"; sw_id = AV_CODEC_ID_AV1; codec_label = "av1";
    } else {
        hw_vaapi_name = "h264_vaapi"; hw_nvdec_name = "h264_nvdec"; sw_id = AV_CODEC_ID_H264; codec_label = "h264";
    }

    const struct { const char *name; enum AVHWDeviceType type; enum AVPixelFormat fmt; } hw[] = {
        { hw_vaapi_name, AV_HWDEVICE_TYPE_VAAPI, AV_PIX_FMT_VAAPI },
        { hw_nvdec_name, AV_HWDEVICE_TYPE_CUDA,  AV_PIX_FMT_CUDA  },
    };

    const AVCodec *codec = NULL;
    for (int i = 0; i < (int)(sizeof(hw)/sizeof(hw[0])); i++) {
        codec = try_hw_decoder(hw[i].name, hw[i].type, hw[i].fmt);
        if (codec) {
            // Log which HW path was selected.
            char msg[64];
            snprintf(msg, sizeof(msg), "libavcodec: using %s", hw[i].name);
            goVTLog(msg);
            break;
        }
    }
    if (!codec) {
        codec = avcodec_find_decoder(sw_id);
        g_hw_pix_fmt = AV_PIX_FMT_NONE;
        char msg[80];
        snprintf(msg, sizeof(msg), "libavcodec: %s software fallback (no VA-API/NVDEC found)", codec_label);
        goVTLog(msg);
    }
    if (!codec) {
        char msg[64];
        snprintf(msg, sizeof(msg), "libavcodec: no decoder available for %s", codec_label);
        goVTLog(msg);
        return;
    }

    g_avctx = avcodec_alloc_context3(codec);
    if (g_hw_dev_ctx) {
        g_avctx->hw_device_ctx = av_buffer_ref(g_hw_dev_ctx);
        g_avctx->get_format    = av_get_hw_format_cb;
    }
    if (avcodec_open2(g_avctx, codec, NULL) < 0) {
        avcodec_free_context(&g_avctx);
        goVTLog((char*)"libavcodec: avcodec_open2 FAILED");
    }
}

static void linux_av_teardown(void) {
    if (g_sws)       { sws_freeContext(g_sws); g_sws = NULL; }
    if (g_avctx)     { avcodec_free_context(&g_avctx); }
    if (g_hw_dev_ctx){ av_buffer_unref(&g_hw_dev_ctx); }
    g_hw_pix_fmt  = AV_PIX_FMT_NONE;
    g_av_frame_cnt = 0;
}

// Frame dump for the netem/packet-loss diagnostic harness: opt-in via
// USBRIDGE_FRAME_DUMP_DIR (unset in normal client runs, so this never touches
// disk otherwise). Writes raw P6 PPM (no PNG encoder linked into this binary)
// every dump_every_n-th frame -- close enough together to catch corruption
// on the P-frames immediately following a keyframe, not just the keyframe
// itself, which is where the FEC-block-loss bug upstream is documented to
// show up.
static void maybe_dump_frame_ppm(const uint8_t *rgba, int w, int h) {
    static const char *dump_dir = NULL;
    static int dump_dir_checked = 0;
    static int dump_every_n = 15;
    if (!dump_dir_checked) {
        dump_dir_checked = 1;
        dump_dir = getenv("USBRIDGE_FRAME_DUMP_DIR");
        const char *n = getenv("USBRIDGE_FRAME_DUMP_EVERY_N");
        if (n && atoi(n) > 0) dump_every_n = atoi(n);
    }
    if (!dump_dir) return;
    if (g_av_frame_cnt % (uint64_t)dump_every_n != 0) return;

    char path[512];
    snprintf(path, sizeof(path), "%s/frame_%06llu.ppm", dump_dir, (unsigned long long)g_av_frame_cnt);
    FILE *f = fopen(path, "wb");
    if (!f) return;
    fprintf(f, "P6\n%d %d\n255\n", w, h);
    // RGBA -> RGB, dropping alpha (PPM has no alpha channel).
    uint8_t *row = (uint8_t *)malloc((size_t)w * 3);
    if (row) {
        for (int y = 0; y < h; y++) {
            const uint8_t *src = rgba + (size_t)y * w * 4;
            for (int x = 0; x < w; x++) {
                row[x * 3 + 0] = src[x * 4 + 0];
                row[x * 3 + 1] = src[x * 4 + 1];
                row[x * 3 + 2] = src[x * 4 + 2];
            }
            fwrite(row, 1, (size_t)w * 3, f);
        }
        free(row);
    }
    fclose(f);
}

static void deliver_frame(AVFrame *frame) {
    AVFrame *sw = NULL;
    if (frame->format == AV_PIX_FMT_VAAPI || frame->format == AV_PIX_FMT_CUDA) {
        sw = av_frame_alloc();
        if (av_hwframe_transfer_data(sw, frame, 0) < 0) { av_frame_free(&sw); return; }
        sw->width = frame->width; sw->height = frame->height;
        frame = sw;
    }

    int w = frame->width, h = frame->height;
    if (!g_sws || w != g_av_w || h != g_av_h) {
        if (g_sws) sws_freeContext(g_sws);
        g_sws = sws_getContext(w, h, (enum AVPixelFormat)frame->format,
                               w, h, AV_PIX_FMT_RGBA, SWS_BILINEAR, NULL, NULL, NULL);
        g_av_w = w; g_av_h = h;
    }
    if (g_sws) {
        uint8_t *rgba = (uint8_t *)malloc((size_t)w * (size_t)h * 4);
        if (rgba) {
            uint8_t *dst[4]   = { rgba, NULL, NULL, NULL };
            int dst_stride[4] = { w * 4, 0, 0, 0 };
            sws_scale(g_sws, (const uint8_t *const *)frame->data, frame->linesize,
                      0, h, dst, dst_stride);
            if (++g_av_frame_cnt == 1)
                goVTLog((char*)"libavcodec: first RGBA frame decoded");
            maybe_dump_frame_ppm(rgba, w, h);
            // Native overlay fast path: VK preferred, GL fallback.
            // goVTFrame is still called so Go-side stats/callbacks run.
            if (vk_video_is_active())
                vk_video_try_submit(rgba, w, h, w * 4);
            else if (gl_video_is_active())
                gl_video_try_submit(rgba, w, h, w * 4);
            goVTFrame(rgba, w, h, w * 4);
            free(rgba);
        }
    }
    if (sw) av_frame_free(&sw);
}

// platform_post_stop: no session state to tear down on Linux.
void platform_post_stop(void) {}

// platform_set_video_format records the negotiated codec so linux_av_init()
// can pick a matching decoder (see g_video_format's comment above) --
// libavcodec's decoder selection here is by explicit codec name/ID, not
// bitstream auto-detection, so this can't be skipped.
void platform_set_video_format(int videoFormat) {
    g_video_format = videoFormat ? videoFormat : 0x0001;
}

// Real network frame-arrival cadence, independent of decode speed --
// du->receiveTimeUs is stamped by moonlight-common-c when the first packet
// of this frame arrived off the wire, so this measures actual RTP jitter,
// unlike decoded-frame timing (which conflates it with local decode-side
// slowness, e.g. software H.264 fallback when no VAAPI/NVDEC device is
// available). Opt-in via USBRIDGE_LOG_FRAME_JITTER so it never runs in
// normal client builds.
static void maybe_log_frame_jitter(uint64_t receive_time_us) {
    static int enabled = -1;
    static uint64_t prev_us = 0;
    static double min_ms = 1e18, max_ms = -1e18;
    static int count = 0;
    if (enabled < 0) enabled = getenv("USBRIDGE_LOG_FRAME_JITTER") ? 1 : 0;
    if (!enabled) return;

    if (prev_us != 0) {
        double delta_ms = (double)(receive_time_us - prev_us) / 1000.0;
        if (delta_ms < min_ms) min_ms = delta_ms;
        if (delta_ms > max_ms) max_ms = delta_ms;
        count++;
        if (count >= 60) {
            char msg[128];
            snprintf(msg, sizeof(msg), "frame arrival cadence: min=%.1fms max=%.1fms jitter=%.1fms over %d frames",
                     min_ms, max_ms, max_ms - min_ms, count);
            goVTLog(msg);
            min_ms = 1e18; max_ms = -1e18; count = 0;
        }
    }
    prev_us = receive_time_us;
}

int platform_dr_submit(PDECODE_UNIT du) {
    maybe_log_frame_jitter(du->receiveTimeUs);

    // USBRIDGE_SKIP_DECODE: measure true producer-side (server encode/send)
    // pacing, unpolluted by this harness's own decode speed. With
    // CAPABILITY_DIRECT_SUBMIT, dr_submit runs synchronously on the RTP
    // receive thread -- a slow consumer here delays draining the socket,
    // which delays du->receiveTimeUs on the *next* frame too, making the
    // receive thread's own slowness look like server-side jitter. Skipping
    // the decode entirely makes this the fastest possible consumer, so
    // receiveTimeUs deltas reflect genuine wire arrival timing.
    static int skip_decode = -1;
    if (skip_decode < 0) skip_decode = getenv("USBRIDGE_SKIP_DECODE") ? 1 : 0;
    if (skip_decode) return DR_OK;

    pthread_mutex_lock(&g_av_mu);
    if (!g_avctx) linux_av_init();
    AVCodecContext *ctx = g_avctx;
    pthread_mutex_unlock(&g_av_mu);
    if (!ctx) return DR_NEED_IDR;

    int total = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) total += e->length;
    if (total <= 0) return DR_OK;

    uint8_t *data = (uint8_t *)av_malloc(total + AV_INPUT_BUFFER_PADDING_SIZE);
    if (!data) return DR_NEED_IDR;
    memset(data + total, 0, AV_INPUT_BUFFER_PADDING_SIZE);
    int off = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) {
        memcpy(data + off, e->data, e->length); off += e->length;
    }

    AVPacket *pkt = av_packet_alloc();
    pkt->data = data; pkt->size = total;
    int ret = avcodec_send_packet(ctx, pkt);
    av_packet_free(&pkt);
    av_free(data);
    if (ret < 0 && ret != AVERROR(EAGAIN)) return DR_NEED_IDR;

    AVFrame *frame = av_frame_alloc();
    while (avcodec_receive_frame(ctx, frame) == 0) {
        deliver_frame(frame);
        av_frame_unref(frame);
    }
    av_frame_free(&frame);
    return DR_OK;
}
*/
import "C"
