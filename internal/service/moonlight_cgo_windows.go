//go:build windows && cgo

package service

/*
#cgo pkg-config: opus openssl
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build -L${SRCDIR}/../../moonlight-common-c/build/enet -lmoonlight-common-c -lenet -lws2_32 -lwinmm
#cgo LDFLAGS: -lavcodec -lavutil -lswscale
#cgo LDFLAGS: -lole32 -loleaut32 -luuid -lmfplat -lmfuuid

#define COBJMACROS
#define INITGUID
#include <windows.h>
#include <mfapi.h>
#include <mmdeviceapi.h>
#include <audioclient.h>
#include <libavcodec/avcodec.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_d3d11va.h>
#include <libavutil/frame.h>
#include <libavutil/imgutils.h>
#include <libswscale/swscale.h>
#include <Limelight.h>
#include <opus_multistream.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

extern void goMoonlightStage(int stage, int result, int errCode);
extern void goMoonlightConnected(void);
extern void goMoonlightTerminated(int errCode);
extern void goVTLog(char *msg);
extern void goVTFrame(uint8_t *rgba, int width, int height);

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
static void cl_log(const char *fmt, ...)   { (void)fmt; }

// ═══════════════════════════════════════════════════════════════════════════════
// WASAPI audio output
// ═══════════════════════════════════════════════════════════════════════════════

static IAudioClient       *g_wa_client  = NULL;
static IAudioRenderClient *g_wa_render  = NULL;
static UINT32              g_wa_frames  = 0;
static CRITICAL_SECTION    g_wa_cs;
static int                 g_wa_cs_init = 0;

static void wasapi_init(int channels, int sample_rate) {
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
    goVTLog((char*)"WASAPI: audio client started (S16LE native output)");
}

static void wasapi_teardown(void) {
    EnterCriticalSection(&g_wa_cs);
    IAudioClient       *c = g_wa_client; g_wa_client = NULL;
    IAudioRenderClient *r = g_wa_render; g_wa_render = NULL;
    LeaveCriticalSection(&g_wa_cs);
    if (c) { IAudioClient_Stop(c); IAudioClient_Release(c); }
    if (r) { IAudioRenderClient_Release(r); }
    goVTLog((char*)"WASAPI: audio client stopped");
}

static void wasapi_write(const opus_int16 *pcm, int samples) {
    EnterCriticalSection(&g_wa_cs);
    IAudioClient       *c = g_wa_client;
    IAudioRenderClient *r = g_wa_render;
    LeaveCriticalSection(&g_wa_cs);
    if (!c || !r) return;

    UINT32 padding = 0;
    IAudioClient_GetCurrentPadding(c, &padding);
    UINT32 avail = g_wa_frames - padding;
    if ((UINT32)samples > avail) return; // buffer full — drop frame

    BYTE *buf = NULL;
    if (FAILED(IAudioRenderClient_GetBuffer(r, (UINT32)samples, &buf)) || !buf) return;
    memcpy(buf, pcm, (size_t)samples * (size_t)g_audio_channels * 2);
    IAudioRenderClient_ReleaseBuffer(r, (UINT32)samples, 0);
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
// libavcodec H.264 decoder with D3D11VA hardware acceleration
// ═══════════════════════════════════════════════════════════════════════════════

static AVCodecContext    *g_avctx       = NULL;
static struct SwsContext *g_sws         = NULL;
static AVBufferRef       *g_hw_dev_ctx  = NULL;
static enum AVPixelFormat g_hw_pix_fmt  = AV_PIX_FMT_NONE;
static int                g_av_w        = 0;
static int                g_av_h        = 0;
static CRITICAL_SECTION   g_av_cs;
static int                g_av_cs_init  = 0;
static uint64_t           g_av_frame_cnt = 0;

static enum AVPixelFormat win_get_hw_format(AVCodecContext *ctx,
                                             const enum AVPixelFormat *fmts) {
    (void)ctx;
    for (const enum AVPixelFormat *p = fmts; *p != AV_PIX_FMT_NONE; p++) {
        if (*p == g_hw_pix_fmt) return *p;
    }
    return AV_PIX_FMT_NONE;
}

static void win_av_init(void) {
    if (!g_av_cs_init) { InitializeCriticalSection(&g_av_cs); g_av_cs_init = 1; }
    if (g_avctx) return;

    const AVCodec *codec = NULL;

    // Try D3D11VA hardware decoder.
    const AVCodec *hw_codec = avcodec_find_decoder_by_name("h264_d3d11va");
    if (hw_codec) {
        AVBufferRef *hw_ctx = NULL;
        if (av_hwdevice_ctx_create(&hw_ctx, AV_HWDEVICE_TYPE_D3D11VA, NULL, NULL, 0) == 0) {
            AVCodecContext *test = avcodec_alloc_context3(hw_codec);
            g_hw_pix_fmt = AV_PIX_FMT_D3D11;
            test->hw_device_ctx = av_buffer_ref(hw_ctx);
            test->get_format = win_get_hw_format;
            if (avcodec_open2(test, hw_codec, NULL) == 0) {
                codec = hw_codec;
                if (g_hw_dev_ctx) av_buffer_unref(&g_hw_dev_ctx);
                g_hw_dev_ctx = hw_ctx;
                goVTLog((char*)"libavcodec/win: using h264_d3d11va (hardware)");
            } else {
                av_buffer_unref(&hw_ctx);
                g_hw_pix_fmt = AV_PIX_FMT_NONE;
            }
            avcodec_free_context(&test);
        }
    }
    if (!codec) {
        codec = avcodec_find_decoder(AV_CODEC_ID_H264);
        g_hw_pix_fmt = AV_PIX_FMT_NONE;
        goVTLog((char*)"libavcodec/win: using h264 software fallback");
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

static void win_deliver_frame(AVFrame *frame) {
    AVFrame *sw = NULL;
    if (frame->format == AV_PIX_FMT_D3D11) {
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
            if (++g_av_frame_cnt == 1) goVTLog((char*)"libavcodec/win: first RGBA frame decoded");
            goVTFrame(rgba, w, h);
            free(rgba);
        }
    }
    if (sw) av_frame_free(&sw);
}

// ── Video callbacks ───────────────────────────────────────────────────────────

static int  dr_setup(int fmt, int w, int h, int rate, void *ctx, int flags) {
    (void)fmt; (void)w; (void)h; (void)rate; (void)ctx; (void)flags; return 0;
}
static void dr_start(void)   {}
static void dr_stop(void)    {}
static void dr_cleanup(void) {}

static int dr_submit(PDECODE_UNIT du) {
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
    int ret = avcodec_send_packet(ctx, pkt);
    av_packet_free(&pkt);
    av_free(data);
    if (ret < 0 && ret != AVERROR(EAGAIN)) return DR_NEED_IDR;

    AVFrame *frame = av_frame_alloc();
    while (avcodec_receive_frame(ctx, frame) == 0) {
        win_deliver_frame(frame);
        av_frame_unref(frame);
    }
    av_frame_free(&frame);
    return DR_OK;
}

// ── LiStartConnection entrypoint ─────────────────────────────────────────────

static int do_li_start(
    const char *address, const char *appVersion, const char *gfeVersion,
    const char *rtspSessionUrl, int serverCodecModeSupport,
    int width, int height, int fps, int bitrate,
    const unsigned char *rikey, int rikeyid, uintptr_t unused
) {
    (void)unused;
    win_av_init();

    SERVER_INFORMATION srv; LiInitializeServerInformation(&srv);
    srv.address = address; srv.serverInfoAppVersion = appVersion;
    srv.serverInfoGfeVersion = gfeVersion; srv.rtspSessionUrl = rtspSessionUrl;
    srv.serverCodecModeSupport = serverCodecModeSupport;

    STREAM_CONFIGURATION cfg; LiInitializeStreamConfiguration(&cfg);
    cfg.width = width; cfg.height = height; cfg.fps = fps; cfg.bitrate = bitrate;
    cfg.packetSize = 1200; cfg.streamingRemotely = STREAM_CFG_AUTO;
    cfg.audioConfiguration = AUDIO_CONFIGURATION_STEREO;
    cfg.supportedVideoFormats = VIDEO_FORMAT_H264;
    cfg.clientRefreshRateX100 = fps * 100; cfg.encryptionFlags = ENCFLG_NONE;
    if (rikey) {
        memcpy(cfg.remoteInputAesKey, rikey, 16);
        cfg.remoteInputAesIv[0] = (char)( rikeyid        & 0xff);
        cfg.remoteInputAesIv[1] = (char)((rikeyid >>  8) & 0xff);
        cfg.remoteInputAesIv[2] = (char)((rikeyid >> 16) & 0xff);
        cfg.remoteInputAesIv[3] = (char)((rikeyid >> 24) & 0xff);
    }

    DECODER_RENDERER_CALLBACKS dr; LiInitializeVideoCallbacks(&dr);
    dr.setup = dr_setup; dr.start = dr_start; dr.stop = dr_stop;
    dr.cleanup = dr_cleanup; dr.submitDecodeUnit = dr_submit;
    dr.capabilities = CAPABILITY_DIRECT_SUBMIT;

    AUDIO_RENDERER_CALLBACKS ar; LiInitializeAudioCallbacks(&ar);
    ar.init = ar_init; ar.start = ar_start; ar.stop = ar_stop;
    ar.cleanup = ar_cleanup; ar.decodeAndPlaySample = ar_decode;

    CONNECTION_LISTENER_CALLBACKS cl; LiInitializeConnectionCallbacks(&cl);
    cl.stageStarting = cl_stage_starting; cl.stageComplete = cl_stage_complete;
    cl.stageFailed = cl_stage_failed; cl.connectionStarted = cl_connected;
    cl.connectionTerminated = cl_terminated; cl.logMessage = cl_log;

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
*/
import "C"

import (
	"fmt"
	"image"
	"os"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/sirupsen/logrus"
)

var liStartConnectionActive atomic.Bool

var (
	activeStreamDone    chan struct{}
	activeStreamOnce    sync.Once
	activeStreamTermErr error
)

var (
	vtFrameCallback   func(image.Image)
	vtFrameCallbackMu sync.Mutex
)

func closeActiveStreamDone() {
	activeStreamOnce.Do(func() { close(activeStreamDone) })
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
	width, height, fps, bitrate int,
	pipeWrite *os.File,
	audioPipeWrite *os.File,
	onStop func(error),
) error {
	activeStreamDone = make(chan struct{})
	activeStreamOnce = sync.Once{}
	activeStreamTermErr = nil

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

		logrus.Infof("🌕 [Moonlight/CGO/Win] LiStartConnection: host=%s %dx%d@%d bitrate=%d",
			w.host, width, height, fps, bitrate)

		ret := C.do_li_start(
			host, appVer, gfeVer, rtsp,
			C.int(serverCodecModeSupport),
			C.int(width), C.int(height), C.int(fps), C.int(bitrate),
			cRikey, C.int(1), C.uintptr_t(0),
		)

		if int(ret) != 0 {
			logrus.Errorf("🌕 [Moonlight/CGO/Win] LiStartConnection FAILED: code=%d", int(ret))
			if pipeWrite != nil {
				_ = pipeWrite.Close()
			}
			if onStop != nil {
				onStop(fmt.Errorf("LiStartConnection error code %d", int(ret)))
			}
			return
		}

		logrus.Info("🌕 [Moonlight/CGO/Win] ✅ streams active")
		liStartConnectionActive.Store(true)

		<-activeStreamDone

		logrus.Info("🌕 [Moonlight/CGO/Win] termination received — stopping")
		C.do_li_stop()

		vtFrameCallbackMu.Lock()
		vtFrameCallback = nil
		vtFrameCallbackMu.Unlock()

		liStartConnectionActive.Store(false)
		if pipeWrite != nil {
			_ = pipeWrite.Close()
		}
		if onStop != nil {
			onStop(activeStreamTermErr)
		}
	}()
	return nil
}

func (w *MoonlightCgoWrapper) StopStream() {
	if !liStartConnectionActive.Load() {
		return
	}
	C.do_li_stop()
	closeActiveStreamDone()
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
func (w *MoonlightCgoWrapper) IsInputActive() bool { return liStartConnectionActive.Load() }

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
func goMoonlightConnected() { logrus.Info("🌕 [Moonlight] stream connected ✅") }

//export goMoonlightTerminated
func goMoonlightTerminated(errCode C.int) {
	reason := "unknown"
	switch int(errCode) {
	case 0:
		reason = "clean disconnect"
	}
	logrus.Errorf("🌕 [Moonlight] ❌ terminated: code=%d (%s)", int(errCode), reason)
	activeStreamTermErr = fmt.Errorf("stream terminated: code=%d (%s)", int(errCode), reason)
	closeActiveStreamDone()
}

//export goVTLog
func goVTLog(msg *C.char) { logrus.Infof("🎬 [Moonlight/HW/Win] %s", C.GoString(msg)) }

var vtFrameCount int64

//export goVTFrame
func goVTFrame(rgba *C.uint8_t, width, height C.int) {
	vtFrameCallbackMu.Lock()
	cb := vtFrameCallback
	vtFrameCallbackMu.Unlock()
	if cb == nil {
		return
	}
	w, h := int(width), int(height)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	n := w * h * 4
	copy(img.Pix, (*[1 << 30]byte)(unsafe.Pointer(rgba))[:n:n])
	cnt := atomic.AddInt64(&vtFrameCount, 1)
	if cnt == 1 {
		logrus.Infof("🎬 [Moonlight/HW/Win] ✅ first RGBA frame — %dx%d", w, h)
	}
	cb(img)
}
