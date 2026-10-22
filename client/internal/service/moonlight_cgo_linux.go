//go:build linux && !android && cgo

package service

/*
#cgo pkg-config: opus openssl libavcodec libavutil libswscale libpulse-simple libva libva-drm
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build -L${SRCDIR}/../../moonlight-common-c/build/enet -lmoonlight-common-c -lenet
#cgo LDFLAGS: -lpthread -lm -ldl
// PyroWave decode (pyrowave_decode_linux.c): one static archive from scripts/build_pyrowave.sh
// that exports only the pyrowave_* C API.
#cgo CFLAGS: -I${SRCDIR}/../../third_party/pyrowave/vendor/pyrowave
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/pyrowave/build -lusbridge-pyrowave -lstdc++ -lm

#include <libavcodec/avcodec.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_vaapi.h>
#include <libavutil/frame.h>
#include <libavutil/imgutils.h>
#include <libavutil/pixdesc.h>
#include <libswscale/swscale.h>
#include <pulse/simple.h>
#include <pulse/error.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <va/va.h>
#include <va/va_drmcommon.h>

// Go callbacks (defined in moonlight_cgo_wrapper.go).
extern void goMoonlightStage(int stage, int result, int errCode);
extern void goMoonlightConnected(void);
extern void goMoonlightTerminated(int errCode);
extern void goMoonlightRumble(unsigned short controllerNumber, unsigned short lowFreq, unsigned short highFreq);
extern void goVTLog(char *msg);
extern void goVTFrame(uint8_t *rgba, int width, int height, int stride);
extern void goVideoFormatNegotiated(int videoFormat);
extern void goAIVisionOverlay(uint8_t *rgba, int width, int height, int stride);
extern void goNetGraphOverlay(uint8_t *rgba, int width, int height, int stride);
extern int  goAIVisionShouldSample(void);
extern void goAIVisionSample(uint8_t *rgba, int width, int height, int stride);

// GL overlay fast path (defined in gl_video_impl_linux.c).
extern int gl_video_is_active(void);
extern int gl_video_try_submit(uint8_t *rgba, int width, int height, int stride);

// Vulkan overlay fast path (defined in vk_video_impl_linux.c).
extern int vk_video_is_active(void);
extern int vk_video_try_submit(uint8_t *rgba, int width, int height, int stride);

// Vulkan zero-copy dma-buf fast path (defined in vk_video_impl_linux.c) --
// see deliver_frame's map_to_vaapi_frame/dma-buf-export block below for the
// full chain this feeds into.
extern int vk_video_zerocopy_supported(void);
// NV12 upload path (vk_video_impl_linux.c): Y + interleaved UV planes from
// system memory, converted to RGB on the GPU by the same YCbCr sampler the
// dma-buf path uses. Used for NVDEC (CUDA) and software decode.
extern int vk_video_nv12_supported(void);
extern int vk_video_try_submit_nv12(const uint8_t *y, int y_stride,
                                     const uint8_t *uv, int uv_stride,
                                     int width, int height);
extern void vk_video_set_nv12_bt709(int bt709);
// PyroWave decode (pyrowave_decode_linux.c).
extern size_t pyrowave_linux_unframe(const uint8_t *in, size_t len, uint8_t *out);
// Zero-copy GPU decode in the Vulkan renderer (vk_video_impl_linux.c).
extern int vk_video_pyrowave_gpu(void);
extern int vk_video_try_submit_pyrowave(const uint8_t *au, size_t len);
extern uint8_t *pyrowave_linux_slot_begin(size_t total);
extern void pyrowave_linux_slot_commit(size_t total);
extern void pyrowave_linux_teardown(void);
extern uint64_t pyrowave_linux_frames(void);
extern int vk_video_try_submit_dmabuf(int fd, uint64_t modifier, int surf_w, int surf_h,
                                       int vis_w, int vis_h, uint32_t plane_count,
                                       uint32_t offset0, uint32_t pitch0,
                                       uint32_t offset1, uint32_t pitch1,
                                       void *release_ctx, void (*release_fn)(void*));

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
    // Target latency: the slider's prebuffer (audio_shed.h), at least 50 ms --
    // low enough for real-time feel, large enough to absorb per-frame jitter
    // without underruns.
    int target_ms = audio_prebuffer_ms() > 50 ? audio_prebuffer_ms() : 50;
    pa_buffer_attr attr = {
        .maxlength = (uint32_t)-1,
        .tlength   = (uint32_t)(sample_rate * channels * 2 * target_ms / 1000),
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
    // See audio_shed.h; pa_simple_get_latency is what's still to be played.
    pa_usec_t latency = pa_simple_get_latency(s, &err);
    if (latency != (pa_usec_t)-1 && audio_should_shed((int64_t)(latency * (pa_usec_t)g_pa_rate / 1000000), g_pa_rate, 0)) return;
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
static struct SwsContext *g_sws_nv12     = NULL; // sw-decode yuv -> NV12 for the Vulkan NV12 upload path
static int                g_sws_nv12_w = 0, g_sws_nv12_h = 0, g_sws_nv12_fmt = -1;
static AVFrame           *g_nv12_scratch = NULL;
static AVBufferRef       *g_hw_dev_ctx   = NULL;
static enum AVPixelFormat g_hw_pix_fmt   = AV_PIX_FMT_NONE;
static int                g_av_w         = 0;
static int                g_av_h         = 0;
static pthread_mutex_t    g_av_mu        = PTHREAD_MUTEX_INITIALIZER;
static uint64_t           g_av_frame_cnt = 0;
static uint64_t           g_av_zc_frame_cnt = 0;
static uint64_t           g_av_nv12_frame_cnt = 0;

// Native VAAPI device, created once and kept as the parent for deriving the
// QSV device (see try_hw_decoder) -- this is what makes the zero-copy path
// possible at all: on Linux, QSV's decoded surfaces ARE VAAPI surfaces
// under the hood (libmfx-gen implements oneVPL on top of VAAPI here), so
// deriving QSV FROM a VAAPI device we already hold onto lets us later map
// any QSV AVFrame back to that same VAAPI surface (av_hwframe_map) and
// export it as a dma-buf (vaExportSurfaceHandle) with zero CPU copies.
// Verified this exact chain end-to-end standalone before wiring it in here.
static AVBufferRef *g_vaapi_dev_ctx    = NULL;
// Derived VAAPI frames ctx over the active decoder's frame pool -- recreated
// whenever the coded resolution changes (mirrors g_sws's w/h-keyed cache).
static AVBufferRef *g_vaapi_frames_ctx = NULL;
static int          g_vaapi_frames_w = 0, g_vaapi_frames_h = 0;

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

// ensure_vaapi_device creates the shared native VAAPI device on first use.
// Kept alive across hw-decoder probing/sessions (torn down only in
// linux_av_teardown) so its VADisplay stays valid for as long as the
// zero-copy path might need it.
static int ensure_vaapi_device(void) {
    if (g_vaapi_dev_ctx) return 1;
    return av_hwdevice_ctx_create(&g_vaapi_dev_ctx, AV_HWDEVICE_TYPE_VAAPI, NULL, NULL, 0) >= 0;
}

// open_hw_device creates (or refs) the device ctx a given hw type needs.
// VAAPI/QSV share g_vaapi_dev_ctx -- see its doc comment above.
static AVBufferRef *open_hw_device(enum AVHWDeviceType hw_type) {
    AVBufferRef *hw_ctx = NULL;
    if (hw_type == AV_HWDEVICE_TYPE_QSV) {
        // Derive from the shared VAAPI device (not a standalone QSV device)
        // so its surfaces stay mappable back to VAAPI later -- see
        // g_vaapi_dev_ctx's doc comment above.
        if (!ensure_vaapi_device()) return NULL;
        if (av_hwdevice_ctx_create_derived(&hw_ctx, AV_HWDEVICE_TYPE_QSV, g_vaapi_dev_ctx, 0) < 0)
            return NULL;
    } else if (hw_type == AV_HWDEVICE_TYPE_VAAPI) {
        if (!ensure_vaapi_device()) return NULL;
        hw_ctx = av_buffer_ref(g_vaapi_dev_ctx);
    } else if (av_hwdevice_ctx_create(&hw_ctx, hw_type, NULL, NULL, 0) < 0) {
        return NULL;
    }
    return hw_ctx;
}

// try_hw_decoder opens `codec` against a hw device of hw_type and returns 1
// if a test open succeeded (g_hw_dev_ctx/g_hw_pix_fmt are then set for the
// real open in linux_av_init). The test open alone doesn't prove the GPU can
// actually decode this codec/profile -- linux_av_init's first-frames check
// (g_hw_verify_*) catches that and drops to the next candidate.
static int try_hw_decoder(const AVCodec *codec, enum AVHWDeviceType hw_type,
                          enum AVPixelFormat hw_fmt) {
    if (!codec) return 0;
    AVBufferRef *hw_ctx = open_hw_device(hw_type);
    if (!hw_ctx) return 0;

    AVCodecContext *test = avcodec_alloc_context3(codec);
    test->hw_device_ctx = av_buffer_ref(hw_ctx);
    g_hw_pix_fmt = hw_fmt;
    test->get_format = av_get_hw_format_cb;
    int ok = (avcodec_open2(test, codec, NULL) == 0);
    avcodec_free_context(&test);
    if (!ok) { av_buffer_unref(&hw_ctx); return 0; }

    if (g_hw_dev_ctx) av_buffer_unref(&g_hw_dev_ctx);
    g_hw_dev_ctx = hw_ctx;
    return 1;
}

// hwaccel_supported reports whether the NATIVE decoder `codec` exposes a
// hw_device_ctx-based hwaccel for hw_type (and which pix_fmt it outputs).
static int hwaccel_supported(const AVCodec *codec, enum AVHWDeviceType hw_type,
                             enum AVPixelFormat *out_fmt) {
    if (!codec) return 0;
    for (int i = 0;; i++) {
        const AVCodecHWConfig *cfg = avcodec_get_hw_config(codec, i);
        if (!cfg) return 0;
        if (cfg->device_type == hw_type && (cfg->methods & AV_CODEC_HW_CONFIG_METHOD_HW_DEVICE_CTX)) {
            *out_fmt = cfg->pix_fmt;
            return 1;
        }
    }
}

// Decoder candidates, in preference order. VAAPI and NVDEC(CUDA) are NOT
// separate libavcodec decoders -- they're hwaccels of the native h264/hevc/
// av1 decoder, selected by attaching a hw_device_ctx and answering
// get_format with the hw pix_fmt. (This file used to look them up as
// decoders named "h264_vaapi"/"h264_nvdec", which don't exist in any
// FFmpeg build -- so on every non-Intel GPU, e.g. NVIDIA, decode silently
// fell back to single-threaded software: ~20-30ms per 2560x1600 HEVC frame
// on the receive thread, i.e. <=30fps plus socket-drain stalls. That was
// the "choppy vs stock Moonlight" bug.) QSV, by contrast, IS a separate
// named decoder (h264_qsv...).
enum { HWC_VAAPI = 0, HWC_CUDA, HWC_QSV, HWC_COUNT };
static int g_hw_skip[HWC_COUNT];   // candidate proved broken this process (see g_hw_verify_*)
static int g_hw_active = -1;       // candidate g_avctx was opened with, -1 = software
static int g_hw_verify_pkts = 0;   // packets sent since open with zero frames out
static int g_hw_verify_ok = 0;

static void linux_av_init(void) {
    if (g_avctx) return;

    // Pick the decoder family to match what was actually negotiated for
    // this session (g_video_format, set by platform_set_video_format).
    const char *hw_qsv_name;
    enum AVCodecID sw_id;
    const char *codec_label;
    if (g_video_format & 0x0F00) { // VIDEO_FORMAT_MASK_H265
        hw_qsv_name = "hevc_qsv"; sw_id = AV_CODEC_ID_HEVC; codec_label = "hevc";
    } else if (g_video_format & 0xF000) { // VIDEO_FORMAT_MASK_AV1
        hw_qsv_name = "av1_qsv"; sw_id = AV_CODEC_ID_AV1; codec_label = "av1";
    } else {
        hw_qsv_name = "h264_qsv"; sw_id = AV_CODEC_ID_H264; codec_label = "h264";
    }
    // Native decoder (not libdav1d/libaom for AV1: those have no hwaccel).
    const AVCodec *native = NULL;
    if (sw_id == AV_CODEC_ID_AV1) native = avcodec_find_decoder_by_name("av1");
    if (!native) native = avcodec_find_decoder(sw_id);

    // USBRIDGE_HWDEC=sw|vaapi|cuda|qsv forces one path (diagnostics).
    const char *force = getenv("USBRIDGE_HWDEC");

    const AVCodec *codec = NULL;
    g_hw_active = -1;
    for (int i = 0; i < HWC_COUNT && !codec; i++) {
        if (g_hw_skip[i]) continue;
        static const char *names[HWC_COUNT] = { "vaapi", "cuda", "qsv" };
        if (force && strcmp(force, names[i]) != 0) continue;
        enum AVPixelFormat fmt = AV_PIX_FMT_NONE;
        const AVCodec *c = NULL;
        enum AVHWDeviceType type;
        if (i == HWC_QSV) {
            c = avcodec_find_decoder_by_name(hw_qsv_name);
            type = AV_HWDEVICE_TYPE_QSV; fmt = AV_PIX_FMT_QSV;
        } else {
            type = (i == HWC_VAAPI) ? AV_HWDEVICE_TYPE_VAAPI : AV_HWDEVICE_TYPE_CUDA;
            if (hwaccel_supported(native, type, &fmt)) c = native;
        }
        if (c && try_hw_decoder(c, type, fmt)) {
            codec = c;
            g_hw_active = i;
            char msg[96];
            snprintf(msg, sizeof(msg), "libavcodec: using %s via %s", c->name, names[i]);
            goVTLog(msg);
        }
    }
    if (!codec) {
        if (g_hw_dev_ctx) av_buffer_unref(&g_hw_dev_ctx);
        codec = native;
        g_hw_pix_fmt = AV_PIX_FMT_NONE;
        char msg[96];
        snprintf(msg, sizeof(msg), "libavcodec: %s software fallback (no VA-API/NVDEC/QSV usable)", codec_label);
        goVTLog(msg);
    }
    if (!codec) {
        char msg[64];
        snprintf(msg, sizeof(msg), "libavcodec: no decoder available for %s", codec_label);
        goVTLog(msg);
        return;
    }

    g_avctx = avcodec_alloc_context3(codec);
    // Streaming: output every frame as soon as it's decodable, never hold
    // one back for reordering (the stream has no B-frames).
    g_avctx->flags  |= AV_CODEC_FLAG_LOW_DELAY;
    g_avctx->flags2 |= AV_CODEC_FLAG2_FAST;
    if (g_hw_dev_ctx) {
        g_avctx->hw_device_ctx = av_buffer_ref(g_hw_dev_ctx);
        g_avctx->get_format    = av_get_hw_format_cb;
    } else {
        // Software: slice threading only -- frame threading would add
        // (threads-1) frames of latency.
        g_avctx->thread_type  = FF_THREAD_SLICE;
        g_avctx->thread_count = 0; // auto
    }
    g_hw_verify_pkts = 0;
    g_hw_verify_ok = (g_hw_active < 0);
    if (avcodec_open2(g_avctx, codec, NULL) < 0) {
        avcodec_free_context(&g_avctx);
        goVTLog((char*)"libavcodec: avcodec_open2 FAILED");
    }
}

static void linux_av_teardown(void) {
    if (g_sws)       { sws_freeContext(g_sws); g_sws = NULL; }
    if (g_sws_nv12)  { sws_freeContext(g_sws_nv12); g_sws_nv12 = NULL; }
    g_hw_active = -1;
    if (g_avctx)     { avcodec_free_context(&g_avctx); }
    if (g_hw_dev_ctx){ av_buffer_unref(&g_hw_dev_ctx); }
    if (g_vaapi_frames_ctx) { av_buffer_unref(&g_vaapi_frames_ctx); }
    if (g_vaapi_dev_ctx)    { av_buffer_unref(&g_vaapi_dev_ctx); }
    g_vaapi_frames_w = 0; g_vaapi_frames_h = 0;
    g_hw_pix_fmt  = AV_PIX_FMT_NONE;
    g_av_frame_cnt = 0;
    g_av_zc_frame_cnt = 0;
    g_av_nv12_frame_cnt = 0;
    av_frame_free(&g_nv12_scratch);
    g_sws_nv12_w = g_sws_nv12_h = 0; g_sws_nv12_fmt = -1;
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

// release_vaframe_cb is the release_fn handed to vk_video_try_submit_dmabuf:
// it runs on the Vulkan render thread once that frame's GPU read has
// retired, and simply drops our AVFrame ref -- letting VAAPI/libmfx-gen
// reclaim that decode surface for reuse. ctx is the AVFrame* itself.
static void release_vaframe_cb(void *ctx) {
    AVFrame *f = (AVFrame *)ctx;
    av_frame_free(&f);
}

// map_to_vaapi_frame obtains a VAAPI-backed AVFrame (data[3] = VASurfaceID)
// for a decoded frame with zero pixel copies: if it's already
// AV_PIX_FMT_VAAPI (a real vaapi decoder, when one exists), it's just
// ref'd; if AV_PIX_FMT_QSV, it's mapped via a VAAPI frames ctx derived from
// the shared g_vaapi_dev_ctx (see that global's doc comment). On success
// (*out_vaframe is non-NULL, caller must eventually av_frame_free it, e.g.
// via release_vaframe_cb) *out_dpy is set to the VADisplay to export from.
static int map_to_vaapi_frame(AVFrame *frame, AVFrame **out_vaframe, VADisplay *out_dpy) {
    *out_vaframe = NULL;
    if (!g_vaapi_dev_ctx) return 0;
    AVHWDeviceContext *devctx = (AVHWDeviceContext *)g_vaapi_dev_ctx->data;
    *out_dpy = ((AVVAAPIDeviceContext *)devctx->hwctx)->display;

    if (frame->format == AV_PIX_FMT_VAAPI) {
        AVFrame *ref = av_frame_alloc();
        if (!ref) return 0;
        if (av_frame_ref(ref, frame) < 0) { av_frame_free(&ref); return 0; }
        *out_vaframe = ref;
        return 1;
    }
    if (frame->format != AV_PIX_FMT_QSV) return 0;

    if (!g_vaapi_frames_ctx || g_vaapi_frames_w != frame->width || g_vaapi_frames_h != frame->height) {
        if (g_vaapi_frames_ctx) av_buffer_unref(&g_vaapi_frames_ctx);
        if (av_hwframe_ctx_create_derived(&g_vaapi_frames_ctx, AV_PIX_FMT_VAAPI,
                                           g_vaapi_dev_ctx, frame->hw_frames_ctx,
                                           AV_HWFRAME_MAP_READ) < 0) {
            g_vaapi_frames_ctx = NULL;
            g_vaapi_frames_w = g_vaapi_frames_h = 0;
            return 0;
        }
        g_vaapi_frames_w = frame->width; g_vaapi_frames_h = frame->height;
    }

    AVFrame *vaframe = av_frame_alloc();
    if (!vaframe) return 0;
    vaframe->hw_frames_ctx = av_buffer_ref(g_vaapi_frames_ctx);
    vaframe->format = AV_PIX_FMT_VAAPI;
    if (av_hwframe_map(vaframe, frame, AV_HWFRAME_MAP_READ) < 0) {
        av_frame_free(&vaframe);
        return 0;
    }
    *out_vaframe = vaframe;
    return 1;
}

// try_deliver_zerocopy attempts the full VAAPI/QSV -> dma-buf -> Vulkan
// path for one decoded frame. Returns 1 if it fully handled the frame
// (ownership of everything it touched has moved on: either to the Vulkan
// renderer's deferred-release mechanism, or released right here on a
// failure branch) -- deliver_frame must not fall through to its CPU path
// for this frame when this returns 1. Returns 0 if zero-copy doesn't apply
// (not a QSV/VAAPI frame, Vulkan not active/capable, or any step up to and
// including handing the fd to Vulkan failed) -- frame is untouched, caller
// proceeds with the existing hwframe_transfer + sws_scale path.
// sample_frame_for_ai_vision does the rare CPU readback AI Vision's detector
// needs (goAIVisionShouldSample gates it to the ~2Hz frames the detector is
// actually due on, so the zero-copy path stays zero-copy otherwise). The
// buffer is throwaway scratch -- boxes are drawn on the GPU by the native
// overlay layer, never into this.
static void sample_frame_for_ai_vision(AVFrame *frame) {
    AVFrame *sw = av_frame_alloc();
    if (!sw) return;
    int got = frame->hw_frames_ctx ? (av_hwframe_transfer_data(sw, frame, 0) == 0)
                                   : (av_frame_ref(sw, frame) == 0);
    if (got) {
        int w = frame->width, h = frame->height;
        if (!g_sws || w != g_av_w || h != g_av_h) {
            if (g_sws) sws_freeContext(g_sws);
            g_sws = sws_getContext(w, h, (enum AVPixelFormat)sw->format,
                                   w, h, AV_PIX_FMT_RGBA, SWS_BILINEAR, NULL, NULL, NULL);
            g_av_w = w; g_av_h = h;
        }
        uint8_t *rgba = g_sws ? (uint8_t *)malloc((size_t)w * (size_t)h * 4) : NULL;
        if (rgba) {
            uint8_t *dst[4]   = { rgba, NULL, NULL, NULL };
            int dst_stride[4] = { w * 4, 0, 0, 0 };
            sws_scale(g_sws, (const uint8_t *const *)sw->data, sw->linesize, 0, h, dst, dst_stride);
            goAIVisionSample(rgba, w, h, w * 4);
            free(rgba);
        }
    }
    av_frame_free(&sw);
}

static int try_deliver_zerocopy(AVFrame *frame) {
    if (!vk_video_is_active() || !vk_video_zerocopy_supported()) return 0;
    if (frame->format != AV_PIX_FMT_QSV && frame->format != AV_PIX_FMT_VAAPI) return 0;

    if (goAIVisionShouldSample()) sample_frame_for_ai_vision(frame);

    AVFrame *vaframe = NULL;
    VADisplay dpy = NULL;
    if (!map_to_vaapi_frame(frame, &vaframe, &dpy)) return 0;

    VASurfaceID surf = (VASurfaceID)(uintptr_t)vaframe->data[3];
    // CPU-side sync with the decode job -- see vk_video_impl_linux.c's
    // "Sync note" doc comment on why this replaces a GPU semaphore here.
    vaSyncSurface(dpy, surf);

    VADRMPRIMESurfaceDescriptor desc;
    memset(&desc, 0, sizeof(desc));
    VAStatus vs = vaExportSurfaceHandle(dpy, surf, VA_SURFACE_ATTRIB_MEM_TYPE_DRM_PRIME_2,
                                         VA_EXPORT_SURFACE_READ_ONLY | VA_EXPORT_SURFACE_SEPARATE_LAYERS,
                                         &desc);
    if (vs != VA_STATUS_SUCCESS) { av_frame_free(&vaframe); return 0; }

    // Only NV12 (8-bit 4:2:0) in a single dma-buf object is wired up on the
    // Vulkan side today (vk_video_impl_linux.c's fixed pipeline is
    // VK_FORMAT_G8_B8R8_2PLANE_420_UNORM only) -- anything else (P010 from
    // a Main10 stream, or a driver that hands back >1 dma-buf object) falls
    // back to the CPU path below rather than being force-fit.
    if (desc.fourcc != VA_FOURCC_NV12 || desc.num_objects != 1 || desc.num_layers != 2) {
        for (uint32_t i = 0; i < desc.num_objects; i++) close(desc.objects[i].fd);
        av_frame_free(&vaframe);
        return 0;
    }

    int ok = vk_video_try_submit_dmabuf(
        desc.objects[0].fd, desc.objects[0].drm_format_modifier,
        (int)desc.width, (int)desc.height, frame->width, frame->height,
        2,
        desc.layers[0].offset[0], desc.layers[0].pitch[0],
        desc.layers[1].offset[0], desc.layers[1].pitch[0],
        (void *)vaframe, release_vaframe_cb);
    if (!ok) {
        // vk_video_try_submit_dmabuf's contract: on failure it touched
        // neither the fd nor our AVFrame ref, so we still own both.
        close(desc.objects[0].fd);
        av_frame_free(&vaframe);
        return 0;
    }

    // Ownership of desc.objects[0].fd and vaframe has moved to the Vulkan
    // renderer; do not touch either again.
    if (++g_av_zc_frame_cnt == 1)
        goVTLog((char*)"libavcodec: first zero-copy frame (VAAPI/QSV dma-buf -> Vulkan, no CPU pixel copy)");
    // Matches the NULL-rgba convention other zero-copy platforms (Android's
    // AHardwareBuffer path, Metal on macOS/iOS) use to keep Go-side frame
    // counting/FPS stats alive with no CPU-readable pixel buffer to hand
    // over -- see goVTFrame's doc comment.
    goVTFrame(NULL, frame->width, frame->height, 0);
    return 1;
}

// try_deliver_nv12 is the fast path for everything the dma-buf zero-copy
// path can't take -- chiefly NVDEC (AV_PIX_FMT_CUDA) on NVIDIA, plus
// software decode: get the frame into system-memory NV12 (a ~1.5ms PCIe
// download for CUDA, or a cheap interleave for sw yuv420p) and hand the two
// planes to Vulkan, which does YCbCr->RGB on the GPU. Replaces the old
// RGBA path's sws_scale NV12->RGBA on the CPU (~13ms/frame at 2560x1600)
// plus its per-pixel R/B swizzle and several full-frame RGBA copies.
// Returns 1 if the frame was handled.
static int try_deliver_nv12(AVFrame *frame) {
    if (!vk_video_is_active() || !vk_video_nv12_supported()) return 0;

    AVFrame *src = frame;
    AVFrame *dl = NULL;
    if (frame->hw_frames_ctx) {
        dl = av_frame_alloc();
        if (!dl) return 0;
        dl->format = AV_PIX_FMT_NV12;
        if (av_hwframe_transfer_data(dl, frame, 0) < 0 || dl->format != AV_PIX_FMT_NV12) {
            av_frame_free(&dl); // e.g. P010 (10-bit) -- not wired up, RGBA path handles it
            return 0;
        }
        dl->width = frame->width; dl->height = frame->height;
        src = dl;
    } else if (frame->format != AV_PIX_FMT_NV12) {
        if (frame->format != AV_PIX_FMT_YUV420P && frame->format != AV_PIX_FMT_YUVJ420P) return 0;
        int w = frame->width, h = frame->height;
        if (!g_sws_nv12 || g_sws_nv12_w != w || g_sws_nv12_h != h || g_sws_nv12_fmt != frame->format) {
            if (g_sws_nv12) sws_freeContext(g_sws_nv12);
            g_sws_nv12 = sws_getContext(w, h, (enum AVPixelFormat)frame->format,
                                        w, h, AV_PIX_FMT_NV12, SWS_POINT, NULL, NULL, NULL);
            g_sws_nv12_w = w; g_sws_nv12_h = h; g_sws_nv12_fmt = frame->format;
            av_frame_free(&g_nv12_scratch);
        }
        if (!g_sws_nv12) return 0;
        if (!g_nv12_scratch) {
            g_nv12_scratch = av_frame_alloc();
            if (!g_nv12_scratch) return 0;
            g_nv12_scratch->format = AV_PIX_FMT_NV12;
            g_nv12_scratch->width = w; g_nv12_scratch->height = h;
            if (av_frame_get_buffer(g_nv12_scratch, 0) < 0) { av_frame_free(&g_nv12_scratch); return 0; }
        }
        sws_scale(g_sws_nv12, (const uint8_t *const *)frame->data, frame->linesize, 0, h,
                  g_nv12_scratch->data, g_nv12_scratch->linesize);
        src = g_nv12_scratch;
    }

    if (goAIVisionShouldSample()) sample_frame_for_ai_vision(src);

    int ok = vk_video_try_submit_nv12(src->data[0], src->linesize[0],
                                      src->data[1], src->linesize[1],
                                      frame->width, frame->height);
    if (dl) av_frame_free(&dl);
    if (!ok) return 0;
    if (++g_av_nv12_frame_cnt == 1) {
        char msg[128];
        snprintf(msg, sizeof(msg), "libavcodec: first NV12 frame -> Vulkan GPU YCbCr (%s, %dx%d)",
                 frame->hw_frames_ctx ? "hw download" : "sw decode", frame->width, frame->height);
        goVTLog(msg);
    }
    goVTFrame(NULL, frame->width, frame->height, 0);
    return 1;
}

static void deliver_frame(AVFrame *frame) {
    if (try_deliver_zerocopy(frame)) return;
    if (try_deliver_nv12(frame)) return;

    AVFrame *sw = NULL;
    if (frame->format == AV_PIX_FMT_VAAPI || frame->format == AV_PIX_FMT_CUDA ||
        frame->format == AV_PIX_FMT_QSV) {
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
            // AI Vision overlay: no-op unless the checkbox in the video
            // settings popup is on (checked internally, single atomic load
            // in the common case) -- burns detection boxes+ids into rgba
            // in place, before it reaches either native fast path.
            goAIVisionOverlay(rgba, w, h, w * 4);
            // Net Graph HUD: no-op unless the checkbox in the video
            // settings popup is on -- burns the cached HUD canvas into
            // rgba in place, bottom-right corner, same "before either
            // native fast path" ordering as AI Vision above so the HUD
            // reaches the actual displayed pixels regardless of which
            // path (VK/GL) ends up rendering them.
            goNetGraphOverlay(rgba, w, h, w * 4);
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

// platform_post_stop — called after LiStopConnection has joined every
// background thread, so no decoder thread can be mid-call here. Must tear
// down g_avctx: it was opened for whichever codec the PREVIOUS session
// negotiated (e.g. hevc_qsv), and platform_dr_submit only calls
// linux_av_init() when g_avctx is NULL -- without this reset, a codec/
// resolution switch on reconnect (g_video_format changes via
// platform_set_video_format, called before the next dr_setup) would keep
// feeding the new session's bitstream into the stale decoder for the old
// codec, which fails silently: avcodec_send_packet/receive_frame never
// errors loudly, it just never produces a frame again, so video goes dark
// and stays dark while the higher-level "stuck-no-frame" watchdog restarts
// the stream forever without ever recovering.
void platform_post_stop(void) {
    pthread_mutex_lock(&g_av_mu);
    linux_av_teardown();
    pthread_mutex_unlock(&g_av_mu);
    pyrowave_linux_teardown();
}

// platform_set_video_format records the negotiated codec so linux_av_init()
// can pick a matching decoder (see g_video_format's comment above) --
// libavcodec's decoder selection here is by explicit codec name/ID, not
// bitstream auto-detection, so this can't be skipped.
void platform_set_video_format(int videoFormat) {
    g_video_format = videoFormat ? videoFormat : 0x0001;
    // PyroWave hosts convert to BT.709; the H.26x/AV1 paths render as BT.601.
    vk_video_set_nv12_bt709((g_video_format & VIDEO_FORMAT_MASK_PYROWAVE) != 0);
}

// pyrowave_submit hands one PyroWave access unit to the decode thread
// (pyrowave_decode_linux.c); the frame reaches the renderer through
// pyrowave_linux_deliver below.
static int pyrowave_submit(PDECODE_UNIT du) {
    size_t total = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) total += (size_t)e->length;
    if (total == 0) return DR_OK;
    // Renderer decodes on the GPU (PyroWave shares its VkDevice): hand it the access unit's
    // bytes and nothing else -- no CPU decode, readback or upload.
    if (vk_video_pyrowave_gpu()) {
        uint8_t *raw = (uint8_t *)malloc(total * 2);
        if (!raw) return DR_NEED_IDR;
        uint8_t *dense = raw + total;
        size_t off = 0;
        for (PLENTRY e = du->bufferList; e; e = e->next) {
            memcpy(raw + off, e->data, (size_t)e->length);
            off += (size_t)e->length;
        }
        size_t n = pyrowave_linux_unframe(raw, total, dense);
        int ok = n ? vk_video_try_submit_pyrowave(dense, n) : 0;
        free(raw);
        // Reported like the CPU path does, so the video widget creates its overlay.
        return DR_OK;
    }
    uint8_t *slot = pyrowave_linux_slot_begin(total);
    if (!slot) return DR_NEED_IDR;
    size_t off = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) {
        memcpy(slot + off, e->data, (size_t)e->length);
        off += (size_t)e->length;
    }
    pyrowave_linux_slot_commit(total);
    return DR_OK;
}

// pyrowave_linux_deliver renders a decoded PyroWave frame (called on the decode
// thread, the only thread submitting NV12 frames in a PyroWave session). PyroWave has
// no software or RGBA fallback here: without the renderer's NV12 path there is
// nowhere to put the frame.
void pyrowave_linux_deliver(const uint8_t *y, const uint8_t *uv, int w, int h, int full_range) {
    static int warned_range = 0, warned_render = 0;
    // USBRIDGE_PYROWAVE_DUMP=<file.pgm>: the luma of the 120th decoded frame (the first
    // ones of a fresh virtual display are often still black), for checking a stream by
    // eye without a window (cmd/pyrowavesmoke).
    if (pyrowave_linux_frames() == 120) {
        const char *dump = getenv("USBRIDGE_PYROWAVE_DUMP");
        FILE *f = dump && *dump ? fopen(dump, "wb") : NULL;
        if (f) {
            fprintf(f, "P5\n%d %d\n255\n", w, h);
            fwrite(y, 1, (size_t)w * (size_t)h, f);
            fclose(f);
        }
    }
    if (full_range && !warned_range) {
        warned_range = 1;
        goVTLog((char*)"pyrowave: stream signals full-range YCbCr -- rendered as limited range");
    }
    if (vk_video_is_active() && !vk_video_nv12_supported() && !warned_render) {
        warned_render = 1;
        goVTLog((char*)"pyrowave: this GPU's Vulkan renderer has no NV12 path -- frames are decoded but cannot be shown");
    }
    if (vk_video_is_active() && vk_video_nv12_supported())
        vk_video_try_submit_nv12(y, w, uv, (w / 2) * 2, w, h);
    // Reported whether or not it was shown: the video widget creates the Vulkan overlay
    // on the first reported frame (there is no RGBA frame to bootstrap it from here).
    goVTFrame(NULL, w, h, 0);
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

    if (g_video_format & VIDEO_FORMAT_MASK_PYROWAVE) return pyrowave_submit(du);

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

    int got = 0;
    if (ret >= 0 || ret == AVERROR(EAGAIN)) {
        AVFrame *frame = av_frame_alloc();
        while (avcodec_receive_frame(ctx, frame) == 0) {
            got = 1;
            deliver_frame(frame);
            av_frame_unref(frame);
        }
        av_frame_free(&frame);
    }

    // A hw decoder that opened fine can still be unable to decode this
    // codec/profile/resolution (e.g. an older NVDEC generation). If one
    // hasn't produced a single frame in its first packets, blacklist it
    // for this process and reopen with the next candidate (ultimately
    // software) on a fresh IDR -- instead of a black screen forever.
    if (!g_hw_verify_ok) {
        if (got) {
            g_hw_verify_ok = 1;
        } else if (++g_hw_verify_pkts >= 20 || (ret < 0 && ret != AVERROR(EAGAIN))) {
            pthread_mutex_lock(&g_av_mu);
            if (g_avctx == ctx && g_hw_active >= 0) {
                char msg[96];
                snprintf(msg, sizeof(msg), "libavcodec: hw decoder produced no frames (ret=%d) -- trying next", ret);
                goVTLog(msg);
                g_hw_skip[g_hw_active] = 1;
                avcodec_free_context(&g_avctx);
                g_hw_active = -1;
            }
            pthread_mutex_unlock(&g_av_mu);
            return DR_NEED_IDR;
        }
    }
    if (ret < 0 && ret != AVERROR(EAGAIN)) return DR_NEED_IDR;
    return DR_OK;
}
*/
import "C"
