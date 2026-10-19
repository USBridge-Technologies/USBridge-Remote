//go:build windows && cgo

// Cache-bust (rev 13): go build's cache doesn't see changes to libmoonlight-common-c.a
// (only referenced via CGO_LDFLAGS -l, not a tracked Go source dependency), so
// a C-only submodule edit silently relinks against a stale .a unless some .go
// file in this package also changes. Bump this comment whenever that happens.
package service

/*
#cgo pkg-config: opus openssl
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build -L${SRCDIR}/../../moonlight-common-c/build/enet -lmoonlight-common-c -lenet -lws2_32 -lwinmm
#cgo LDFLAGS: -lavcodec -lavutil -lswscale
#cgo LDFLAGS: -lole32 -loleaut32 -luuid -lmfplat -lmfuuid -ld3d11 -ldxgi

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
// wait_sem/wait_value: the AVVkFrame's timeline semaphore and the value its
// decode signals -- the render thread waits on exactly that instead of
// idling ffmpeg's whole decode queue.
// external: the image is imported from D3D11 (d3d11_interop_windows.c) and
// must be acquired from / released to VK_QUEUE_FAMILY_EXTERNAL.
extern int vk_video_try_submit_vkframe(void *vk_image, int vk_format, int vk_layout, int width, int height,
                                        int narrow_range, void *wait_sem, uint64_t wait_value, int external,
                                        void *release_ctx, void (*release_fn)(void *));
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
// Playback starts once WASAPI_PREBUFFER_MS of audio is queued and restarts the
// same way after an underrun: packets reach us in bursts (Wi-Fi/tailnet jitter,
// GC pauses), and a 10 ms-at-a-time feed straight into a 40 ms buffer crackled.
static int                 g_wa_started = 0;
#define WASAPI_BUFFER_MS    120
#define WASAPI_PREBUFFER_MS 40

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
    // Shared mode: a 120 ms ring that wasapi_write keeps ~40 ms full.
    HRESULT hr = IAudioClient_Initialize(pClient,
        AUDCLNT_SHAREMODE_SHARED,
        0,
        (REFERENCE_TIME)WASAPI_BUFFER_MS * 10000, // 100-ns units
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

    EnterCriticalSection(&g_wa_cs);
    g_wa_client = pClient;
    g_wa_render = pRender;
    LeaveCriticalSection(&g_wa_cs);
    g_wa_fail_count    = 0;
    g_wa_last_write_ms = 0;
    g_wa_started       = 0;
    goVTLog((char*)"WASAPI: audio client ready (S16LE native output, prebuffering)");
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
    UINT32 padding = 0;
    HRESULT hr = IAudioClient_GetCurrentPadding(c, &padding);
    if (FAILED(hr)) { wasapi_handle_failure("GetCurrentPadding"); return; }
    // Underrun (the buffer ran dry, or a long gap: a drained shared-mode client
    // can keep rendering silence even after new frames arrive): reset the clock
    // and prebuffer again instead of playing the next frames into a dry buffer.
    if (g_wa_started && (padding == 0 || (g_wa_last_write_ms != 0 && (now - g_wa_last_write_ms) > 250))) {
        IAudioClient_Stop(c);
        IAudioClient_Reset(c);
        g_wa_started = 0;
        padding = 0;
    }
    // A burst leaves the queue long until the next underrun; shed a frame while
    // it holds more than twice the prebuffer so the lag settles back to ~40 ms.
    if (g_wa_started && padding > (UINT32)g_wa_rate * (2 * WASAPI_PREBUFFER_MS) / 1000) return;
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
    if (!g_wa_started && padding + (UINT32)samples >= (UINT32)g_wa_rate * WASAPI_PREBUFFER_MS / 1000) {
        IAudioClient_Start(c);
        g_wa_started = 1;
    }
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
// D3D11VA decode -> Vulkan renderer bridge (d3d11_interop_windows.c).
extern AVBufferRef *d3dx_device_ref(void);
extern int d3dx_deliver(AVFrame *frame, void **out_img, int *out_vkfmt, void **out_sem, uint64_t *out_val, void **out_release_ctx);
extern void d3dx_release_slot(void *ctx);
extern AVFrame *d3dx_download(AVFrame *frame);
// PyroWave GPU decode (pyrowave_decode_windows.c).
extern int pyrowave_win_decode(const uint8_t *au, size_t len, void **out_img, int *out_vkfmt, void **out_sem,
                               uint64_t *out_val, int *out_w, int *out_h, int *out_narrow, void **out_slot);
extern void pyrowave_win_release_slot(void *ctx);
extern void pyrowave_win_stream_reset(void);
extern void vk_frame_release_avframe(void *ctx);

// ═══════════════════════════════════════════════════════════════════════════════
// libavcodec H.264 decoder with D3D11VA hardware acceleration
// ═══════════════════════════════════════════════════════════════════════════════

static AVCodecContext    *g_avctx       = NULL;
static struct SwsContext *g_sws         = NULL;
static AVBufferRef       *g_hw_dev_ctx  = NULL;
static enum AVPixelFormat g_hw_pix_fmt  = AV_PIX_FMT_NONE;
static int                g_using_vulkan_decode = 0; // set once the Vulkan zero-copy tier is committed for this session
static int                g_using_d3dx_decode   = 0; // D3D11VA decode copied into Vulkan (d3d11_interop_windows.c)
static int                g_using_pyrowave      = 0; // VIDEO_FORMAT_PYROWAVE: pyrowave_decode_windows.c, no libavcodec
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
static int g_stream_w = 0, g_stream_h = 0, g_stream_fps = 0; // also from dr_setup

// win_vk_decode_threads: libavcodec threads for the Vulkan Video decoder.
// With one thread ffmpeg decodes strictly one frame at a time -- measured on
// a Radeon 780M, 4K HEVC tops out at ~104fps (9.6ms/frame), so a 4K@120
// stream can never keep up. Two frame threads keep two decodes in flight
// (~200fps) at the cost of exactly one frame of added latency (frame
// threading needs AV_CODEC_FLAG_LOW_DELAY off), which is why it's only used
// above a 4K@60 pixel rate. USBRIDGE_DECODE_THREADS overrides.
static int win_vk_decode_threads(void) {
    const char *v = getenv("USBRIDGE_DECODE_THREADS");
    if (v && atoi(v) > 0) return atoi(v);
    double px_rate = (double)g_stream_w * (double)g_stream_h * (double)g_stream_fps;
    return px_rate > 3840.0 * 2160.0 * 60.0 ? 2 : 1;
}

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

    // Tier 0: D3D11VA decode -- what moonlight-qt uses on Windows -- copied
    // on the GPU into images the Vulkan renderer imports
    // (d3d11_interop_windows.c). On a Radeon 780M it decodes 4K HEVC at
    // ~232 fps on one thread vs ~151 fps for ffmpeg's Vulkan decoder, with
    // lower tail latency at 120 fps (docs/WINDOWS_DECODE_PIPELINE.md), and
    // the renderer never touches an image libavcodec still uses as a
    // reference. USBRIDGE_DECODER=vulkan skips it.
    {
        const char *dec_env = getenv("USBRIDGE_DECODER");
        AVBufferRef *dx_ref = (dec_env && _stricmp(dec_env, "vulkan") == 0) ? NULL : d3dx_device_ref();
        if (dx_ref) {
            g_hw_pix_fmt = AV_PIX_FMT_D3D11;
            g_avctx = avcodec_alloc_context3(codec);
            g_avctx->flags |= AV_CODEC_FLAG_LOW_DELAY;
            g_avctx->hw_device_ctx = dx_ref; // ownership transferred
            g_avctx->get_format = win_get_hw_format;
            if (avcodec_open2(g_avctx, codec, NULL) == 0) {
                g_using_d3dx_decode = 1;
                char msg[160];
                snprintf(msg, sizeof(msg), "libavcodec/win: using %s (hardware D3D11VA -> Vulkan zero-copy interop, %dx%d@%d)",
                         codec_label, g_stream_w, g_stream_h, g_stream_fps);
                goVTLog(msg);
                return;
            }
            avcodec_free_context(&g_avctx);
            g_hw_pix_fmt = AV_PIX_FMT_NONE;
            goVTLog((char*)"libavcodec/win: D3D11VA interop decoder rejected this codec/profile -- trying Vulkan Video Decode");
        }
    }

    // Tier 1: real Vulkan Video Decode (VK_KHR_video_decode_h264/h265),
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
                int threads = win_vk_decode_threads();
                g_avctx = avcodec_alloc_context3(codec);
                if (threads > 1) {
                    g_avctx->thread_count = threads;
                    g_avctx->thread_type = FF_THREAD_FRAME;
                } else {
                    g_avctx->flags |= AV_CODEC_FLAG_LOW_DELAY; // same as moonlight-qt: no reorder delay
                }
                g_avctx->hw_device_ctx = vk_ref; // ownership transferred
                g_avctx->get_format = win_get_hw_format;
                if (avcodec_open2(g_avctx, codec, NULL) == 0) {
                    g_using_vulkan_decode = 1;
                    char msg[160];
                    snprintf(msg, sizeof(msg), "libavcodec/win: using %s (hardware Vulkan Video Decode, zero-copy, %d decode thread%s, %dx%d@%d)",
                             codec_label, threads, threads > 1 ? "s" : "", g_stream_w, g_stream_h, g_stream_fps);
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
    g_avctx->flags |= AV_CODEC_FLAG_LOW_DELAY;
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

// win_deliver_frame runs on the decode thread (pull/thread modes), or -- with
// USBRIDGE_DECODE_MODE=direct only -- synchronously on the RTP video-receive
// thread. In direct mode anything slow in here delays draining the video UDP
// socket, not just presentation: a stall long enough can overflow the
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
// win_readback_thread: GPU->CPU copy of a decoded Vulkan frame for the two
// consumers that need real pixels -- the GUI's first frame / pre-overlay
// display (goVTFrame) and AI Vision's detector (goAIVisionSample). Single
// slot: a frame posted while one is still being converted is dropped. The
// cloned AVFrame keeps its hw frames context (and so the VkImage) alive even
// if the stream stops meanwhile; this thread has its own sws context.
static CRITICAL_SECTION g_rb_cs;
static HANDLE   g_rb_event  = NULL;
static HANDLE   g_rb_thread = NULL;
static AVFrame *g_rb_frame  = NULL;
static int      g_rb_vtframe = 0;
static LONG     g_rb_gen = 0;
static int      g_vk_session_frames = 0; // frames this stream; reset in dr_setup
static int      g_vk_overlay_seen = 0;   // overlay was active at some point this stream
// g_stream_gen changes at every stream start and stop. A readback job is
// only delivered if the generation it was taken in is still current: a
// conversion finishing after its stream stopped (or after the next one
// started) used to reach the GUI as that stream's "first frame" and create a
// second Vulkan overlay on top of the live one -- broken swapchain, fence
// timeouts, a frozen picture left over the UI.
static volatile LONG g_stream_gen = 0;

static DWORD WINAPI win_readback_thread(LPVOID arg) {
    (void)arg;
    struct SwsContext *sws = NULL;
    int sws_w = 0, sws_h = 0, sws_fmt = -1;
    for (;;) {
        WaitForSingleObject(g_rb_event, INFINITE);
        EnterCriticalSection(&g_rb_cs);
        AVFrame *frame = g_rb_frame;
        int vtframe = g_rb_vtframe;
        LONG gen = g_rb_gen;
        g_rb_frame = NULL;
        LeaveCriticalSection(&g_rb_cs);
        if (!frame) continue;
        if (gen != g_stream_gen) { av_frame_free(&frame); continue; }

        // D3D11VA frames go through d3dx_download, which never waits for
        // the GPU while holding the device lock D3D11VA decode needs.
        AVFrame *sw = NULL;
        int got = 0;
        if (frame->format == AV_PIX_FMT_D3D11) {
            sw = d3dx_download(frame);
            got = sw != NULL;
        } else {
            sw = av_frame_alloc();
            got = sw && av_hwframe_transfer_data(sw, frame, 0) == 0;
        }
        if (got) {
            int w = frame->width, h = frame->height;
            if (!sws || w != sws_w || h != sws_h || sw->format != sws_fmt) {
                if (sws) sws_freeContext(sws);
                sws = sws_getContext(w, h, (enum AVPixelFormat)sw->format, w, h, AV_PIX_FMT_RGBA, SWS_BILINEAR, NULL, NULL, NULL);
                sws_w = w; sws_h = h; sws_fmt = sw->format;
            }
            uint8_t *pixels = sws ? (uint8_t*)malloc((size_t)w * (size_t)h * 4) : NULL;
            if (pixels) {
                uint8_t *dst[4]   = { pixels, NULL, NULL, NULL };
                int dst_stride[4] = { w * 4, 0, 0, 0 };
                sws_scale(sws, (const uint8_t *const *)sw->data, sw->linesize, 0, h, dst, dst_stride);
                if (gen == g_stream_gen) {
                    goAIVisionSample(pixels, w, h, w * 4);
                    if (vtframe && !(vk_video_is_active() || gl_video_is_active())) goVTFrame(pixels, w, h, w * 4);
                }
                free(pixels);
            }
        }
        if (sw) av_frame_free(&sw);
        av_frame_free(&frame);
    }
    return 0;
}

// win_readback_cancel drops a queued (not yet started) readback job.
static void win_readback_cancel(void) {
    if (!g_rb_thread) return;
    EnterCriticalSection(&g_rb_cs);
    if (g_rb_frame) av_frame_free(&g_rb_frame);
    LeaveCriticalSection(&g_rb_cs);
}

static void win_readback_post(AVFrame *frame, int vtframe) {
    if (!g_rb_thread) {
        InitializeCriticalSection(&g_rb_cs);
        g_rb_event = CreateEvent(NULL, FALSE, FALSE, NULL);
        g_rb_thread = CreateThread(NULL, 0, win_readback_thread, NULL, 0, NULL);
        // Background work (AI Vision sampling): never ahead of decode.
        if (g_rb_thread) SetThreadPriority(g_rb_thread, THREAD_PRIORITY_BELOW_NORMAL);
    }
    EnterCriticalSection(&g_rb_cs);
    if (!g_rb_frame) {
        g_rb_frame = av_frame_clone(frame);
        g_rb_vtframe = vtframe;
        g_rb_gen = g_stream_gen;
    }
    LeaveCriticalSection(&g_rb_cs);
    SetEvent(g_rb_event);
}

// win_frame_notify: per-frame GUI/stats notification shared by the
// zero-copy paths (Vulkan Video decode and D3D11VA interop).
static void win_frame_notify(AVFrame *frame) {
    // The readback itself runs on win_readback_thread, never here: a 4K
    // transfer + sws_scale costs ~270ms, and on the decode thread that
    // overflowed moonlight-common-c's 15-frame queue on the first frame of
    // every session (flush -> ~1.5s of "Waiting for IDR frame").
    //
    // No CPU copy is needed to bring the overlay up: a nil-pixels goVTFrame
    // on frame 1 already makes the video widget create the Vulkan overlay
    // (handleVideoFrame's frame == nil branch), with the size taken from
    // noteNativeFrameSize. Pixels are only read back if the overlay still
    // isn't up ~2s in (fallback to the Fyne canvas), or for AI Vision.
    // Only while the overlay has never come up this stream: it is also
    // briefly inactive while a stream is being torn down or restarted, and a
    // readback taken then is exactly the stale frame g_stream_gen guards
    // against.
    int native_overlay_active = vk_video_is_active() || gl_video_is_active();
    if (native_overlay_active) g_vk_overlay_seen = 1;
    int session_frame = ++g_vk_session_frames;
    if (!native_overlay_active && !g_vk_overlay_seen && session_frame > 2 * (g_stream_fps > 0 ? g_stream_fps : 60)) {
        win_readback_post(frame, 1);
    } else {
        if (goAIVisionShouldSample()) win_readback_post(frame, 0);
        // Stats-only notification (first-frame log, FPS counter, overlay
        // bootstrap on frame 1).
        goVTFrame(NULL, frame->width, frame->height, 0);
    }

}

// win_deliver_frame_d3dx: D3D11VA frame -> GPU copy into an imported image
// (d3dx_deliver) -> renderer. The ring slot comes back to d3dx via
// d3dx_release_slot once the renderer's read of it has retired.
static void win_deliver_frame_d3dx(AVFrame *frame) {
    double t_start = win_mono_ms();
    win_frame_notify(frame);
    void *img = NULL, *sem = NULL, *slot = NULL;
    int vkfmt = 0;
    uint64_t val = 0;
    if (d3dx_deliver(frame, &img, &vkfmt, &sem, &val, &slot)) {
        if (!vk_video_try_submit_vkframe(img, vkfmt, 0, frame->width, frame->height,
                                          1, sem, val, 1, slot, d3dx_release_slot)) {
            d3dx_release_slot(slot);
        }
    }
    if (++g_av_frame_cnt == 1) {
        char msg[128];
        snprintf(msg, sizeof(msg), "libavcodec/win: first video frame decoded (D3D11VA -> Vulkan) %dx%d", frame->width, frame->height);
        goVTLog(msg);
    }
    double t_end = win_mono_ms();
    g_last_decode_ms = t_end - t_start;
    if (t_end - t_start > WIN_DELIVER_SLOW_MS) {
        char msg[96];
        snprintf(msg, sizeof(msg), "SLOW win_deliver_frame(d3d11) %.0fms", t_end - t_start);
        goVTLog(msg);
    }
}

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
    win_frame_notify(frame);

    // narrow_range=1: Moonlight/H264/HEVC streams are limited-range BT.601/709.
    AVFrame *ref = av_frame_clone(frame);
    if (ref) {
        if (!vk_video_try_submit_vkframe((void*)vkf->img[0], (int)vkfctx->format[0], (int)vkf->layout[0],
                                          frame->width, frame->height,
                                          1, (void*)vkf->sem[0], vkf->sem_value[0], 0,
                                          (void*)ref, vk_frame_release_avframe)) {
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
    if (frame->format == AV_PIX_FMT_D3D11 && g_using_d3dx_decode) {
        win_deliver_frame_d3dx(frame);
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
    (void)ctx; (void)flags;
    g_video_format = fmt ? fmt : 0x0001;
    g_stream_w = w; g_stream_h = h; g_stream_fps = rate;
    g_vk_session_frames = 0;
    g_vk_overlay_seen = 0;
    InterlockedIncrement(&g_stream_gen);
    g_using_pyrowave = (g_video_format & 0xF0000) != 0; // VIDEO_FORMAT_MASK_PYROWAVE
    if (g_using_pyrowave) {
        pyrowave_win_stream_reset();
        goVTLog((char*)"pyrowave: stream negotiated -- decoding on the GPU (pyrowave_decode_windows.c)");
    } else {
        // Create the decoder now (moonlight-qt does the same in its setup)
        // rather than on the first frame: Vulkan Video session setup takes
        // about a second, and frames queued behind it overflowed the 15-frame
        // queue.
        if (!g_av_cs_init) { InitializeCriticalSection(&g_av_cs); g_av_cs_init = 1; }
        EnterCriticalSection(&g_av_cs);
        if (!g_avctx) win_av_init();
        LeaveCriticalSection(&g_av_cs);
    }
    goVideoFormatNegotiated(fmt);
    return 0;
}
// -- Decode threading mode --------------------------------------------------
//
// USBRIDGE_DECODE_MODE picks who calls into libavcodec:
//   direct -- CAPABILITY_DIRECT_SUBMIT: decode runs on moonlight-common-c's
//             RTP receive thread. Every millisecond spent decoding is a
//             millisecond the video socket isn't drained; once decode costs
//             more than a frame interval (4K HEVC at 120fps on an iGPU), the
//             backlog piles up in the kernel socket buffer -- seconds of
//             video at low bitrates -- and shows up as growing input lag.
//   thread -- no capability bit: moonlight-common-c's own "VideoDec" thread
//             calls dr_submit off its bounded 15-frame queue; an overflow
//             flushes the queue and requests an IDR instead of piling up lag.
//   pull   -- CAPABILITY_PULL_RENDERER (default, what moonlight-qt does): our
//             own thread pulls from that same bounded queue and keeps the
//             decoder fed while it waits for output (win_pull_decode_thread).
enum { WIN_DECODE_DIRECT = 0, WIN_DECODE_THREAD = 1, WIN_DECODE_PULL = 2 };
static int g_playout_buffer_on = 0;
static void set_playout_buffer_on(int on) { g_playout_buffer_on = on; }
static int g_decode_mode = WIN_DECODE_PULL;
static HANDLE g_pull_thread = NULL;
static volatile LONG g_pull_quit = 0;
static DWORD WINAPI win_pull_decode_thread(LPVOID arg);

static int win_decode_mode_from_env(void) {
    const char *v = getenv("USBRIDGE_DECODE_MODE");
    if (v && _stricmp(v, "direct") == 0) return WIN_DECODE_DIRECT;
    if (v && _stricmp(v, "thread") == 0) return WIN_DECODE_THREAD;
    return WIN_DECODE_PULL;
}

// Decode-thread bookkeeping. g_dec_in/g_dec_out count packets libavcodec
// accepted and frames it returned (frame threading holds some in between);
// g_dec_stage/g_dec_stage_ms say what the decode thread is doing right now,
// for win_decode_watchdog's stall report.
enum { DEC_STAGE_IDLE = 0, DEC_STAGE_WAIT, DEC_STAGE_SEND, DEC_STAGE_DRAIN, DEC_STAGE_DELIVER, DEC_STAGE_POLL, DEC_STAGE_SLEEP };
static volatile unsigned int g_dec_in = 0, g_dec_out = 0;
static volatile LONG g_dec_stage = DEC_STAGE_IDLE;
static volatile double g_dec_stage_ms = 0;
static volatile unsigned int g_dec_eagain = 0, g_dec_send_err = 0, g_dec_recv_err = 0;
static volatile int g_dec_last_send_err = 0, g_dec_last_recv_err = 0;
static void dec_stage(LONG st) { g_dec_stage = st; g_dec_stage_ms = win_mono_ms(); }
static int win_decode_drain(void);

static HANDLE g_dec_watchdog = NULL;
static DWORD WINAPI win_decode_watchdog(LPVOID arg);

static void dr_start(void) {
    g_dec_in = g_dec_out = 0;
    g_dec_eagain = g_dec_send_err = g_dec_recv_err = 0;
    InterlockedExchange(&g_pull_quit, 0);
    if (!g_dec_watchdog) g_dec_watchdog = CreateThread(NULL, 0, win_decode_watchdog, NULL, 0, NULL);
    if (g_decode_mode != WIN_DECODE_PULL || g_pull_thread) return;
    g_pull_thread = CreateThread(NULL, 0, win_pull_decode_thread, NULL, 0, NULL);
    // Decode must keep pace with the network even when AI Vision's detector
    // or anything else saturates the CPU; a 125ms scheduling gap overflows
    // moonlight-common-c's 15-frame queue at 120 fps.
    if (g_pull_thread) SetThreadPriority(g_pull_thread, THREAD_PRIORITY_ABOVE_NORMAL);
}
static void dr_stop(void) {
    InterlockedExchange(&g_pull_quit, 1);
    if (g_pull_thread) {
        LiWakeWaitForVideoFrame();
        WaitForSingleObject(g_pull_thread, INFINITE);
        CloseHandle(g_pull_thread);
        g_pull_thread = NULL;
    }
    if (g_dec_watchdog) {
        WaitForSingleObject(g_dec_watchdog, INFINITE);
        CloseHandle(g_dec_watchdog);
        g_dec_watchdog = NULL;
    }
}
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

// g_total_video_bytes: Windows counterpart to moonlight_cgo_shared.h's
// identically-named static -- same "defined in net_graph_stats_windows.c,
// extern-declared here" split as g_last_host_latency_tenths_ms above, for
// the same multiple-definition reason. Read by do_get_total_video_bytes
// (net_graph_windows.go's GetTotalVideoBytes).
extern volatile uint64_t g_total_video_bytes;

#include "bench_frames.h"

// win_decode_send feeds one decode unit to libavcodec without pulling any
// output -- the receive half is win_decode_drain. dr_submit (direct/thread
// modes) does both back to back; the pull-mode thread interleaves them the
// way moonlight-qt's FFmpegVideoDecoder::decoderThreadProc does.
// win_pyrowave_send: one PyroWave access unit -> GPU decode straight into a
// ring image on the renderer's device (pyrowave_decode_windows.c) -> renderer,
// sampled in place (external mode 2: same device, GENERAL layout). The decode
// call only records and submits GPU work; the renderer waits on the timeline
// semaphore value. No CPU copy of the picture anywhere -- AI Vision sampling
// is not available for PyroWave streams.
static uint8_t *g_pw_au = NULL;
static size_t   g_pw_au_cap = 0;
static int win_pyrowave_send(PDECODE_UNIT du) {
    size_t total = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) total += (size_t)e->length;
    if (total == 0) return DR_OK;
    if (g_pw_au_cap < total) {
        uint8_t *grown = (uint8_t *)realloc(g_pw_au, total);
        if (!grown) return DR_OK;
        g_pw_au = grown;
        g_pw_au_cap = total;
    }
    size_t off = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) { memcpy(g_pw_au + off, e->data, (size_t)e->length); off += (size_t)e->length; }

    dec_stage(DEC_STAGE_SEND);
    void *img = NULL, *sem = NULL, *slot = NULL;
    int vkfmt = 0, w = 0, h = 0, narrow = 1;
    uint64_t val = 0;
    g_dec_in++;
    if (!pyrowave_win_decode(g_pw_au, total, &img, &vkfmt, &sem, &val, &w, &h, &narrow, &slot)) {
        g_dec_out++;
        return DR_OK; // every frame is a keyframe: nothing to recover
    }
    g_dec_out++;
    dec_stage(DEC_STAGE_DELIVER);
    int native_overlay_active = vk_video_is_active() || gl_video_is_active();
    if (native_overlay_active) g_vk_overlay_seen = 1;
    ++g_vk_session_frames;
    goVTFrame(NULL, w, h, 0); // stats + overlay bootstrap on frame 1
    // layout 1 = VK_IMAGE_LAYOUT_GENERAL
    if (!vk_video_try_submit_vkframe(img, vkfmt, 1, w, h, narrow, sem, val, 2,
                                      slot, pyrowave_win_release_slot)) {
        pyrowave_win_release_slot(slot);
    }
    if (++g_av_frame_cnt == 1) {
        char msg[96];
        snprintf(msg, sizeof(msg), "pyrowave: first frame handed to the renderer (%dx%d)", w, h);
        goVTLog(msg);
    }
    return DR_OK;
}

static int win_decode_send(PDECODE_UNIT du) {
    g_last_host_latency_tenths_ms = du->frameHostProcessingLatency;
    g_total_video_bytes += (uint64_t)du->fullLength;
    bench_frames_note(du);
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

    if (g_using_pyrowave) return win_pyrowave_send(du);

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
    // EAGAIN means libavcodec did NOT take the packet: with frame threads its
    // input is full until a finished frame is received. Dropping the packet
    // there (as this used to, treating EAGAIN as success) silently breaks
    // the reference chain with no IDR request. Drain output and retry.
    int ret;
    for (int tries = 0;; tries++) {
        dec_stage(DEC_STAGE_SEND);
        ret = avcodec_send_packet(ctx, pkt);
        if (ret != AVERROR(EAGAIN) || tries >= 100) break;
        g_dec_eagain++;
        if (win_decode_drain() == 0) { dec_stage(DEC_STAGE_SLEEP); Sleep(1); }
    }
    av_packet_free(&pkt);
    av_free(data);
    if (ret == 0) g_dec_in++;
    double t_decode1 = win_mono_ms();
    if (t_decode1 - t_decode0 > WIN_DELIVER_SLOW_MS) {
        char msg[96];
        snprintf(msg, sizeof(msg), "SLOW avcodec_send_packet %.0fms", t_decode1 - t_decode0);
        goVTLog(msg);
    }
    if (ret < 0) {
        g_dec_send_err++;
        g_dec_last_send_err = ret;
        return DR_NEED_IDR;
    }
    return DR_OK;
}

// win_decode_drain delivers every frame libavcodec has ready and returns how
// many it delivered (0 = the decoder wants more input first).
static int win_decode_drain(void) {
    AVCodecContext *ctx = g_avctx;
    if (!ctx) return 0;
    AVFrame *frame = av_frame_alloc();
    if (!frame) return 0;
    int n = 0, err;
    for (;;) {
        dec_stage(DEC_STAGE_DRAIN);
        double t_recv0 = win_mono_ms();
        err = avcodec_receive_frame(ctx, frame);
        double t_recv = win_mono_ms() - t_recv0;
        if (t_recv > WIN_DELIVER_SLOW_MS) {
            char msg[96];
            snprintf(msg, sizeof(msg), "SLOW avcodec_receive_frame %.0fms", t_recv);
            goVTLog(msg);
        }
        if (err != 0) break;
        g_dec_out++;
        dec_stage(DEC_STAGE_DELIVER);
        win_deliver_frame(frame);
        av_frame_unref(frame);
        n++;
    }
    if (err != AVERROR(EAGAIN) && err != AVERROR_EOF) {
        g_dec_recv_err++;
        g_dec_last_recv_err = err;
    }
    av_frame_free(&frame);
    return n;
}

static int dr_submit(PDECODE_UNIT du) {
    int ret = win_decode_send(du);
    if (ret == DR_OK) win_decode_drain();
    return ret;
}

// Pull mode's decode thread, modeled on moonlight-qt's
// FFmpegVideoDecoder::decoderThreadProc: block for input only once every
// submitted frame has come back out; otherwise poll for output and keep
// feeding newly arrived input while the GPU works.
static DWORD WINAPI win_pull_decode_thread(LPVOID arg) {
    (void)arg;
    while (!InterlockedCompareExchange(&g_pull_quit, 0, 0)) {
        VIDEO_FRAME_HANDLE handle;
        PDECODE_UNIT du;
        // A decode error can swallow frames for good, so never trust
        // out to catch up with in by more than libavcodec's thread depth.
        unsigned int pending = g_dec_in - g_dec_out;
        if (pending == 0 || pending > 8) {
            g_dec_out = g_dec_in;
            dec_stage(DEC_STAGE_WAIT);
            if (!LiWaitForNextVideoFrame(&handle, &du)) continue;
            LiCompleteVideoFrame(handle, win_decode_send(du));
        }
        while (g_dec_in != g_dec_out && !InterlockedCompareExchange(&g_pull_quit, 0, 0)) {
            if (win_decode_drain() > 0) break;
            dec_stage(DEC_STAGE_POLL);
            if (LiPollNextVideoFrame(&handle, &du)) {
                LiCompleteVideoFrame(handle, win_decode_send(du));
            } else {
                dec_stage(DEC_STAGE_SLEEP);
                Sleep(1);
            }
        }
    }
    dec_stage(DEC_STAGE_IDLE);
    return 0;
}

// win_decode_watchdog: logs what the decode thread is doing whenever no
// frame has come out of libavcodec for a second -- the evidence for any
// "no video frame for Ns" reconnect.
static DWORD WINAPI win_decode_watchdog(LPVOID arg) {
    (void)arg;
    static const char *names[] = { "idle", "wait-for-input", "send_packet", "receive_frame", "deliver", "poll-input", "sleep" };
    unsigned int last_out = g_dec_out;
    double last_change = win_mono_ms(), last_log = 0;
    while (!InterlockedCompareExchange(&g_pull_quit, 0, 0)) {
        Sleep(250);
        double now = win_mono_ms();
        if (g_dec_out != last_out) { last_out = g_dec_out; last_change = now; continue; }
        if (now - last_change < 1000 || now - last_log < 1000) continue;
        last_log = now;
        LONG st = g_dec_stage;
        // RTP counters tell "no packets arriving" (host/tunnel) apart from
        // "packets arrive but no frame completes" (loss/FEC) apart from "frames
        // complete but aren't pulled" (this side).
        const RTP_VIDEO_STATS *rs = LiGetRTPVideoStats();
        uint32_t rtt = 0, rttVar = 0;
        LiGetEstimatedRttInfo(&rtt, &rttVar);
        char msg[480];
        snprintf(msg, sizeof(msg),
                 "decode stall: no frame out for %.0fms, thread in %s for %.0fms, in=%u out=%u eagain=%u send_err=%u(last %d) recv_err=%u(last %d) | rtp video=%u fec=%u fecRecovered=%u fecFailed=%u oos=%u invalid=%u rtt=%ums",
                 now - last_change, (st >= 0 && st <= DEC_STAGE_SLEEP) ? names[st] : "?", now - g_dec_stage_ms,
                 g_dec_in, g_dec_out, g_dec_eagain, g_dec_send_err, g_dec_last_send_err, g_dec_recv_err, g_dec_last_recv_err,
                 rs ? rs->packetCountVideo : 0, rs ? rs->packetCountFec : 0, rs ? rs->packetCountFecRecovered : 0,
                 rs ? rs->packetCountFecFailed : 0, rs ? rs->packetCountOOS : 0, rs ? rs->packetCountInvalid : 0, rtt);
        goVTLog(msg);
    }
    return 0;
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
uint64_t do_get_total_video_bytes(void);

// ── LiStartConnection entrypoint ─────────────────────────────────────────────

#include <stdio.h>
static int tailnet_packet_size(const char *address) {
    // Tailnet (100.64/10, or the 127.0.0.1 proxy the client reaches it by): the
    // tunnel MTU is raised to 1440 (TS_DEBUG_MTU, on both ends), so 1392 + RTP
    // header + IP/UDP fits; 1200 elsewhere.
    unsigned a = 0, b = 0;
    if (address && sscanf(address, "%u.%u.", &a, &b) == 2 && (a == 127 || (a == 100 && b >= 64 && b < 128))) return 1392;
    return 1200;
}

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
    cfg.packetSize = tailnet_packet_size(address); cfg.streamingRemotely = STREAM_CFG_AUTO;
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
    dr.capabilities = CAPABILITY_REFERENCE_FRAME_INVALIDATION_AVC | CAPABILITY_REFERENCE_FRAME_INVALIDATION_HEVC | CAPABILITY_REFERENCE_FRAME_INVALIDATION_AV1;
    g_decode_mode = win_decode_mode_from_env();
    if (g_decode_mode == WIN_DECODE_DIRECT) dr.capabilities |= CAPABILITY_DIRECT_SUBMIT;
    if (g_decode_mode == WIN_DECODE_PULL) {
        // LiStartConnection rejects a pull renderer that also has a
        // submitDecodeUnit callback (Connection.c) -- win_pull_decode_thread
        // pulls the frames itself.
        dr.capabilities |= CAPABILITY_PULL_RENDERER;
        dr.submitDecodeUnit = NULL;
    }
    // moonlight-common-c's playout buffer (our fork's VideoDepacketizer.c)
    // sleeps before handing each frame to the decoder -- delay the official
    // clients never add. Driven by the video settings checkbox (off by
    // default, see service.SetPlayoutBufferEnabled); the fork re-reads the
    // variable at the start of every stream.
    _putenv(g_playout_buffer_on ? "USBRIDGE_PLAYOUT_BUFFER=1" : "USBRIDGE_PLAYOUT_BUFFER=0");
    {
        static const char *mode_names[] = { "direct (RTP receive thread)", "thread (moonlight-common-c VideoDec)", "pull (own decode thread, moonlight-qt style)" };
        char msg[160];
        snprintf(msg, sizeof(msg), "libavcodec/win: decode mode = %s, playout buffer %s",
                 mode_names[g_decode_mode], strcmp(getenv("USBRIDGE_PLAYOUT_BUFFER"), "0") == 0 ? "off" : "on");
        goVTLog(msg);
    }

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
    InterlockedIncrement(&g_stream_gen); // in-flight readbacks belong to a dead stream now
    LiStopConnection();
    win_readback_cancel();
    if (g_sws) { sws_freeContext(g_sws); g_sws = NULL; }
    if (g_avctx) avcodec_free_context(&g_avctx);
    if (g_hw_dev_ctx) av_buffer_unref(&g_hw_dev_ctx);
    // The shared Vulkan hwaccel device (vk_hwdev_bridge_windows.c) deliberately
    // survives past this stream -- it's also adopted by vk_video_impl_windows.c's
    // overlay for presentation, and expensive to recreate. g_using_vulkan_decode
    // resets so the next session's win_av_init() re-probes cleanly (e.g. if
    // the codec changed to AV1, which has no Vulkan decode extension here).
    g_using_vulkan_decode = 0;
    g_using_d3dx_decode = 0;
    g_using_pyrowave = 0;
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
static void do_send_controller_arrival(unsigned short cn, unsigned short am, unsigned char ctype,
    unsigned int supportedButtonFlags, unsigned short capabilities)
{
    LiSendControllerArrivalEvent((uint8_t)cn, am, ctype, supportedButtonFlags, capabilities);
}
static void do_send_utf8_text(const char *text, unsigned int len) { LiSendUtf8TextEvent(text, len); }
static void do_send_pen(unsigned char eventType, unsigned char toolType, unsigned char penButtons,
                         float x, float y, float pressureOrDistance,
                         unsigned short rotation, unsigned char tilt)
{
    LiSendPenEvent(eventType, toolType, penButtons, x, y, pressureOrDistance, 0.0f, 0.0f, rotation, tilt);
}
// Raw HID devices rebuilt by a USBridge host (see moonlight_rawhid.go).
static int do_host_supports_raw_hid(void)
{
    return (LiGetHostFeatureFlags() & LI_FF_USBRIDGE_RAW_HID) != 0;
}
static int do_send_raw_hid(unsigned char kind, unsigned char slot, unsigned char endpoint,
                           unsigned short total, unsigned short offset,
                           const unsigned char *data, unsigned short length, int reliable)
{
    return LiSendRawHidEvent(kind, slot, endpoint, total, offset, data, length, reliable != 0);
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
	case format&0xF0000 != 0: // VIDEO_FORMAT_MASK_PYROWAVE
		return models.VideoModePyroWave, true
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

		playoutOn := 0
		if PlayoutBufferEnabled() {
			playoutOn = 1
		}
		C.set_playout_buffer_on(C.int(playoutOn))
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
		liRawHIDEpoch.Add(1)

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
func (w *MoonlightCgoWrapper) SendMoonlightControllerArrival(
	controllerNumber uint16, activeGamepadMask uint16, controllerType uint8,
	supportedButtonFlags uint32, capabilities uint16,
) {
	if !liStartConnectionActive.Load() {
		return
	}
	C.do_send_controller_arrival(
		C.ushort(controllerNumber), C.ushort(activeGamepadMask), C.uchar(controllerType),
		C.uint(supportedButtonFlags), C.ushort(capabilities),
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

func (w *MoonlightCgoWrapper) RawHIDEpoch() uint64 {
	if !liStartConnectionActive.Load() || C.do_host_supports_raw_hid() == 0 {
		return 0
	}
	return liRawHIDEpoch.Load()
}

func (w *MoonlightCgoWrapper) SendMoonlightRawHID(kind, slot, endpoint uint8, total, offset uint16, data []byte, reliable bool) bool {
	if !liStartConnectionActive.Load() {
		return false
	}
	var p *C.uchar
	if len(data) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	rel := C.int(0)
	if reliable {
		rel = 1
	}
	return C.do_send_raw_hid(C.uchar(kind), C.uchar(slot), C.uchar(endpoint),
		C.ushort(total), C.ushort(offset), p, C.ushort(len(data)), rel) == 0
}

// NegotiatedVideoCodecName returns the codec moonlight-common-c actually
// negotiated with the server for the current session (from dr_setup's
// NegotiatedVideoFormat), matching the macOS/Linux implementation.
func (w *MoonlightCgoWrapper) NegotiatedVideoCodecName() (string, bool) {
	return windowsNegotiatedVideoCodecNameNow()
}

// windowsNegotiatedVideoCodecNameNow is NegotiatedVideoCodecName's
// package-level body, split out so net_graph_windows.go's netGraphCodecFn
// hook (no MoonlightCgoWrapper instance to call a method on) can wire it
// directly -- matches moonlight_cgo_wrapper.go's negotiatedVideoCodecNameNow.
func windowsNegotiatedVideoCodecNameNow() (string, bool) {
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
	if usbapi.LiveFrameWanted() || usbapi.LiveLumaWanted() || usbapi.LiveRGBAWanted() {
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
