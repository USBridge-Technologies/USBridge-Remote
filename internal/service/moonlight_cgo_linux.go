//go:build linux && !android && cgo

package service

/*
#cgo pkg-config: opus openssl libavcodec libavutil libswscale alsa
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build -L${SRCDIR}/../../moonlight-common-c/build/enet -lmoonlight-common-c -lenet
#cgo LDFLAGS: -lpthread -lm -ldl

#include <libavcodec/avcodec.h>
#include <libavutil/hwcontext.h>
#include <libavutil/frame.h>
#include <libavutil/imgutils.h>
#include <libavutil/pixdesc.h>
#include <libswscale/swscale.h>
#include <alsa/asoundlib.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

// Go callbacks (defined in moonlight_cgo_wrapper.go).
extern void goMoonlightStage(int stage, int result, int errCode);
extern void goMoonlightConnected(void);
extern void goMoonlightTerminated(int errCode);
extern void goVTLog(char *msg);
extern void goVTFrame(uint8_t *rgba, int width, int height, int stride);

#include "moonlight_cgo_shared.h"

// ═══════════════════════════════════════════════════════════════════════════════
// ALSA audio output — platform audio implementation
// ═══════════════════════════════════════════════════════════════════════════════

static snd_pcm_t      *g_alsa_pcm  = NULL;
static pthread_mutex_t g_alsa_mu   = PTHREAD_MUTEX_INITIALIZER;

void platform_ar_init(int channels, int sample_rate) {
    if (g_alsa_pcm) return;
    snd_pcm_t *pcm = NULL;
    int r = snd_pcm_open(&pcm, "default", SND_PCM_STREAM_PLAYBACK, 0);
    if (r < 0) { goVTLog((char*)"ALSA: snd_pcm_open failed"); return; }

    r = snd_pcm_set_params(pcm,
        SND_PCM_FORMAT_S16_LE, SND_PCM_ACCESS_RW_INTERLEAVED,
        (unsigned int)channels, (unsigned int)sample_rate,
        1,      // allow sw resampling
        40000); // 40 ms latency
    if (r < 0) {
        snd_pcm_close(pcm);
        goVTLog((char*)"ALSA: snd_pcm_set_params failed");
        return;
    }
    pthread_mutex_lock(&g_alsa_mu);
    g_alsa_pcm = pcm;
    pthread_mutex_unlock(&g_alsa_mu);
    goVTLog((char*)"ALSA: audio device opened (S16LE native output)");
}

void platform_ar_cleanup(void) {
    pthread_mutex_lock(&g_alsa_mu);
    snd_pcm_t *pcm = g_alsa_pcm;
    g_alsa_pcm = NULL;
    pthread_mutex_unlock(&g_alsa_mu);
    if (!pcm) return;
    snd_pcm_drain(pcm);
    snd_pcm_close(pcm);
    goVTLog((char*)"ALSA: audio device closed");
}

void platform_ar_decode(const opus_int16 *pcm_data, int byte_count, int samples) {
    (void)byte_count;
    pthread_mutex_lock(&g_alsa_mu);
    snd_pcm_t *pcm = g_alsa_pcm;
    pthread_mutex_unlock(&g_alsa_mu);
    if (!pcm) return;
    snd_pcm_sframes_t r = snd_pcm_writei(pcm, pcm_data, (snd_pcm_uframes_t)samples);
    if (r < 0) snd_pcm_recover(pcm, (int)r, 1);
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

    static const struct { const char *name; enum AVHWDeviceType type; enum AVPixelFormat fmt; } hw[] = {
        { "h264_vaapi", AV_HWDEVICE_TYPE_VAAPI, AV_PIX_FMT_VAAPI },
        { "h264_nvdec", AV_HWDEVICE_TYPE_CUDA,  AV_PIX_FMT_CUDA  },
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
        codec = avcodec_find_decoder(AV_CODEC_ID_H264);
        g_hw_pix_fmt = AV_PIX_FMT_NONE;
        goVTLog((char*)"libavcodec: h264 software fallback (no VA-API/NVDEC found)");
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
            goVTFrame(rgba, w, h, w * 4);
            free(rgba);
        }
    }
    if (sw) av_frame_free(&sw);
}

// platform_post_stop: no session state to tear down on Linux.
void platform_post_stop(void) {}

int platform_dr_submit(PDECODE_UNIT du) {
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
