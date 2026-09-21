//go:build windows && cgo

// Cache-bust (rev 10): go build's cache doesn't see changes to libmoonlight-common-c.a
// (only referenced via CGO_LDFLAGS -l, not a tracked Go source dependency), so
// a C-only submodule edit silently relinks against a stale .a unless some .go
// file in this package also changes. Bump this comment whenever that happens.
package service

/*
#cgo pkg-config: opus openssl
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build -L${SRCDIR}/../../moonlight-common-c/build/enet -lmoonlight-common-c -lenet -lws2_32 -lwinmm
#cgo LDFLAGS: -lavcodec -lavutil -lswscale
#cgo LDFLAGS: -lole32 -loleaut32 -luuid -lmfplat -lmfuuid

#define COBJMACROS
#define INITGUID
#define VK_USE_PLATFORM_WIN32_KHR
#include <stdarg.h>
#include <windows.h>
#include <mfapi.h>
#include <mmdeviceapi.h>
#include <audioclient.h>
#include <vulkan/vulkan.h>
#include <vulkan/vulkan_win32.h>
#include <libavcodec/avcodec.h>
#include <libavutil/error.h>
#include <libavutil/dict.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_d3d11va.h>
#include <libavutil/hwcontext_vulkan.h>
#include <libavutil/frame.h>
#include <libavutil/imgutils.h>
#include <libswscale/swscale.h>
#include <Limelight.h>
#include <Platform.h>
#include <opus_multistream.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

extern void goMoonlightStage(int stage, int result, int errCode);
extern void goMoonlightConnected(void);
extern void goMoonlightTerminated(int errCode);
extern void goMoonlightRumble(unsigned short controllerNumber, unsigned short lowFreq, unsigned short highFreq);
extern void goVTLog(char *msg);
extern void goVTFrame(uint8_t *rgba, int width, int height, int stride);
extern void goVideoFormatNegotiated(int videoFormat);
extern void goAIVisionOverlay(uint8_t *rgba, int width, int height, int stride);
extern void goNetGraphOverlay(uint8_t *rgba, int width, int height, int stride, int bgr);

// g_last_decode_ms: written from win_deliver_frame/win_deliver_frame_vulkan
// below on every frame (their existing t_start/t_end timing, previously only
// used for the "SLOW win_deliver_frame" diagnostic log). Defined (not
// declared) in net_graph_stats_windows.c, same reasoning as
// g_last_host_latency_tenths_ms further down; read there by
// win_get_last_decode_ms for net_graph_windows.go's GetDecodeMs.
extern volatile double g_last_decode_ms;

// Native overlay fast paths.
// Vulkan (vk_video_impl_windows.c) — preferred, RGBA format.
extern int vk_video_is_active(void);
extern int vk_video_is_device_lost(void);
extern void vk_video_mark_device_lost(void);
extern int vk_video_try_submit(uint8_t *rgba, int width, int height, int stride);
// Zero-copy path: hand a decoded AVVkFrame's VkImage straight to the renderer
// for GPU-side YCbCr sampling (see win_deliver_frame's AV_PIX_FMT_VULKAN
// branch below). release_ctx/release_fn let the renderer free the AVFrame
// ref that keeps the VkImage's memory alive once its own GPU work retires.
extern int vk_video_try_submit_vkframe(void *vk_image, int vk_format, int vk_layout, int width, int height,
                                        int narrow_range, void *release_ctx,
                                        void (*release_fn)(void *));
// GDI fallback (gl_video_impl_windows.c) — BGRA format.
extern int gl_video_is_active(void);
extern int gl_video_try_submit(uint8_t *bgra, int width, int height, int stride);

// ── Shared state ──────────────────────────────────────────────────────────────

static volatile int    g_li_active          = 0;
static volatile int    g_audio_muted        = 0;
static OpusMSDecoder  *g_opus_ms_decoder    = NULL;
static int             g_audio_channels     = 2;

static void set_audio_pipe_fd(int fd) { (void)fd; }
static void set_audio_muted(int muted) { g_audio_muted = muted; }

// ── Connection callbacks ──────────────────────────────────────────────────────

static void cl_stage_starting(int s)      { goMoonlightStage(s,  0, 0); }
static void cl_stage_complete(int s)       { goMoonlightStage(s,  1, 0); }
static void cl_stage_failed(int s, int ec) { goMoonlightStage(s, -1, ec); }
static void cl_connected(void)             { goMoonlightConnected(); }
static void cl_terminated(int ec)          { goMoonlightTerminated(ec); }
static void cl_rumble(unsigned short n, unsigned short low, unsigned short high) { goMoonlightRumble(n, low, high); }
static void cl_log(const char *fmt, ...) {
    char buf[256];
    va_list ap; va_start(ap, fmt); vsnprintf(buf, sizeof(buf), fmt, ap); va_end(ap);
    int n = (int)strlen(buf); if (n > 0 && buf[n-1] == '\n') buf[n-1] = '\0';
    if (buf[0]) goVTLog(buf);
}

// ═══════════════════════════════════════════════════════════════════════════════
// WASAPI audio output
// ═══════════════════════════════════════════════════════════════════════════════

static IAudioClient       *g_wa_client     = NULL;
static IAudioRenderClient *g_wa_render     = NULL;
static UINT32              g_wa_frames     = 0;
static CRITICAL_SECTION    g_wa_cs;
static int                 g_wa_cs_init    = 0;
static int                 g_wa_rate       = 48000;
static int                 g_wa_fail_count = 0;
static ULONGLONG           g_wa_last_write_ms = 0;

static void wasapi_init(int channels, int sample_rate) {
    g_wa_rate = sample_rate;
    if (!g_wa_cs_init) { InitializeCriticalSection(&g_wa_cs); g_wa_cs_init = 1; }
    if (g_wa_client) return;

    CoInitializeEx(NULL, COINIT_MULTITHREADED);

    IMMDeviceEnumerator *pEnum = NULL;
    if (FAILED(CoCreateInstance(&CLSID_MMDeviceEnumerator, NULL,
                                CLSCTX_ALL, &IID_IMMDeviceEnumerator,
                                (void**)&pEnum))) {
        goVTLog((char*)"WASAPI: CoCreateInstance MMDeviceEnumerator FAILED");
        return;
    }
    IMMDevice *pDev = NULL;
    if (FAILED(IMMDeviceEnumerator_GetDefaultAudioEndpoint(pEnum, eRender, eConsole, &pDev))) {
        IMMDeviceEnumerator_Release(pEnum);
        goVTLog((char*)"WASAPI: GetDefaultAudioEndpoint FAILED");
        return;
    }
    IMMDeviceEnumerator_Release(pEnum);

    IAudioClient *pClient = NULL;
    if (FAILED(IMMDevice_Activate(pDev, &IID_IAudioClient, CLSCTX_ALL,
                                   NULL, (void**)&pClient))) {
        IMMDevice_Release(pDev);
        goVTLog((char*)"WASAPI: IMMDevice::Activate FAILED");
        return;
    }
    IMMDevice_Release(pDev);

    WAVEFORMATEX wfx = {
        .wFormatTag      = WAVE_FORMAT_PCM,
        .nChannels       = (WORD)channels,
        .nSamplesPerSec  = (DWORD)sample_rate,
        .wBitsPerSample  = 16,
        .nBlockAlign     = (WORD)(channels * 2),
        .nAvgBytesPerSec = (DWORD)(sample_rate * channels * 2),
        .cbSize          = 0,
    };
    // Shared mode, event-driven — 40 ms buffer.
    HRESULT hr = IAudioClient_Initialize(pClient,
        AUDCLNT_SHAREMODE_SHARED,
        0,
        400000, // 40 ms in 100-ns units
        0, &wfx, NULL);
    if (FAILED(hr)) {
        IAudioClient_Release(pClient);
        goVTLog((char*)"WASAPI: IAudioClient::Initialize FAILED");
        return;
    }

    IAudioRenderClient *pRender = NULL;
    if (FAILED(IAudioClient_GetService(pClient, &IID_IAudioRenderClient, (void**)&pRender))) {
        IAudioClient_Release(pClient);
        goVTLog((char*)"WASAPI: GetService IAudioRenderClient FAILED");
        return;
    }
    IAudioClient_GetBufferSize(pClient, &g_wa_frames);
    IAudioClient_Start(pClient);

    EnterCriticalSection(&g_wa_cs);
    g_wa_client = pClient;
    g_wa_render = pRender;
    LeaveCriticalSection(&g_wa_cs);
    g_wa_fail_count    = 0;
    g_wa_last_write_ms = 0;
    goVTLog((char*)"WASAPI: audio client started (S16LE native output)");
}

static void wasapi_teardown(void) {
    if (!g_wa_cs_init) return;
    EnterCriticalSection(&g_wa_cs);
    IAudioClient       *c = g_wa_client; g_wa_client = NULL;
    IAudioRenderClient *r = g_wa_render; g_wa_render = NULL;
    LeaveCriticalSection(&g_wa_cs);
    if (c) { IAudioClient_Stop(c); IAudioClient_Release(c); }
    if (r) { IAudioRenderClient_Release(r); }
    goVTLog((char*)"WASAPI: audio client stopped");
}

// Repeated WASAPI failures (bad HRESULTs from a client that's fallen into an
// unrecoverable state) never self-heal — recreate the client from scratch so
// audio can resume instead of staying silent for the rest of the session.
static void wasapi_handle_failure(const char *where) {
    if (++g_wa_fail_count < 5) return;
    g_wa_fail_count = 0;
    char msg[128];
    snprintf(msg, sizeof(msg), "WASAPI: %s failing repeatedly, reinitializing audio client", where);
    goVTLog(msg);
    wasapi_teardown();
    wasapi_init(g_audio_channels, g_wa_rate);
}

static void wasapi_write(const opus_int16 *pcm, int samples) {
    EnterCriticalSection(&g_wa_cs);
    IAudioClient       *c = g_wa_client;
    IAudioRenderClient *r = g_wa_render;
    LeaveCriticalSection(&g_wa_cs);
    if (!c || !r) return;

    ULONGLONG now = GetTickCount64();
    // A long gap since the last successful write (network stall drained the
    // buffer) can leave the shared-mode client stuck rendering silence even
    // after new frames arrive; force a fresh clock so playback actually resumes.
    if (g_wa_last_write_ms != 0 && (now - g_wa_last_write_ms) > 250) {
        goVTLog((char*)"WASAPI: resuming after stall, restarting audio client");
        IAudioClient_Stop(c);
        IAudioClient_Reset(c);
        IAudioClient_Start(c);
    }

    UINT32 padding = 0;
    HRESULT hr = IAudioClient_GetCurrentPadding(c, &padding);
    if (FAILED(hr)) { wasapi_handle_failure("GetCurrentPadding"); return; }
    UINT32 avail = g_wa_frames - padding;
    if ((UINT32)samples > avail) return; // buffer full — drop frame, not an error

    BYTE *buf = NULL;
    hr = IAudioRenderClient_GetBuffer(r, (UINT32)samples, &buf);
    if (FAILED(hr) || !buf) { wasapi_handle_failure("GetBuffer"); return; }
    memcpy(buf, pcm, (size_t)samples * (size_t)g_audio_channels * 2);
    hr = IAudioRenderClient_ReleaseBuffer(r, (UINT32)samples, 0);
    if (FAILED(hr)) { wasapi_handle_failure("ReleaseBuffer"); return; }

    g_wa_fail_count    = 0;
    g_wa_last_write_ms = now;
}

// ── Audio callbacks ───────────────────────────────────────────────────────────

static int ar_init(int audioConfig, const POPUS_MULTISTREAM_CONFIGURATION cfg, void *ctx, int flags) {
    (void)audioConfig; (void)ctx; (void)flags;
    g_audio_channels = cfg->channelCount;
    if (g_opus_ms_decoder) { opus_multistream_decoder_destroy(g_opus_ms_decoder); g_opus_ms_decoder = NULL; }
    int error = OPUS_OK;
    g_opus_ms_decoder = opus_multistream_decoder_create(
        cfg->sampleRate, cfg->channelCount,
        cfg->streams, cfg->coupledStreams, cfg->mapping, &error);
    if (error != OPUS_OK) return -1;
    wasapi_init(cfg->channelCount, (int)cfg->sampleRate);
    return 0;
}
static void ar_start(void)   {}
static void ar_stop(void)    {}
static void ar_cleanup(void) {
    wasapi_teardown();
    if (g_opus_ms_decoder) { opus_multistream_decoder_destroy(g_opus_ms_decoder); g_opus_ms_decoder = NULL; }
}
static void ar_decode(char *data, int len) {
    if (!g_opus_ms_decoder) return;
    opus_int16 pcm[5760 * 8];
    int samples = opus_multistream_decode(g_opus_ms_decoder,
        (const unsigned char *)data, len, pcm, 5760, 0);
    if (samples <= 0) return;
    if (g_audio_muted) memset(pcm, 0, samples * g_audio_channels * 2);
    wasapi_write(pcm, samples);
}

// ═══════════════════════════════════════════════════════════════════════════════
// Shared Vulkan hwaccel device — decode (this file) + presentation
// (vk_video_impl_windows.c) on the SAME VkDevice, validated standalone before
// this integration: ffmpeg's own auto-created Vulkan device already comes
// with a GRAPHICS-capable queue family and VK_KHR_external_memory_win32/
// external_semaphore_win32 enabled by default, and (with the extra
// instance/device_extensions opts) VK_KHR_swapchain + the Win32 surface
// extensions too — so decode and presentation can share one VkImage directly
// with zero cross-device export/import, zero D3D11, and no per-frame CPU
// readback. A from-scratch manually-created VkDevice handed TO ffmpeg (the
// reverse direction) reliably crashed deep in libavcodec's Vulkan decode
// internals even after matching every extension/feature ffmpeg's own
// auto-create enables -- letting ffmpeg create the device and adopting it
// (here, and in vk_video_impl_windows.c) is the only path proven to work.
//
// The actual device creation/lookup lives in vk_hwdev_bridge_windows.c, its
// own translation unit — NOT inline in this preamble comment, because cgo
// duplicates any non-static function body written directly in a preamble
// into the generated _cgo_export.c (needed there for //export type info),
// which fails to link with "multiple definition" for anything not `static`.
// ═══════════════════════════════════════════════════════════════════════════════

extern int  goAIVisionShouldSample(void);
extern void goAIVisionSample(uint8_t *rgba, int width, int height, int stride);
extern AVBufferRef *win_vk_hwdev_ctx_ref(void);
extern void vk_frame_release_avframe(void *ctx);

// ═══════════════════════════════════════════════════════════════════════════════
// libavcodec H.264 decoder with D3D11VA hardware acceleration
// ═══════════════════════════════════════════════════════════════════════════════

static AVCodecContext    *g_avctx       = NULL;
static struct SwsContext *g_sws         = NULL;
static AVBufferRef       *g_hw_dev_ctx  = NULL;
static enum AVPixelFormat g_hw_pix_fmt  = AV_PIX_FMT_NONE;
static int                g_using_vulkan_decode = 0; // set once the Vulkan zero-copy tier is committed for this session
static enum AVPixelFormat g_av_dst_fmt  = AV_PIX_FMT_NONE;
static int                g_av_w        = 0;
static int                g_av_h        = 0;
static CRITICAL_SECTION   g_av_cs;
static int                g_av_cs_init  = 0;
static uint64_t           g_av_frame_cnt = 0;

// Codec negotiated by moonlight-common-c for the current session (set in
// dr_setup from its NegotiatedVideoFormat param). VIDEO_FORMAT_* bitmask
// from Limelight.h: 0x0001=H264, 0x0100=H265/HEVC, 0x1000=AV1_MAIN8.
// win_av_init() reads this to pick a matching decoder -- without it, every
// session decoded as H264 regardless of what was actually negotiated, which
// silently breaks HEVC/AV1 sessions (the decoder rejects bitstream it can't
// parse as H264).
static int g_video_format = 0x0001;

static enum AVPixelFormat win_get_hw_format(AVCodecContext *ctx,
                                             const enum AVPixelFormat *fmts) {
    (void)ctx;
    for (const enum AVPixelFormat *p = fmts; *p != AV_PIX_FMT_NONE; p++) {
        if (*p == g_hw_pix_fmt) return *p;
    }
    return AV_PIX_FMT_NONE;
}

// win_av_log_callback: installed once (win_av_init, below) to catch
// "Unable to submit command buffer: VK_ERROR_DEVICE_LOST" from ffmpeg's
// H264/HEVC Vulkan-hwaccel decoder (libavcodec's own vulkan_decode.c --
// vendored as a prebuilt DLL here, not vendored source we can patch
// directly) the instant it's logged, rather than only finding out about it
// indirectly whenever vk_render_thread's own next Vulkan call happens to
// fail too. That decoder shares vk_video_impl_windows.c's VkDevice/VkQueue
// for the zero-copy path: once it's lost, ffmpeg's own decode retries every
// subsequent frame on the same dead device regardless of anything the
// render thread does, and live debugging (gdb, 2026-09-19) showed that
// retry storm alone -- even after the render thread stopped touching the
// device via g_device_lost -- was still enough to trip the NVIDIA driver's
// internal fail-fast (0xc0000409) a few calls later. Marking the device
// lost right here, synchronously inside the same av_log() call that first
// reports it, closes that race: dr_submit's vk_video_is_device_lost() check
// (before the next avcodec_send_packet) sees it in time.
static void win_av_log_callback(void *avcl, int level, const char *fmt, va_list vl) {
    if (level <= AV_LOG_ERROR) {
        va_list vl2;
        va_copy(vl2, vl);
        char buf[512];
        vsnprintf(buf, sizeof(buf), fmt, vl2);
        va_end(vl2);
        if (strstr(buf, "DEVICE_LOST")) {
            vk_video_mark_device_lost();
        }
    }
    av_log_default_callback(avcl, level, fmt, vl);
}

static void win_av_init(void) {
    av_log_set_callback(win_av_log_callback);
    if (!g_av_cs_init) { InitializeCriticalSection(&g_av_cs); g_av_cs_init = 1; }
    if (g_avctx) return;

    // Pick the decoder family to match what was actually negotiated for this
    // session (g_video_format, set by dr_setup) -- previously this always
    // picked H264 unconditionally, so an HEVC/AV1 session fed HEVC/AV1
    // bitstream into an H264 decoder and silently failed to produce frames.
    enum AVCodecID sw_id;
    const char *codec_label;
    if (g_video_format & 0x0F00) { // VIDEO_FORMAT_MASK_H265
        sw_id = AV_CODEC_ID_HEVC; codec_label = "hevc";
    } else if (g_video_format & 0xF000) { // VIDEO_FORMAT_MASK_AV1
        sw_id = AV_CODEC_ID_AV1; codec_label = "av1";
    } else {
        sw_id = AV_CODEC_ID_H264; codec_label = "h264";
    }

    // Unlike VAAPI (Linux) or NVDEC, D3D11VA has no separately-named decoder
    // in ffmpeg's registry -- there is no "h264_d3d11va" entry to look up by
    // name (avcodec_find_decoder_by_name() for it always returns NULL, no
    // matter how ffmpeg was built). D3D11VA, like DXVA2 and VideoToolbox, is a
    // "generic hwaccel": you open the *regular* software decoder (same one
    // used for the fallback path below) with hw_device_ctx + get_format set on
    // its AVCodecContext, and ffmpeg negotiates hardware decode transparently
    // through get_format. The previous by-name lookup silently failed every
    // single time regardless of GPU/driver, forcing 100% of Windows sessions
    // onto the software decoder -- confirmed live via the SLOW receive-loop/
    // direct-submit timing logs: submitDecodeUnit cost 8-250ms/frame
    // (well over the 8.3ms budget at 120fps), which is what was actually
    // driving the RFI/IDR storms this instrumentation was added to chase down,
    // not real network loss.
    const AVCodec *codec = avcodec_find_decoder(sw_id);
    if (!codec) {
        char msg[96];
        snprintf(msg, sizeof(msg), "libavcodec/win: no decoder available for %s", codec_label);
        goVTLog(msg);
        return;
    }

    // Tier 0: real Vulkan Video Decode (VK_KHR_video_decode_h264/h265),
    // zero-copy -- decode and presentation share one VkDevice/VkImage, no
    // CPU readback, no sws_scale. Validated standalone (decode, same-device
    // plane readback, and a real VkSamplerYcbcrConversion render pass all
    // confirmed correct on this GPU/driver) before wiring in here. AV1 has no
    // Vulkan decode extension on this driver, so only try for H264/HEVC --
    // AV1 falls straight through to the D3D11VA tier below as before.
    //
    // USBRIDGE_DISABLE_VK_DECODE (debug/diagnostic only, 2026-09-19): forces
    // straight to the D3D11VA tier below, skipping this one entirely. Added
    // to A/B-test a live VK_ERROR_DEVICE_LOST -> NVIDIA driver fail-fast
    // (0xc0000409 in nvoglv64!DrvPresentBuffers, caught under gdb) against
    // this specific decode tier -- see the crash writeup for why app-level
    // guards (vk_video_is_device_lost/vk_video_mark_device_lost) alone
    // couldn't stop it: the driver's own TDR recovery appears to fail-fast
    // internally, before/regardless of anything this process does afterward.
    if (sw_id != AV_CODEC_ID_AV1 && getenv("USBRIDGE_DISABLE_VK_DECODE")) {
        goVTLog((char*)"libavcodec/win: USBRIDGE_DISABLE_VK_DECODE set -- skipping Vulkan Video Decode tier");
    } else if (sw_id == AV_CODEC_ID_H264 || sw_id == AV_CODEC_ID_HEVC) {
        AVBufferRef *vk_ref = win_vk_hwdev_ctx_ref();
        if (vk_ref) {
            AVCodecContext *test = avcodec_alloc_context3(codec);
            test->hw_device_ctx = av_buffer_ref(vk_ref);
            test->get_format = win_get_hw_format;
            g_hw_pix_fmt = AV_PIX_FMT_VULKAN;
            int openErr = avcodec_open2(test, codec, NULL);
            avcodec_free_context(&test);
            if (openErr == 0) {
                g_avctx = avcodec_alloc_context3(codec);
                g_avctx->hw_device_ctx = vk_ref; // ownership transferred
                g_avctx->get_format = win_get_hw_format;
                if (avcodec_open2(g_avctx, codec, NULL) == 0) {
                    g_using_vulkan_decode = 1;
                    char msg[96];
                    snprintf(msg, sizeof(msg), "libavcodec/win: using %s (hardware Vulkan Video Decode, zero-copy)", codec_label);
                    goVTLog(msg);
                    return;
                }
                avcodec_free_context(&g_avctx); // also unrefs vk_ref via hw_device_ctx
                goVTLog((char*)"libavcodec/win: Vulkan decode avcodec_open2 (real ctx) failed unexpectedly after a successful probe -- trying D3D11VA");
            } else {
                av_buffer_unref(&vk_ref);
                char errbuf[AV_ERROR_MAX_STRING_SIZE] = {0};
                av_strerror(openErr, errbuf, sizeof(errbuf));
                char msg[192];
                snprintf(msg, sizeof(msg), "libavcodec/win: Vulkan decode unavailable for %s: %d (%s) -- trying D3D11VA", codec_label, openErr, errbuf);
                goVTLog(msg);
            }
            g_hw_pix_fmt = AV_PIX_FMT_NONE;
        }
    }

    // Probe D3D11VA on a throwaway context first so a failure here never
    // touches g_avctx -- same reason the old by-name lookup used a `test`
    // context before committing to it.
    AVBufferRef *hw_ctx = NULL;
    int hwErr = av_hwdevice_ctx_create(&hw_ctx, AV_HWDEVICE_TYPE_D3D11VA, NULL, NULL, 0);
    if (hwErr != 0) {
        char errbuf[AV_ERROR_MAX_STRING_SIZE] = {0};
        av_strerror(hwErr, errbuf, sizeof(errbuf));
        char msg[192];
        snprintf(msg, sizeof(msg), "libavcodec/win: av_hwdevice_ctx_create(D3D11VA) failed: %d (%s) -- GPU/driver has no usable D3D11 video decode device, using software", hwErr, errbuf);
        goVTLog(msg);
    } else {
        AVCodecContext *test = avcodec_alloc_context3(codec);
        test->hw_device_ctx = av_buffer_ref(hw_ctx);
        test->get_format = win_get_hw_format;
        g_hw_pix_fmt = AV_PIX_FMT_D3D11;
        int openErr = avcodec_open2(test, codec, NULL);
        avcodec_free_context(&test);
        if (openErr == 0) {
            if (g_hw_dev_ctx) av_buffer_unref(&g_hw_dev_ctx);
            g_hw_dev_ctx = hw_ctx;
            char msg[96];
            snprintf(msg, sizeof(msg), "libavcodec/win: using %s (hardware D3D11VA)", codec_label);
            goVTLog(msg);
        } else {
            char errbuf[AV_ERROR_MAX_STRING_SIZE] = {0};
            av_strerror(openErr, errbuf, sizeof(errbuf));
            char msg[192];
            snprintf(msg, sizeof(msg), "libavcodec/win: avcodec_open2(%s, D3D11VA) failed: %d (%s) -- GPU/driver rejected this codec/profile, using software", codec_label, openErr, errbuf);
            goVTLog(msg);
            av_buffer_unref(&hw_ctx);
            g_hw_pix_fmt = AV_PIX_FMT_NONE;
        }
    }
    if (!g_hw_dev_ctx) {
        char msg[96];
        snprintf(msg, sizeof(msg), "libavcodec/win: using %s software fallback", codec_label);
        goVTLog(msg);
    }

    g_avctx = avcodec_alloc_context3(codec);
    if (g_hw_dev_ctx) {
        g_avctx->hw_device_ctx = av_buffer_ref(g_hw_dev_ctx);
        g_avctx->get_format    = win_get_hw_format;
    }
    if (avcodec_open2(g_avctx, codec, NULL) < 0) {
        avcodec_free_context(&g_avctx);
        goVTLog((char*)"libavcodec/win: avcodec_open2 FAILED");
    }
}

// win_mono_ms: monotonic milliseconds via QueryPerformanceCounter, used only
// for the stage timing below -- deliberately independent of moonlight-common-c's
// own PltGetMicroseconds() so this can't be skewed by anything going on in that
// clock's init/threading.
static double win_mono_ms(void) {
    static LARGE_INTEGER freq;
    static int freq_init = 0;
    if (!freq_init) { QueryPerformanceFrequency(&freq); freq_init = 1; }
    LARGE_INTEGER now;
    QueryPerformanceCounter(&now);
    return (double)now.QuadPart * 1000.0 / (double)freq.QuadPart;
}

// win_deliver_frame runs synchronously on the RTP video-receive thread (see
// dr_submit's call site -- CAPABILITY_DIRECT_SUBMIT means there is no separate
// decode thread on this path). Anything slow in here delays draining the video
// UDP socket, not just presentation: a stall long enough can overflow the
// kernel receive buffer and look identical to real network packet loss in the
// Moonlight/RFI logs (many frames "unrecoverable" in the same instant, then a
// full IDR resync) even though nothing was actually lost on the wire. The
// per-stage timing below exists to tell those two cases apart -- log a
// breakdown whenever one call takes longer than a frame's own budget would
// allow at the negotiated frame rate, so a slow D3D11VA readback or sws_scale
// hitch shows up directly instead of being misdiagnosed as network loss.
#define WIN_DELIVER_SLOW_MS 20.0

// win_deliver_frame_vulkan: zero-copy path for AV_PIX_FMT_VULKAN frames. Hands
// the decoded VkImage straight to the Vulkan renderer for GPU-side YCbCr
// sampling -- no av_hwframe_transfer_data readback, no sws_scale, on the RTP
// receive thread. av_frame_clone bumps the AVFrame's refcount so the decoded
// VkImage's backing memory (owned by ffmpeg's internal Vulkan frame pool)
// stays valid until the renderer's own GPU work reading it has retired;
// vk_frame_release_avframe (passed as the release callback) drops that ref
// at that point. If the renderer rejects the frame (not active / not yet
// initialized), the ref is dropped immediately instead of leaking.
static void win_deliver_frame_vulkan(AVFrame *frame) {
    double t_start = win_mono_ms();
    AVVkFrame *vkf = (AVVkFrame*)frame->data[0];
    AVHWFramesContext *fctx = (AVHWFramesContext*)frame->hw_frames_ctx->data;
    AVVulkanFramesContext *vkfctx = (AVVulkanFramesContext*)fctx->hwctx;

    // AI Vision's detector needs real CPU-readable RGBA pixels every so
    // often (icon_detect ~2Hz, OCR ~0.5Hz -- see ai_vision.go's package doc
    // comment), NOT every frame -- goAIVisionShouldSample() is a cheap
    // (atomics + time comparisons only) pre-check that says whether this
    // particular frame is actually due, mirroring
    // moonlight_cgo_wrapper.go's identically-named macOS Metal fast-path
    // mechanism exactly (see its own doc comment) so the overwhelming
    // majority of frames skip the GPU->CPU readback + sws_scale entirely and
    // this zero-copy decode path stays zero-copy. goAIVisionSample (unlike
    // goAIVisionOverlay) only feeds the detector -- it must NOT draw into
    // pixels, since this buffer is a throwaway conversion scratch space,
    // never the one actually displayed (see below). This also keeps
    // goVTFrame's FPS/first-frame stats tracking working in the common case
    // via its nil-pixels stats-only branch -- EXCEPT that branch relies on
    // the Go side's NativeVideoOverlayIsActive() already being true (it
    // dereferences the pixel pointer otherwise), which is NOT guaranteed on
    // the very first frames: this zero-copy decode path can now activate
    // fast enough that frames start arriving before the GUI thread has
    // finished creating the Vulkan/GDI overlay window. Only take the
    // nil-pixels fast path once a native overlay is confirmed active;
    // otherwise fall back to a real (if wasted) CPU readback so goVTFrame
    // never gets called with a null pointer and a real width/height.
    //
    // Neither AI Vision's detection boxes nor the Net Graph HUD are drawn
    // into a CPU buffer here: this whole zero-copy path exists specifically
    // so hardware Vulkan Video Decode frames go straight to the renderer's
    // VkImage with no GPU->CPU readback of the actual displayed picture at
    // all -- forcing one just to burn in an overlay would defeat that. Both
    // are composited natively instead, straight in vk_video_impl_windows.c's
    // own present path (vk_hud_record_draw / vk_aivision_record_draw,
    // alpha-blended draw calls in the same render pass as the video -- same
    // idea as metal_video_impl_darwin.m's g_hud_layer/g_overlay_layer on
    // macOS), fed by pushNetGraphOverlayToVulkan/pushAIVisionOverlayToVulkan
    // via vk_hud_set_pixels/vk_aivision_set_pixels, independent of this
    // function entirely.
    int native_overlay_active = vk_video_is_active() || gl_video_is_active();
    if (goAIVisionShouldSample() || !native_overlay_active) {
        AVFrame *sw = av_frame_alloc();
        if (sw && av_hwframe_transfer_data(sw, frame, 0) == 0) {
            sw->width = frame->width; sw->height = frame->height;
            int w = sw->width, h = sw->height;
            if (!g_sws || w != g_av_w || h != g_av_h || g_av_dst_fmt != AV_PIX_FMT_RGBA) {
                if (g_sws) sws_freeContext(g_sws);
                g_sws = sws_getContext(w, h, (enum AVPixelFormat)sw->format, w, h, AV_PIX_FMT_RGBA, SWS_BILINEAR, NULL, NULL, NULL);
                g_av_w = w; g_av_h = h; g_av_dst_fmt = AV_PIX_FMT_RGBA;
            }
            if (g_sws) {
                uint8_t *pixels = (uint8_t*)malloc((size_t)w * (size_t)h * 4);
                if (pixels) {
                    uint8_t *dst[4]   = { pixels, NULL, NULL, NULL };
                    int dst_stride[4] = { w * 4, 0, 0, 0 };
                    sws_scale(g_sws, (const uint8_t *const *)sw->data, sw->linesize, 0, h, dst, dst_stride);
                    goAIVisionSample(pixels, w, h, w * 4);
                    goVTFrame(pixels, w, h, w * 4);
                    free(pixels);
                }
            }
        }
        if (sw) av_frame_free(&sw);
    } else {
        // Stats-only notification (first-frame log, FPS counter) -- matches
        // goVTFrame's own NativeVideoOverlayIsActive() nil-pixels branch.
        goVTFrame(NULL, frame->width, frame->height, 0);
    }

    // narrow_range=1: Moonlight/H264/HEVC streams are limited-range BT.601/709.
    AVFrame *ref = av_frame_clone(frame);
    if (ref) {
        if (!vk_video_try_submit_vkframe((void*)vkf->img[0], (int)vkfctx->format[0], (int)vkf->layout[0],
                                          frame->width, frame->height,
                                          1, (void*)ref, vk_frame_release_avframe)) {
            av_frame_free(&ref);
        }
    }

    if (++g_av_frame_cnt == 1) {
        char msg[192];
        snprintf(msg, sizeof(msg), "libavcodec/win: first video frame decoded (Vulkan zero-copy) vk_format=0x%x layout=%d %dx%d",
                 (unsigned)vkfctx->format[0], (int)vkf->layout[0], frame->width, frame->height);
        goVTLog(msg);
    }
    double t_end = win_mono_ms();
    g_last_decode_ms = t_end - t_start;
    if (t_end - t_start > WIN_DELIVER_SLOW_MS) {
        char msg[96];
        snprintf(msg, sizeof(msg), "SLOW win_deliver_frame(vulkan) %.0fms", t_end - t_start);
        goVTLog(msg);
    }
}

static void win_deliver_frame(AVFrame *frame) {
    if (frame->format == AV_PIX_FMT_VULKAN) {
        win_deliver_frame_vulkan(frame);
        return;
    }
    double t_start = win_mono_ms();
    AVFrame *sw = NULL;
    if (frame->format == AV_PIX_FMT_D3D11) {
        sw = av_frame_alloc();
        if (av_hwframe_transfer_data(sw, frame, 0) < 0) { av_frame_free(&sw); return; }
        sw->width = frame->width; sw->height = frame->height;
        frame = sw;
    }
    double t_readback = win_mono_ms();
    int w = frame->width, h = frame->height;
    // Vulkan and Fyne canvas both want RGBA; only GDI fallback needs BGRA.
    enum AVPixelFormat dst_fmt = (!vk_video_is_active() && gl_video_is_active())
                                 ? AV_PIX_FMT_BGRA : AV_PIX_FMT_RGBA;
    if (!g_sws || w != g_av_w || h != g_av_h || dst_fmt != g_av_dst_fmt) {
        if (g_sws) sws_freeContext(g_sws);
        g_sws = sws_getContext(w, h, (enum AVPixelFormat)frame->format,
                               w, h, dst_fmt, SWS_BILINEAR, NULL, NULL, NULL);
        g_av_w = w; g_av_h = h; g_av_dst_fmt = dst_fmt;
    }
    if (g_sws) {
        uint8_t *pixels = (uint8_t *)malloc((size_t)w * (size_t)h * 4);
        if (pixels) {
            double t_alloc = win_mono_ms();
            uint8_t *dst[4]   = { pixels, NULL, NULL, NULL };
            int dst_stride[4] = { w * 4, 0, 0, 0 };
            sws_scale(g_sws, (const uint8_t *const *)frame->data, frame->linesize,
                      0, h, dst, dst_stride);
            double t_scale = win_mono_ms();
            if (++g_av_frame_cnt == 1) goVTLog((char*)"libavcodec/win: first video frame decoded");
            // AI Vision overlay: no-op unless the checkbox in the video
            // settings popup is on (checked internally, single atomic load
            // in the common case) -- burns detection boxes+ids into pixels
            // in place, before it reaches either native fast path. Mirrors
            // moonlight_cgo_linux.go's ordering. Only valid when pixels is
            // actually RGBA (dst_fmt above) -- skip it on the rare GDI/BGRA
            // fallback path, where ApplyAIVisionOverlay's box colors and
            // downstream PNG-encode-as-RGBA would both come out wrong
            // (R/B channels swapped).
            if (dst_fmt == AV_PIX_FMT_RGBA) {
                goAIVisionOverlay(pixels, w, h, w * 4);
            }
            // Net Graph HUD: unlike AI Vision above, this one handles BGRA
            // too (goNetGraphOverlay's bgr param swaps R/B on the way in --
            // see net_graph_windows.go/net_graph.go) instead of skipping the
            // GDI/BGRA fallback path outright -- skipping it here meant the
            // HUD simply never appeared whenever Vulkan wasn't active.
            goNetGraphOverlay(pixels, w, h, w * 4, dst_fmt == AV_PIX_FMT_BGRA ? 1 : 0);
            double t_aivision = win_mono_ms();
            // Submit to native overlay (Vulkan preferred, GDI fallback); no-op if inactive.
            if (!vk_video_try_submit(pixels, w, h, w * 4))
                gl_video_try_submit(pixels, w, h, w * 4);
            double t_submit = win_mono_ms();
            goVTFrame(pixels, w, h, w * 4);
            free(pixels);
            double t_end = win_mono_ms();
            g_last_decode_ms = t_end - t_start;
            if (t_end - t_start > WIN_DELIVER_SLOW_MS) {
                char msg[192];
                snprintf(msg, sizeof(msg),
                    "SLOW win_deliver_frame %.0fms (readback=%.0f alloc=%.0f scale=%.0f aivision=%.0f submit=%.0f vtframe=%.0f)",
                    t_end - t_start, t_readback - t_start, t_alloc - t_readback,
                    t_scale - t_alloc, t_aivision - t_scale, t_submit - t_aivision, t_end - t_submit);
                goVTLog(msg);
            }
        }
    }
    if (sw) av_frame_free(&sw);
}

// ── Video callbacks ───────────────────────────────────────────────────────────

static int  dr_setup(int fmt, int w, int h, int rate, void *ctx, int flags) {
    (void)w; (void)h; (void)rate; (void)ctx; (void)flags;
    g_video_format = fmt ? fmt : 0x0001;
    goVideoFormatNegotiated(fmt);
    return 0;
}
static void dr_start(void)   {}
static void dr_stop(void)    {}
static void dr_cleanup(void) {}

// Latency breakdown, logged periodically so the ~500ms of perceived glass-to-
// glass lag reported live ("джиттер в пол секунды") can be attributed to a
// stage instead of guessed at: PlayoutBuffer's own "SLOW direct-submit" log
// (VideoDepacketizer.c) already accounts for everything from reassembleFrame()
// (~= enqueueTimeUs) through submitDecodeUnit() returning, and that's been
// confirmed fast (submitDecodeUnit ~0.4-0.6ms, Vulkan zero-copy). What's NOT
// instrumented anywhere is receiveTimeUs -> enqueueTimeUs (time this frame's
// packets actually took to arrive+reassemble over the network -- large values
// here mean real network/host delay, not anything this client controls) and
// frameHostProcessingLatency (the host's own self-reported capture+encode
// time). PltGetMicroseconds() (Platform.h) shares its epoch with
// du->receiveTimeUs/enqueueTimeUs (both are moonlight-common-c timestamps),
// unlike win_mono_ms() above which is deliberately on a separate clock.
static unsigned int g_latency_log_ctr;
#define LATENCY_LOG_FRAMES 120 // ~2s at 60fps, matches PlayoutBuffer's own status cadence

// g_last_host_latency_tenths_ms: Windows counterpart to
// moonlight_cgo_shared.h's identically-named static -- captured here
// instead of in a shared dr_submit trampoline because Windows's do_li_start/
// dr_submit setup is a fully separate, self-contained implementation (see
// this file's own comments), not built on moonlight_cgo_shared.h. Read by
// do_get_last_host_latency_tenths_ms (net_graph_windows.go's
// GetLastHostLatencyMs, body in net_graph_stats_windows.c -- see that file's
// header comment for why it isn't inline here). Defined (not just declared)
// in net_graph_stats_windows.c instead of here: a non-static variable
// *definition* in this preamble comment gets duplicated into the generated
// _cgo_export.c the same way a non-static function body would (this file
// has //export directives), causing "multiple definition" at link time.
extern volatile uint16_t g_last_host_latency_tenths_ms;

static int dr_submit(PDECODE_UNIT du) {
    g_last_host_latency_tenths_ms = du->frameHostProcessingLatency;
    if (++g_latency_log_ctr >= LATENCY_LOG_FRAMES) {
        g_latency_log_ctr = 0;
        uint64_t nowUs = PltGetMicroseconds();
        uint64_t recvToEnqueueUs = du->enqueueTimeUs - du->receiveTimeUs;
        uint64_t enqueueToNowUs = nowUs - du->enqueueTimeUs;
        char msg[192];
        snprintf(msg, sizeof(msg),
                 "Latency: hostProc=%.1fms recvToEnqueue=%lluus (network+reassembly) enqueueToSubmit=%lluus frame=%d",
                 du->frameHostProcessingLatency / 10.0,
                 (unsigned long long)recvToEnqueueUs,
                 (unsigned long long)enqueueToNowUs,
                 du->frameNumber);
        goVTLog(msg);
    }

    // The zero-copy H.264 Vulkan-hwaccel decoder shares vk_video_impl_windows.c's
    // VkDevice/VkQueue. Once that device has reported VK_ERROR_DEVICE_LOST
    // (unrecoverable without a full teardown/recreate, not implemented), stop
    // feeding it any more data -- ffmpeg's own internal decode submission
    // keeps retrying (and re-hitting VK_ERROR_DEVICE_LOST) on every packet
    // otherwise, and that retry storm is what was crashing the NVIDIA driver
    // (0xc0000409 fail-fast) even after vk_render_thread itself stopped
    // touching the device (2026-09-19 live debugging). DR_OK (not
    // DR_NEED_IDR): the problem is local/GPU-side, not a network loss the
    // host can fix by resending an IDR frame.
    if (vk_video_is_device_lost()) return DR_OK;

    if (!g_av_cs_init) { InitializeCriticalSection(&g_av_cs); g_av_cs_init = 1; }
    EnterCriticalSection(&g_av_cs);
    if (!g_avctx) win_av_init();
    AVCodecContext *ctx = g_avctx;
    LeaveCriticalSection(&g_av_cs);
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
    double t_decode0 = win_mono_ms();
    int ret = avcodec_send_packet(ctx, pkt);
    av_packet_free(&pkt);
    av_free(data);
    if (ret < 0 && ret != AVERROR(EAGAIN)) return DR_NEED_IDR;

    AVFrame *frame = av_frame_alloc();
    double t_decode1 = win_mono_ms();
    if (t_decode1 - t_decode0 > WIN_DELIVER_SLOW_MS) {
        char msg[96];
        snprintf(msg, sizeof(msg), "SLOW avcodec_send_packet %.0fms", t_decode1 - t_decode0);
        goVTLog(msg);
    }
    while (avcodec_receive_frame(ctx, frame) == 0) {
        win_deliver_frame(frame);
        av_frame_unref(frame);
    }
    av_frame_free(&frame);
    return DR_OK;
}

// do_get_rtp_video_stats / do_get_estimated_rtt_info /
// do_get_last_host_latency_tenths_ms / do_get_playout_jitter_us /
// do_get_playout_applied_delay_us: Windows counterparts to
// moonlight_cgo_shared.h's identically-named functions (that header can't
// be #include-d here -- see this file's own comments on why Windows's
// do_li_start is fully self-contained -- so these are duplicated verbatim
// rather than shared). Called from net_graph_windows.go's Go wrappers.
// Bodies live in net_graph_stats_windows.c, not inline here -- see
// vk_hwdev_bridge_windows.c's header comment: this file has //export
// directives, so a non-static function *body* in this preamble comment
// would get duplicated into the generated _cgo_export.c and fail to link
// with "multiple definition".
void do_get_rtp_video_stats(uint32_t *out);
int do_get_estimated_rtt_info(uint32_t *out);
uint16_t do_get_last_host_latency_tenths_ms(void);
uint64_t do_get_playout_jitter_us(void);
uint64_t do_get_playout_applied_delay_us(void);

// ── LiStartConnection entrypoint ─────────────────────────────────────────────

static int do_li_start(
    const char *address, const char *appVersion, const char *gfeVersion,
    const char *rtspSessionUrl, int serverCodecModeSupport,
    int videoFormat,
    int width, int height, int fps, int bitrate,
    const unsigned char *rikey, int rikeyid, uintptr_t unused
) {
    (void)unused;
    // Do NOT call win_av_init() here: g_video_format at this point is
    // whatever the *previous* session negotiated (there's nothing to reset
    // it in between -- dr_cleanup()/dr_stop() are no-ops), so creating the
    // decoder this early picks the wrong codec family whenever the user
    // switches codec between sessions (e.g. HEVC -> H264). dr_setup() runs
    // during LiStartConnection's stream init, before any decode units are
    // submitted, and updates g_video_format with what was *actually*
    // negotiated for this session; the lazy `if (!g_avctx) win_av_init();`
    // in dr_submit() then creates the decoder against the correct format.

    SERVER_INFORMATION srv; LiInitializeServerInformation(&srv);
    srv.address = address; srv.serverInfoAppVersion = appVersion;
    srv.serverInfoGfeVersion = gfeVersion; srv.rtspSessionUrl = rtspSessionUrl;
    srv.serverCodecModeSupport = serverCodecModeSupport;

    STREAM_CONFIGURATION cfg; LiInitializeStreamConfiguration(&cfg);
    cfg.width = width; cfg.height = height; cfg.fps = fps; cfg.bitrate = bitrate;
    cfg.packetSize = 1200; cfg.streamingRemotely = STREAM_CFG_AUTO;
    cfg.audioConfiguration = AUDIO_CONFIGURATION_STEREO;
    cfg.supportedVideoFormats = videoFormat ? videoFormat : VIDEO_FORMAT_H264;
    // ENCFLG_AUDIO matches the official Moonlight clients' default.
    cfg.clientRefreshRateX100 = fps * 100; cfg.encryptionFlags = ENCFLG_AUDIO;
    if (rikey) {
        memcpy(cfg.remoteInputAesKey, rikey, 16);
        // remoteInputAesIv holds the rikeyid in BIG-endian (network) byte order —
        // that is what we sent as "rikeyid" in /launch and what AudioStream.c
        // reads back via BE32() to build the per-packet audio AES-CBC IV.
        // Writing it little-endian corrupted the first 16 bytes (including the
        // Opus TOC byte) of every decrypted audio packet, producing garbled
        // audio while decode still reported success.
        cfg.remoteInputAesIv[0] = (char)((rikeyid >> 24) & 0xff);
        cfg.remoteInputAesIv[1] = (char)((rikeyid >> 16) & 0xff);
        cfg.remoteInputAesIv[2] = (char)((rikeyid >>  8) & 0xff);
        cfg.remoteInputAesIv[3] = (char)( rikeyid        & 0xff);
    }

    DECODER_RENDERER_CALLBACKS dr; LiInitializeVideoCallbacks(&dr);
    dr.setup = dr_setup; dr.start = dr_start; dr.stop = dr_stop;
    dr.cleanup = dr_cleanup; dr.submitDecodeUnit = dr_submit;
    // See moonlight_cgo_shared.h's identical assignment for why the RFI
    // bits are added here too -- both sides (host DESCRIBE flag + this
    // capability bit) are required before moonlight-common-c actually uses
    // reference-frame-invalidation recovery instead of a full IDR request.
    dr.capabilities = CAPABILITY_DIRECT_SUBMIT | CAPABILITY_REFERENCE_FRAME_INVALIDATION_AVC | CAPABILITY_REFERENCE_FRAME_INVALIDATION_HEVC | CAPABILITY_REFERENCE_FRAME_INVALIDATION_AV1;

    AUDIO_RENDERER_CALLBACKS ar; LiInitializeAudioCallbacks(&ar);
    ar.init = ar_init; ar.start = ar_start; ar.stop = ar_stop;
    ar.cleanup = ar_cleanup; ar.decodeAndPlaySample = ar_decode;
    // See moonlight_cgo_shared.h's identical assignment for why -- requests
    // AudioPacketDuration=10ms (protocol-native branch) so a host's Opus
    // inband FEC (5ms is CELT-only, can never carry it) actually works.
    ar.capabilities = CAPABILITY_SLOW_OPUS_DECODER;

    CONNECTION_LISTENER_CALLBACKS cl; LiInitializeConnectionCallbacks(&cl);
    cl.stageStarting = cl_stage_starting; cl.stageComplete = cl_stage_complete;
    cl.stageFailed = cl_stage_failed; cl.connectionStarted = cl_connected;
    cl.connectionTerminated = cl_terminated; cl.logMessage = cl_log;
    cl.rumble = cl_rumble;

    int ret = LiStartConnection(&srv, &cfg, &cl, &dr, &ar, NULL, 0, NULL, 0);
    if (ret != 0) return ret;
    g_li_active = 1;
    return 0;
}

static void do_li_stop(void) {
    if (!g_li_active) return;
    g_li_active = 0;
    LiStopConnection();
    if (g_sws) { sws_freeContext(g_sws); g_sws = NULL; }
    if (g_avctx) avcodec_free_context(&g_avctx);
    if (g_hw_dev_ctx) av_buffer_unref(&g_hw_dev_ctx);
    // The shared Vulkan hwaccel device (vk_hwdev_bridge_windows.c) deliberately
    // survives past this stream -- it's also adopted by vk_video_impl_windows.c's
    // overlay for presentation, and expensive to recreate. g_using_vulkan_decode
    // resets so the next session's win_av_init() re-probes cleanly (e.g. if
    // the codec changed to AV1, which has no Vulkan decode extension here).
    g_using_vulkan_decode = 0;
}

static void do_li_interrupt(void) {
    LiInterruptConnection();
}

// ── Input forwarders ──────────────────────────────────────────────────────────

static void do_send_key(short vkCode, char action, char modifiers) {
    LiSendKeyboardEvent(vkCode, action, modifiers);
}
static void do_send_mouse_move(short dx, short dy)        { LiSendMouseMoveEvent(dx, dy); }
static void do_send_mouse_position(short x, short y, short refW, short refH) {
    LiSendMousePositionEvent(x, y, refW, refH);
}
static void do_send_mouse_button(char action, int button) { LiSendMouseButtonEvent(action, button); }
static void do_send_scroll(signed char clicks)            { LiSendScrollEvent(clicks); }
static void do_send_multi_controller(
    unsigned short cn, unsigned short am, unsigned short b,
    unsigned char lt, unsigned char rt,
    short lx, short ly, short rx, short ry)
{
    LiSendMultiControllerEvent(cn, am, b, lt, rt, lx, ly, rx, ry);
}
static void do_send_utf8_text(const char *text, unsigned int len) { LiSendUtf8TextEvent(text, len); }
static void do_send_pen(unsigned char eventType, unsigned char toolType, unsigned char penButtons,
                         float x, float y, float pressureOrDistance,
                         unsigned short rotation, unsigned char tilt)
{
    LiSendPenEvent(eventType, toolType, penButtons, x, y, pressureOrDistance, 0.0f, 0.0f, rotation, tilt);
}
*/
import "C"

import (
	"fmt"
	"image"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"

	usbapi "usbridge-client/internal/api"
	"usbridge-client/internal/models"
)

var liStartConnectionActive atomic.Bool

// negotiatedVideoFormat holds the VIDEO_FORMAT_* value moonlight-common-c
// reported via dr_setup(NegotiatedVideoFormat, ...) -- the server's actual
// codec choice for the current session. -1 means "no session has reported a
// negotiated format yet". See moonlight_cgo_wrapper.go's identical pattern
// used on macOS/Linux.
var negotiatedVideoFormat atomic.Int32

func init() {
	negotiatedVideoFormat.Store(-1)
}

func windowsVideoFormatCodecName(format int32) (string, bool) {
	switch {
	case format < 0:
		return "", false
	case format&0x0F00 != 0:
		return models.VideoModeH265, true
	case format&0xF000 != 0:
		return models.VideoModeAV1, true
	case format&0x00FF != 0:
		return models.VideoModeH264, true
	default:
		return "", false
	}
}

var (
	activeStreamDone    chan struct{}
	activeStreamOnce    sync.Once
	activeStreamTermErr error
)

var (
	vtFrameCallback   func(image.Image)
	vtFrameCallbackMu sync.Mutex
)

// liStreamMu serializes LiStopConnection / LiStartConnection so they never
// run concurrently. liStreamGen is a generation counter that lets the goroutine
// detect whether it is still the "current" stream before touching shared state.
var (
	liStreamMu  sync.Mutex
	liStartMu   sync.Mutex // Ensures C.do_li_start is never executed concurrently
	liStreamGen atomic.Uint64
)

func closeActiveStreamDone() {
	activeStreamOnce.Do(func() { close(activeStreamDone) })
}

// stopConnectionSafely tears down the current connection without racing an
// in-flight LiStartConnection() on another goroutine. LiStartConnection()
// and LiStopConnection() are documented (Limelight.h) as NOT safe to call
// concurrently with each other -- only LiInterruptConnection() is safe to
// call at any time. liStreamMu alone does not prevent this race: do_li_start
// runs under the separate liStartMu while holding liStreamMu only briefly
// (or not at all, in the deferred-stop path), so a concurrent do_li_stop()
// under liStreamMu could still overlap a do_li_start() in flight elsewhere.
// Interrupting first unblocks any in-progress LiStartConnection() quickly,
// then waiting on liStartMu guarantees do_li_start has fully returned before
// do_li_stop() touches moonlight-common-c's shared static state.
func stopConnectionSafely() {
	C.do_li_interrupt()
	liStartMu.Lock()
	defer liStartMu.Unlock()
	C.do_li_stop()
}

type MoonlightCgoWrapper struct {
	host       string
	audioMuted bool
}

func NewMoonlightCgoWrapper(host string) *MoonlightCgoWrapper {
	return &MoonlightCgoWrapper{host: host}
}

func (w *MoonlightCgoWrapper) StartStream(
	rtspSessionUrl string,
	rikey []byte,
	appVersion, gfeVersion string,
	serverCodecModeSupport int,
	videoFormat int,
	width, height, fps, bitrate int,
	pipeWrite *os.File,
	audioPipeWrite *os.File,
	onStop func(error),
) error {
	// Hold the stream mutex while stopping any previous connection and resetting
	// state.  This blocks until any in-progress LiStopConnection (from a prior
	// goroutine or from StopStream) has fully returned, preventing concurrent
	// LiStartConnection + LiStopConnection which corrupts moonlight-common-c
	// static state and causes SIGSEGV.
	liStreamMu.Lock()
	stopConnectionSafely()
	myGen := liStreamGen.Add(1)
	activeStreamDone = make(chan struct{})
	activeStreamOnce = sync.Once{}
	activeStreamTermErr = nil
	liStreamMu.Unlock()

	host := C.CString(w.host)
	appVer := C.CString(appVersion)
	gfeVer := C.CString(gfeVersion)
	rtsp := C.CString("rtsp://" + rtspSessionUrl)

	var cRikey *C.uchar
	if len(rikey) == 16 {
		cRikey = (*C.uchar)(C.CBytes(rikey))
	}

	go func() {
		defer C.free(unsafe.Pointer(host))
		defer C.free(unsafe.Pointer(appVer))
		defer C.free(unsafe.Pointer(gfeVer))
		defer C.free(unsafe.Pointer(rtsp))
		if cRikey != nil {
			defer C.free(unsafe.Pointer(cRikey))
		}

		liStartMu.Lock()
		// If another StartStream or StopStream occurred while we waited for the lock, abort this stale attempt.
		if liStreamGen.Load() != myGen {
			liStartMu.Unlock()
			logrus.Info("🌕 [Moonlight/CGO/Win] Aborting stale stream start")
			return
		}

		logrus.Infof("🌕 [Moonlight/CGO/Win] LiStartConnection: host=%s %dx%d@%d bitrate=%d",
			w.host, width, height, fps, bitrate)

		ret := C.do_li_start(
			host, appVer, gfeVer, rtsp,
			C.int(serverCodecModeSupport), C.int(videoFormat),
			C.int(width), C.int(height), C.int(fps), C.int(bitrate),
			cRikey, C.int(1), C.uintptr_t(0),
		)
		liStartMu.Unlock()

		if int(ret) != 0 {
			logrus.Errorf("🌕 [Moonlight/CGO/Win] LiStartConnection FAILED: code=%d", int(ret))
			if pipeWrite != nil {
				_ = pipeWrite.Close()
			}
			if onStop != nil && liStreamGen.Load() == myGen {
				onStop(fmt.Errorf("LiStartConnection error code %d", int(ret)))
			}
			return
		}

		logrus.Info("🌕 [Moonlight/CGO/Win] ✅ streams active")
		// Unconditionally true -- see the Android cgo file's identical fix
		// for why gating this the same way as the generation-checked reset
		// below caused a real bug: under reconnect races this branch could
		// run for a stale generation and skip the store, leaving
		// IsInputActive() stuck false (and every mouse/keyboard send, all
		// gated on it, silently dropped) even though video/audio kept
		// streaming fine. This goroutine's own do_li_start really did just
		// succeed, so the store is always correct and idempotent here.
		liStartConnectionActive.Store(true)

		<-activeStreamDone

		logrus.Info("🌕 [Moonlight/CGO/Win] termination received — stopping")
		// Call LiStopConnection under the mutex so that the next StartStream
		// cannot call LiStartConnection until this stop is fully complete.
		liStreamMu.Lock()
		stopConnectionSafely()
		liStreamMu.Unlock()

		// Only clear shared state if we are still the current generation;
		// a newer StartStream may have already reset these.
		if liStreamGen.Load() == myGen {
			vtFrameCallbackMu.Lock()
			vtFrameCallback = nil
			vtFrameCallbackMu.Unlock()
			liStartConnectionActive.Store(false)
		}
		if pipeWrite != nil {
			_ = pipeWrite.Close()
		}
		if onStop != nil && liStreamGen.Load() == myGen {
			onStop(activeStreamTermErr)
		}
	}()
	return nil
}

func (w *MoonlightCgoWrapper) StopStream() {
	logrus.Info("🌕 [Moonlight/CGO/Win] StopStream: stopping")
	liStreamMu.Lock()
	stopConnectionSafely()
	liStreamMu.Unlock()
	if activeStreamDone != nil {
		closeActiveStreamDone()
	}
}

func (w *MoonlightCgoWrapper) SetAudioMuted(muted bool) {
	w.audioMuted = muted
	if muted {
		C.set_audio_muted(1)
	} else {
		C.set_audio_muted(0)
	}
}
func (w *MoonlightCgoWrapper) GetAudioMuted() bool { return w.audioMuted }

func (w *MoonlightCgoWrapper) SendMoonlightKey(vkCode int16, action int8, modifiers int8) {
	if !liStartConnectionActive.Load() {
		return
	}
	C.do_send_key(C.short(vkCode), C.char(action), C.char(modifiers))
}
func (w *MoonlightCgoWrapper) SendMoonlightMouseMove(dx, dy int16) {
	if !liStartConnectionActive.Load() {
		return
	}
	C.do_send_mouse_move(C.short(dx), C.short(dy))
}
func (w *MoonlightCgoWrapper) SendMoonlightMousePosition(x, y, refW, refH int16) {
	if !liStartConnectionActive.Load() {
		return
	}
	C.do_send_mouse_position(C.short(x), C.short(y), C.short(refW), C.short(refH))
}
func (w *MoonlightCgoWrapper) SendMoonlightMouseButton(action int8, button int) {
	if !liStartConnectionActive.Load() {
		return
	}
	C.do_send_mouse_button(C.char(action), C.int(button))
}
func (w *MoonlightCgoWrapper) SendMoonlightScroll(clicks int8) {
	if !liStartConnectionActive.Load() {
		return
	}
	C.do_send_scroll(C.schar(clicks))
}
func (w *MoonlightCgoWrapper) SendMoonlightControllerEvent(
	controllerNumber uint16, activeGamepadMask uint16, buttons uint16,
	leftTrigger uint8, rightTrigger uint8,
	leftStickX int16, leftStickY int16,
	rightStickX int16, rightStickY int16,
) {
	if !liStartConnectionActive.Load() {
		return
	}
	C.do_send_multi_controller(
		C.ushort(controllerNumber), C.ushort(activeGamepadMask), C.ushort(buttons),
		C.uchar(leftTrigger), C.uchar(rightTrigger),
		C.short(leftStickX), C.short(leftStickY),
		C.short(rightStickX), C.short(rightStickY),
	)
}
func (w *MoonlightCgoWrapper) SendMoonlightPenEvent(
	eventType, toolType, penButtons uint8,
	x, y, pressureOrDistance float32,
	rotation uint16, tilt uint8,
) {
	if !liStartConnectionActive.Load() {
		return
	}
	C.do_send_pen(
		C.uchar(eventType), C.uchar(toolType), C.uchar(penButtons),
		C.float(x), C.float(y), C.float(pressureOrDistance),
		C.ushort(rotation), C.uchar(tilt),
	)
}

func (w *MoonlightCgoWrapper) IsInputActive() bool { return liStartConnectionActive.Load() }

// NegotiatedVideoCodecName returns the codec moonlight-common-c actually
// negotiated with the server for the current session (from dr_setup's
// NegotiatedVideoFormat), matching the macOS/Linux implementation.
func (w *MoonlightCgoWrapper) NegotiatedVideoCodecName() (string, bool) {
	if !liStartConnectionActive.Load() {
		return "", false
	}
	return windowsVideoFormatCodecName(negotiatedVideoFormat.Load())
}

//export goVideoFormatNegotiated
func goVideoFormatNegotiated(format C.int) {
	negotiatedVideoFormat.Store(int32(format))
	name, ok := windowsVideoFormatCodecName(int32(format))
	if !ok {
		logrus.Warnf("🎬 [Moonlight/HW/Win] negotiated video format: unrecognized 0x%04X", int(format))
		return
	}
	logrus.Infof("🎬 [Moonlight/HW/Win] negotiated video format: %s (0x%04X)", name, int(format))
}

func (w *MoonlightCgoWrapper) SendMoonlightUtf8Text(text string) {
	if !liStartConnectionActive.Load() || len(text) == 0 {
		return
	}
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	C.do_send_utf8_text(cs, C.uint(len(text)))
}

// ── CGO-exported Go callbacks ─────────────────────────────────────────────────

var stageNames = []string{
	"none", "platform-init", "name-resolution", "audio-stream-init",
	"rtsp-handshake", "control-stream-init", "video-stream-init",
	"input-stream-init", "control-stream-start", "video-stream-start",
	"audio-stream-start", "input-stream-start",
}

//export goMoonlightStage
func goMoonlightStage(stage, result, errCode C.int) {
	name := "unknown"
	if int(stage) < len(stageNames) {
		name = stageNames[stage]
	}
	switch int(result) {
	case 0:
		logrus.Infof("🌕 [Moonlight] ► %s …", name)
	case 1:
		logrus.Infof("🌕 [Moonlight] ✅ %s", name)
	default:
		logrus.Errorf("🌕 [Moonlight] ❌ %s failed (err=%d)", name, int(errCode))
	}
}

//export goMoonlightConnected
func goMoonlightConnected() {
	logrus.Info("🌕 [Moonlight] stream connected ✅")
	notifyMoonlightStreamReady()
}

// goMoonlightRumble receives the host's gamepad rumble (moonlight-common-c
// ConnListenerRumble) and hands it to the handler set with SetRumbleHandler.
//
//export goMoonlightRumble
func goMoonlightRumble(controller, lowFreq, highFreq C.ushort) {
	dispatchRumble(uint16(controller), uint16(lowFreq), uint16(highFreq))
}

//export goMoonlightTerminated
func goMoonlightTerminated(errCode C.int) {
	reason := "unknown"
	switch int(errCode) {
	case 0:
		reason = "clean disconnect"
	}
	logrus.Errorf("🌕 [Moonlight] ❌ terminated: code=%d (%s)", int(errCode), reason)
	activeStreamTermErr = fmt.Errorf("stream terminated: code=%d (%s)", int(errCode), reason)
	// Clear the negotiated codec so a stale value from this session can't be
	// shown as "currently active" once the stream has actually ended.
	negotiatedVideoFormat.Store(-1)
	closeActiveStreamDone()
}

//export goVTLog
func goVTLog(msg *C.char) { logrus.Infof("🎬 [Moonlight/HW/Win] %s", C.GoString(msg)) }

var vtFrameCount int64

//export goVTFrame
func goVTFrame(rgba *C.uint8_t, width, height, stride C.int) {
	noteNativeFrameSize(int(width), int(height))
	vtFrameCallbackMu.Lock()
	cb := vtFrameCallback
	vtFrameCallbackMu.Unlock()
	if cb == nil {
		return
	}

	cnt := atomic.AddInt64(&vtFrameCount, 1)
	if cnt == 1 {
		logrus.Infof("🎬 [Moonlight/HW/Win] ✅ first video frame — %dx%d", int(width), int(height))
	}

	// When the native overlay was active at the C call site, the frame was
	// already submitted at C level and this call carries rgba=NULL purely
	// for stats tracking (see win_deliver_frame_vulkan's native_overlay_active
	// branch in moonlight_cgo_windows.go's C preamble). Trust that pointer
	// directly instead of re-checking NativeVideoOverlayIsActive() here: that
	// re-check reads the same live atomic the C side already sampled, and if
	// it flips between the two reads (e.g. the Vulkan render thread tearing
	// down mid-frame), this would take the "real pixels" branch below with a
	// NULL rgba and segfault -- which is exactly what happened (SIGSEGV in
	// goVTFrame, rgba=0x0, stride=0, caught live under gdb on the VideoRecv
	// thread).
	if rgba == nil {
		cb(nil)
		return
	}

	w, h, s := int(width), int(height), int(stride)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rowBytes := w * 4
	if s == rowBytes {
		copy(img.Pix, (*[1 << 30]byte)(unsafe.Pointer(rgba))[:w*h*4:w*h*4])
	} else {
		src := (*[1 << 30]byte)(unsafe.Pointer(rgba))[: h*s : h*s]
		for y := 0; y < h; y++ {
			copy(img.Pix[y*rowBytes:], src[y*s:y*s+rowBytes])
		}
	}
	cb(img)
}

// goAIVisionOverlay is the Windows counterpart to moonlight_cgo_wrapper.go's
// export of the same name (that file is built only for darwin/ios/linux --
// see its own doc comment for why Windows needs a separate definition), used
// by win_deliver_frame's non-zero-copy branches: those already run every
// decoded frame through a CPU-readable RGBA buffer, so drawing straight into
// it in place is fine there. Identical body: no-op unless the checkbox is
// on, draws detection boxes into rgba in place.
//
// NOT called from win_deliver_frame_vulkan (hardware Vulkan Video Decode's
// zero-copy path) -- that path uses goAIVisionShouldSample/goAIVisionSample
// below instead, same split moonlight_cgo_wrapper.go's macOS Metal fast path
// uses (see that file's doc comments), so the rare CPU readback it still
// needs for detection never draws into (and never displays) that throwaway
// buffer -- the boxes reach the screen via pushAIVisionOverlayToVulkan's
// native compositor-layer draw call instead (vk_aivision_record_draw).
//
//export goAIVisionOverlay
func goAIVisionOverlay(rgba *C.uint8_t, width, height, stride C.int) {
	if rgba == nil || width <= 0 || height <= 0 || stride <= 0 {
		return
	}
	if !aiVisionEnabled.Load() {
		return
	}
	w, h, s := int(width), int(height), int(stride)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(rgba)), s*h)
	ApplyAIVisionOverlay(buf, w, h, s)
}

// goAIVisionShouldSample is the Windows counterpart to
// moonlight_cgo_wrapper.go's identically-named macOS export -- see its doc
// comment for the full reasoning. Cheap (atomics + time comparisons only,
// no pixel access) pre-check called every frame from
// win_deliver_frame_vulkan: lets that zero-copy path skip the GPU->CPU
// readback + sws_scale entirely on the overwhelming majority of frames,
// where neither the icon nor the OCR loop is actually due yet.
//
//export goAIVisionShouldSample
func goAIVisionShouldSample() C.int {
	if usbapi.LiveFrameWanted() {
		return 1
	}
	if !aiVisionEnabled.Load() {
		return 0
	}
	now := time.Now().UnixNano()
	iconDue := !aiVisionIconBusy.Load() && now-aiVisionIconLastRun.Load() >= int64(aiVisionIconInterval)
	ocrDue := !aiVisionOCRBusy.Load() && now-aiVisionOCRLastRun.Load() >= int64(aiVisionOCRInterval)
	if iconDue || ocrDue {
		return 1
	}
	return 0
}

// goAIVisionSample is win_deliver_frame_vulkan's counterpart to
// goAIVisionOverlay: called only on the rare frame goAIVisionShouldSample
// green-lit, with a CPU readback of that one frame. It only feeds the
// detector (maybeKickIconDetection/maybeKickOCR/maybeServeLiveFrame) -- it
// must NOT draw into buf, unlike goAIVisionOverlay's ApplyAIVisionOverlay,
// because this buffer is a throwaway conversion scratch space, never the one
// actually displayed (the zero-copy VkImage is, straight in the renderer).
//
//export goAIVisionSample
func goAIVisionSample(rgba *C.uint8_t, width, height, stride C.int) {
	if rgba == nil || width <= 0 || height <= 0 || stride <= 0 {
		return
	}
	w, h, s := int(width), int(height), int(stride)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(rgba)), s*h)
	maybeServeLiveFrame(buf, w, h, s)
	if !aiVisionEnabled.Load() {
		return
	}
	maybeKickIconDetection(buf, w, h, s)
	maybeKickOCR(buf, w, h, s)
}
