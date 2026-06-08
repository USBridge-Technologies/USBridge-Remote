//go:build android && cgo

package service

/*
#cgo pkg-config: opus openssl
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build/android -lmoonlight-common-c -lenet
#cgo LDFLAGS: -lmediandk -laaudio -lpthread -lm -ldl -landroid

#include <media/NdkMediaCodec.h>
#include <media/NdkMediaFormat.h>
#include <aaudio/AAudio.h>
#include <android/log.h>
#include <Limelight.h>
#include <opus_multistream.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

extern void goMoonlightStage(int stage, int result, int errCode);
extern void goMoonlightConnected(void);
extern void goMoonlightTerminated(int errCode);
extern void goVTLog(char *msg);
extern void goVTFrame(uint8_t *rgba, int width, int height, int stride);

// ── Shared state ──────────────────────────────────────────────────────────────

static volatile int    g_li_active       = 0;
static volatile int    g_audio_muted     = 0;
static OpusMSDecoder  *g_opus_ms_decoder = NULL;
static int             g_audio_channels  = 2;

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
// AAudio output
// ═══════════════════════════════════════════════════════════════════════════════

static AAudioStream *g_aa_stream = NULL;

static void aaudio_init(int channels, int sample_rate) {
    if (g_aa_stream) return;
    AAudioStreamBuilder *builder = NULL;
    if (AAudio_createStreamBuilder(&builder) != AAUDIO_OK) return;

    AAudioStreamBuilder_setDirection(builder,      AAUDIO_DIRECTION_OUTPUT);
    AAudioStreamBuilder_setSharingMode(builder,    AAUDIO_SHARING_MODE_SHARED);
    AAudioStreamBuilder_setFormat(builder,         AAUDIO_FORMAT_PCM_I16);
    AAudioStreamBuilder_setChannelCount(builder,   channels);
    AAudioStreamBuilder_setSampleRate(builder,     sample_rate);
    AAudioStreamBuilder_setPerformanceMode(builder,AAUDIO_PERFORMANCE_MODE_LOW_LATENCY);

    AAudioStream *stream = NULL;
    if (AAudioStreamBuilder_openStream(builder, &stream) == AAUDIO_OK) {
        AAudioStream_requestStart(stream);
        g_aa_stream = stream;
        goVTLog((char*)"AAudio: stream started (S16LE native output)");
    }
    AAudioStreamBuilder_delete(builder);
}

static void aaudio_teardown(void) {
    if (!g_aa_stream) return;
    AAudioStream_requestStop(g_aa_stream);
    AAudioStream_close(g_aa_stream);
    g_aa_stream = NULL;
    goVTLog((char*)"AAudio: stream closed");
}

static void aaudio_write(const opus_int16 *pcm, int samples) {
    if (!g_aa_stream) return;
    AAudioStream_write(g_aa_stream, pcm, samples, 10000000LL); // 10 ms timeout
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
    aaudio_init(cfg->channelCount, (int)cfg->sampleRate);
    return 0;
}
static void ar_start(void)   {}
static void ar_stop(void)    {}
static void ar_cleanup(void) {
    aaudio_teardown();
    if (g_opus_ms_decoder) { opus_multistream_decoder_destroy(g_opus_ms_decoder); g_opus_ms_decoder = NULL; }
}
static void ar_decode(char *data, int len) {
    if (!g_opus_ms_decoder) return;
    opus_int16 pcm[5760 * 8];
    int samples = opus_multistream_decode(g_opus_ms_decoder,
        (const unsigned char *)data, len, pcm, 5760, 0);
    if (samples <= 0) return;
    if (g_audio_muted) memset(pcm, 0, samples * g_audio_channels * 2);
    aaudio_write(pcm, samples);
}

// ═══════════════════════════════════════════════════════════════════════════════
// AMediaCodec H.264 hardware decoder (Android NDK)
// ═══════════════════════════════════════════════════════════════════════════════

static AMediaCodec  *g_amc        = NULL;
static int           g_amc_w      = 0;
static int           g_amc_h      = 0;
static uint64_t      g_amc_pts    = 0;

static void amc_init(int width, int height) {
    if (g_amc) return;
    g_amc_w = width; g_amc_h = height;

    AMediaFormat *fmt = AMediaFormat_new();
    AMediaFormat_setString(fmt, AMEDIAFORMAT_KEY_MIME, "video/avc");
    AMediaFormat_setInt32(fmt,  AMEDIAFORMAT_KEY_WIDTH,  width);
    AMediaFormat_setInt32(fmt,  AMEDIAFORMAT_KEY_HEIGHT, height);
    AMediaFormat_setInt32(fmt,  AMEDIAFORMAT_KEY_COLOR_FORMAT, 19); // COLOR_FormatYUV420Planar

    g_amc = AMediaCodec_createDecoderByType("video/avc");
    if (!g_amc) { AMediaFormat_delete(fmt); goVTLog((char*)"AMediaCodec: create FAILED"); return; }

    if (AMediaCodec_configure(g_amc, fmt, NULL, NULL, 0) != AMEDIA_OK) {
        AMediaCodec_delete(g_amc); g_amc = NULL;
        AMediaFormat_delete(fmt);
        goVTLog((char*)"AMediaCodec: configure FAILED");
        return;
    }
    AMediaCodec_start(g_amc);
    AMediaFormat_delete(fmt);
    goVTLog((char*)"AMediaCodec: decoder started");
}

static void amc_teardown(void) {
    if (!g_amc) return;
    AMediaCodec_stop(g_amc);
    AMediaCodec_delete(g_amc);
    g_amc = NULL;
    goVTLog((char*)"AMediaCodec: decoder stopped");
}

// YUV420p → RGBA (simple I420 conversion for RGBA output to goVTFrame)
static void yuv420_to_rgba(const uint8_t *y, const uint8_t *u, const uint8_t *v,
                             int y_stride, int uv_stride, int w, int h,
                             uint8_t *rgba)
{
    for (int row = 0; row < h; row++) {
        for (int col = 0; col < w; col++) {
            int yv = y[row * y_stride + col];
            int uv_row = row / 2, uv_col = col / 2;
            int uval = u[uv_row * uv_stride + uv_col] - 128;
            int vval = v[uv_row * uv_stride + uv_col] - 128;
            int r = yv + (int)(1.402f * vval);
            int g = yv - (int)(0.344f * uval) - (int)(0.714f * vval);
            int b = yv + (int)(1.772f * uval);
            uint8_t *p = rgba + (row * w + col) * 4;
            p[0] = r < 0 ? 0 : r > 255 ? 255 : (uint8_t)r;
            p[1] = g < 0 ? 0 : g > 255 ? 255 : (uint8_t)g;
            p[2] = b < 0 ? 0 : b > 255 ? 255 : (uint8_t)b;
            p[3] = 255;
        }
    }
}

static int dr_submit(PDECODE_UNIT du) {
    if (!g_amc) {
        // Width/height from dr_setup not available here; use globals set externally.
        amc_init(g_amc_w > 0 ? g_amc_w : 1920, g_amc_h > 0 ? g_amc_h : 1080);
        if (!g_amc) return DR_NEED_IDR;
    }

    // Queue all LENTRY buffers into input buffer.
    ssize_t idx = AMediaCodec_dequeueInputBuffer(g_amc, 5000); // 5 ms timeout
    if (idx < 0) return DR_OK;

    size_t buf_size = 0;
    uint8_t *buf = AMediaCodec_getInputBuffer(g_amc, (size_t)idx, &buf_size);
    if (!buf) { AMediaCodec_queueInputBuffer(g_amc, (size_t)idx, 0, 0, 0, 0); return DR_OK; }

    size_t written = 0;
    for (PLENTRY e = du->bufferList; e && written < buf_size; e = e->next) {
        size_t n = e->length;
        if (written + n > buf_size) n = buf_size - written;
        memcpy(buf + written, e->data, n);
        written += n;
    }
    AMediaCodec_queueInputBuffer(g_amc, (size_t)idx, 0, written, g_amc_pts++, 0);

    // Drain output.
    AMediaCodecBufferInfo info;
    ssize_t out_idx = AMediaCodec_dequeueOutputBuffer(g_amc, &info, 0);
    if (out_idx >= 0) {
        size_t out_size = 0;
        const uint8_t *out = AMediaCodec_getOutputBuffer(g_amc, (size_t)out_idx, &out_size);
        if (out && g_amc_w > 0 && g_amc_h > 0) {
            int w = g_amc_w, h = g_amc_h;
            uint8_t *rgba = (uint8_t *)malloc((size_t)w * (size_t)h * 4);
            if (rgba) {
                int y_size  = w * h;
                int uv_size = (w / 2) * (h / 2);
                const uint8_t *yp = out;
                const uint8_t *up = out + y_size;
                const uint8_t *vp = out + y_size + uv_size;
                yuv420_to_rgba(yp, up, vp, w, w / 2, w, h, rgba);
                goVTFrame(rgba, w, h, w * 4);
                free(rgba);
            }
        }
        AMediaCodec_releaseOutputBuffer(g_amc, (size_t)out_idx, 0);
    }
    return DR_OK;
}

static int  dr_setup(int fmt, int w, int h, int rate, void *ctx, int flags) {
    (void)fmt; (void)rate; (void)ctx; (void)flags;
    g_amc_w = w; g_amc_h = h;
    amc_init(w, h);
    return 0;
}
static void dr_start(void)   {}
static void dr_stop(void)    {}
static void dr_cleanup(void) { amc_teardown(); }

// ── LiStartConnection entrypoint ─────────────────────────────────────────────

static int do_li_start(
    const char *address, const char *appVersion, const char *gfeVersion,
    const char *rtspSessionUrl, int serverCodecModeSupport,
    int width, int height, int fps, int bitrate,
    const unsigned char *rikey, int rikeyid, int pipeFd
) {
    (void)pipeFd;
    g_amc_w = width; g_amc_h = height;

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
    amc_teardown();
}

static void do_send_key(short vkCode, char action, char modifiers) {
    LiSendKeyboardEvent(vkCode, action, modifiers);
}
static void do_send_mouse_move(short dx, short dy)        { LiSendMouseMoveEvent(dx, dy); }
static void do_send_mouse_position(short x, short y, short rW, short rH) {
    LiSendMousePositionEvent(x, y, rW, rH);
}
static void do_send_mouse_button(char action, int button) { LiSendMouseButtonEvent(action, button); }
static void do_send_scroll(signed char clicks)            { LiSendScrollEvent(clicks); }
static void do_send_multi_controller(unsigned short cn, unsigned short am, unsigned short b,
    unsigned char lt, unsigned char rt, short lx, short ly, short rx, short ry)
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
	rtspSessionUrl string, rikey []byte,
	appVersion, gfeVersion string, serverCodecModeSupport int,
	width, height, fps, bitrate int,
	pipeWrite *os.File, audioPipeWrite *os.File, onStop func(error),
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

		logrus.Infof("🌕 [Moonlight/CGO/Android] LiStartConnection: host=%s %dx%d@%d", w.host, width, height, fps)

		ret := C.do_li_start(
			host, appVer, gfeVer, rtsp,
			C.int(serverCodecModeSupport),
			C.int(width), C.int(height), C.int(fps), C.int(bitrate),
			cRikey, C.int(1), C.int(-1),
		)
		if int(ret) != 0 {
			logrus.Errorf("🌕 [Moonlight/CGO/Android] LiStartConnection FAILED: %d", int(ret))
			if onStop != nil {
				onStop(fmt.Errorf("LiStartConnection error %d", int(ret)))
			}
			return
		}

		logrus.Info("🌕 [Moonlight/CGO/Android] ✅ streams active")
		liStartConnectionActive.Store(true)
		<-activeStreamDone

		C.do_li_stop()
		vtFrameCallbackMu.Lock()
		vtFrameCallback = nil
		vtFrameCallbackMu.Unlock()
		liStartConnectionActive.Store(false)
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
	cn uint16, am uint16, b uint16, lt uint8, rt uint8, lx int16, ly int16, rx int16, ry int16,
) {
	if !liStartConnectionActive.Load() {
		return
	}
	C.do_send_multi_controller(C.ushort(cn), C.ushort(am), C.ushort(b),
		C.uchar(lt), C.uchar(rt), C.short(lx), C.short(ly), C.short(rx), C.short(ry))
}
func (w *MoonlightCgoWrapper) IsInputActive() bool { return liStartConnectionActive.Load() }

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
	logrus.Errorf("🌕 [Moonlight] ❌ terminated: code=%d", int(errCode))
	activeStreamTermErr = fmt.Errorf("stream terminated: code=%d", int(errCode))
	closeActiveStreamDone()
}

//export goVTLog
func goVTLog(msg *C.char) { logrus.Infof("🎬 [Moonlight/HW/Android] %s", C.GoString(msg)) }

var vtFrameCount int64

//export goVTFrame
func goVTFrame(rgba *C.uint8_t, width, height, stride C.int) {
	vtFrameCallbackMu.Lock()
	cb := vtFrameCallback
	vtFrameCallbackMu.Unlock()
	if cb == nil {
		return
	}
	w, h, s := int(width), int(height), int(stride)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rowBytes := w * 4
	if s == rowBytes {
		copy(img.Pix, (*[1 << 30]byte)(unsafe.Pointer(rgba))[:w*h*4:w*h*4])
	} else {
		src := (*[1 << 30]byte)(unsafe.Pointer(rgba))[:h*s : h*s]
		for y := 0; y < h; y++ {
			copy(img.Pix[y*rowBytes:], src[y*s:y*s+rowBytes])
		}
	}
	cnt := atomic.AddInt64(&vtFrameCount, 1)
	if cnt == 1 {
		logrus.Infof("🎬 [Moonlight/HW/Android] ✅ first RGBA frame — %dx%d", w, h)
	}
	cb(img)
}
