//go:build android && cgo

package service

/*
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/build/android/opus/include/opus
#cgo android CFLAGS: -D__ANDROID_UNAVAILABLE_SYMBOLS_ARE_WEAK__
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build/android -lmoonlight-common-c -lenet -lssl -lcrypto
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build/android/opus/lib -lopus
#cgo LDFLAGS: -lmediandk -laaudio -lm -ldl -landroid -lEGL -lGLESv2

#include <media/NdkMediaCodec.h>
#include <media/NdkMediaFormat.h>
#include <aaudio/AAudio.h>
#include <android/log.h>
#include <android/native_window_jni.h>
#include <jni.h>
#include <Limelight.h>
#include <opus_multistream.h>
#include <stdlib.h>
#include <string.h>

#define ALOGI(...) __android_log_print(ANDROID_LOG_INFO,  "Moonlight/CGO", __VA_ARGS__)
#define ALOGE(...) __android_log_print(ANDROID_LOG_ERROR, "Moonlight/CGO", __VA_ARGS__)

extern void goMoonlightStage(int stage, int result, int errCode);
extern void goMoonlightConnected(void);
extern void goMoonlightTerminated(int errCode);
extern void goVTLog(char *msg);
extern void goVTFrame(uint8_t *rgba, int width, int height, int stride);

// Declared in gl_video_impl_android.c
extern void         android_gl_set_jvm(JavaVM *jvm, jobject ctx);
extern ANativeWindow *android_gl_init(int width, int height);
extern uint8_t      *android_gl_get_frame(int width, int height);
extern void          android_gl_release(void);

// Declared in vk_video_impl_android.c
extern void android_vk_set_jvm(JavaVM *jvm, jobject ctx);
extern int  android_vk_is_active(void);
extern int  android_vk_try_submit(uint8_t *rgba, int width, int height, int stride);

// ── Shared state ──────────────────────────────────────────────────────────────

#include <stdatomic.h>
// g_li_active uses a proper C11 atomic to ensure cross-thread visibility
// on ARM64 (plain volatile is insufficient for inter-thread signalling).
static _Atomic int    g_li_active      = 0;
static volatile int   g_audio_muted    = 0;
static AAudioStream  *g_aa_stream      = NULL;
static int            g_audio_channels = 2;
static OpusMSDecoder *g_opus_decoder   = NULL;
static AMediaCodec   *g_amc            = NULL;
static int            g_amc_w          = 0;
static int            g_amc_h          = 0;
static uint64_t       g_amc_pts        = 0;

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
    if (g_aa_stream) return;
    AAudioStreamBuilder *builder = NULL;
    AAudio_createStreamBuilder(&builder);
    AAudioStreamBuilder_setDirection(builder,       AAUDIO_DIRECTION_OUTPUT);
    AAudioStreamBuilder_setSharingMode(builder,     AAUDIO_SHARING_MODE_SHARED);
    AAudioStreamBuilder_setFormat(builder,          AAUDIO_FORMAT_PCM_I16);
    AAudioStreamBuilder_setChannelCount(builder,    channels);
    AAudioStreamBuilder_setSampleRate(builder,      (int32_t)sample_rate);
    AAudioStreamBuilder_setPerformanceMode(builder, AAUDIO_PERFORMANCE_MODE_LOW_LATENCY);
    aaudio_result_t status = AAudioStreamBuilder_openStream(builder, &g_aa_stream);
    AAudioStreamBuilder_delete(builder);
    if (status == AAUDIO_OK) {
        AAudioStream_requestStart(g_aa_stream);
        ALOGI("AAudio stream started: ch=%d rate=%d", channels, sample_rate);
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
    int samples = opus_multistream_decode(g_opus_decoder,
        (const unsigned char *)data, len, pcm, 5760, 0);
    if (samples <= 0) return;
    if (g_audio_muted) memset(pcm, 0, (size_t)(samples * g_audio_channels * 2));
    AAudioStream_write(g_aa_stream, pcm, samples, 10000000LL);
}

// ── Video implementation (AMediaCodec) ──────────────────────────────────────

static void amc_init(int width, int height) {
    ALOGI("amc_init: %dx%d", width, height);
    // Safety: clean up stale codec and EGL if dr_cleanup wasn't called.
    if (g_amc) {
        ALOGE("amc_init: stale g_amc found — cleaning up");
        AMediaCodec_stop(g_amc);
        AMediaCodec_delete(g_amc);
        g_amc = NULL;
        android_gl_release();  // also tear down stale EGL context
    }
    g_amc_w = width; g_amc_h = height;

    ANativeWindow *nwin = android_gl_init(width, height);
    if (!nwin) { ALOGE("amc_init: no native window"); return; }

    AMediaFormat *fmt = AMediaFormat_new();
    AMediaFormat_setString(fmt, "mime", "video/avc");
    AMediaFormat_setInt32(fmt,  "width",  width);
    AMediaFormat_setInt32(fmt,  "height", height);

    g_amc = AMediaCodec_createDecoderByType("video/avc");
    if (g_amc && AMediaCodec_configure(g_amc, fmt, nwin, NULL, 0) == AMEDIA_OK) {
        if (AMediaCodec_start(g_amc) == AMEDIA_OK) {
            ALOGI("AMediaCodec started");
        } else { ALOGE("AMediaCodec_start failed"); }
    } else { ALOGE("AMediaCodec configure failed"); }
    
    AMediaFormat_delete(fmt);
    if (nwin) ANativeWindow_release(nwin);
}

static int dr_setup(int fmt, int w, int h, int rate, void *ctx, int flags) {
    ALOGI("dr_setup: %dx%d@%d", w, h, rate);
    amc_init(w, h);
    return 0;
}

static int dr_submit(PDECODE_UNIT du) {
    if (!g_amc) return DR_OK;
    ssize_t idx = AMediaCodec_dequeueInputBuffer(g_amc, 1000);
    if (idx >= 0) {
        size_t sz = 0;
        uint8_t *buf = AMediaCodec_getInputBuffer(g_amc, idx, &sz);
        if (buf) {
            size_t written = 0;
            for (PLENTRY e = du->bufferList; e && written < sz; e = e->next) {
                size_t n = (e->length < sz - written) ? e->length : sz - written;
                memcpy(buf + written, e->data, n);
                written += n;
            }
            AMediaCodec_queueInputBuffer(g_amc, idx, 0, written, g_amc_pts += 33333, 0);
        }
    }
    AMediaCodecBufferInfo info;
    ssize_t out_idx = AMediaCodec_dequeueOutputBuffer(g_amc, &info, 0);
    if (out_idx >= 0) {
        AMediaCodec_releaseOutputBuffer(g_amc, out_idx, 1);
        uint8_t *rgba = android_gl_get_frame(g_amc_w, g_amc_h);
        if (rgba) {
            if (android_vk_is_active()) {
                // Vulkan overlay: present directly to SurfaceView, skip Fyne canvas.
                android_vk_try_submit(rgba, g_amc_w, g_amc_h, g_amc_w * 4);
            } else {
                goVTFrame(rgba, g_amc_w, g_amc_h, g_amc_w * 4);
            }
        }
    }
    return DR_OK;
}

static void dr_cleanup(void) {
    ALOGI("dr_cleanup");
    if (g_amc) {
        AMediaCodec_stop(g_amc);
        AMediaCodec_delete(g_amc);
        g_amc = NULL;
    }
    android_gl_release();  // always release EGL, even if codec creation failed
}

// ── Start/Stop ────────────────────────────────────────────────────────────────

static void cl_connected(void)    { goMoonlightConnected(); }
static void cl_terminated(int ec) { goMoonlightTerminated(ec); }
static void cl_log(const char *fmt, ...) { (void)fmt; }

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
    cfg.packetSize             = 1200;
    cfg.streamingRemotely      = STREAM_CFG_AUTO;
    cfg.audioConfiguration     = AUDIO_CONFIGURATION_STEREO;
    cfg.supportedVideoFormats  = videoFmt;
    cfg.clientRefreshRateX100  = fps * 100;
    cfg.encryptionFlags        = ENCFLG_NONE;
    if (key) {
        memcpy(cfg.remoteInputAesKey, key, 16);
        cfg.remoteInputAesIv[0] = (char)( kid        & 0xff);
        cfg.remoteInputAesIv[1] = (char)((kid >>  8) & 0xff);
        cfg.remoteInputAesIv[2] = (char)((kid >> 16) & 0xff);
        cfg.remoteInputAesIv[3] = (char)((kid >> 24) & 0xff);
    }

    DECODER_RENDERER_CALLBACKS dr;
    LiInitializeVideoCallbacks(&dr);
    dr.setup            = dr_setup;
    dr.submitDecodeUnit = dr_submit;
    dr.cleanup          = dr_cleanup;
    dr.capabilities     = CAPABILITY_DIRECT_SUBMIT;

    AUDIO_RENDERER_CALLBACKS ar;
    LiInitializeAudioCallbacks(&ar);
    ar.init                = ar_init;
    ar.start               = ar_start;
    ar.stop                = ar_stop;
    ar.cleanup             = ar_cleanup;
    ar.decodeAndPlaySample = ar_decode;

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
void do_li_force_stop(void) {
    int prev = atomic_exchange(&g_li_active, 0);
    ALOGI("do_li_force_stop: was active=%d, calling LiStopConnection", prev);
    LiStopConnection();
    ALOGI("do_li_force_stop: LiStopConnection returned");
}

void set_audio_muted(int muted) { g_audio_muted = muted; }
void set_audio_pipe_fd(int fd)  { (void)fd; }

// ── Input ─────────────────────────────────────────────────────────────────────

void do_send_key(short code, char act, char mod) { LiSendKeyboardEvent(code, act, mod); }
void do_send_mouse_move(short dx, short dy) { LiSendMouseMoveEvent(dx, dy); }
void do_send_mouse_position(short x, short y, short w, short h) { LiSendMousePositionEvent(x, y, w, h); }
void do_send_mouse_button(char act, int btn) { LiSendMouseButtonEvent(act, btn); }
void do_send_scroll(signed char c) { LiSendScrollEvent(c); }
void do_send_multi_controller(unsigned short cn, unsigned short am, unsigned short b, unsigned char lt, unsigned char rt, short lx, short ly, short rx, short ry) {
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
	return &MoonlightCgoWrapper{host: host}
}

func (w *MoonlightCgoWrapper) StartStream(url string, key []byte, appV, gfeV string, codec, videoFormat, width, height, fps, bit int, pw, apw *os.File, onStop func(error)) error {
	// Acquire the mutex and cleanly stop any in-progress connection before
	// resetting state.  We use liConnected (Go-level atomic) as the reliable
	// signal because C's g_li_active may be zero by the time we reach here
	// even though the connection is technically still active.
	liStreamMu.Lock()
	if liConnected.Swap(false) {
		logrus.Info("🌕 [Moonlight/Android] StartStream: stopping previous connection")
		C.do_li_force_stop()
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
		ret := C.do_li_start(cHost, cAppV, cGfeV, cRtsp,
			C.int(codec), C.int(videoFormat), C.int(width), C.int(height), C.int(fps), C.int(bit),
			cKey, C.int(1), C.int(-1))

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
		if liStreamGen.Load() == myGen {
			liStartConnectionActive.Store(true)
		}
		<-activeStreamDone

		logrus.Infof("🌕 [Moonlight/Android] goroutine woke from activeStreamDone — gen=%d current=%d connected=%v",
			myGen, liStreamGen.Load(), liConnected.Load())

		// Whoever wins the liConnected.Swap(false) race calls LiStopConnection.
		// This is reliable regardless of g_li_active state.
		liStreamMu.Lock()
		if liConnected.Swap(false) {
			logrus.Info("🌕 [Moonlight/Android] goroutine calling do_li_force_stop")
			C.do_li_force_stop()
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
	if liConnected.Swap(false) {
		logrus.Info("🌕 [Moonlight/Android] StopStream: calling do_li_force_stop")
		C.do_li_force_stop()
	} else {
		logrus.Info("🌕 [Moonlight/Android] StopStream: not connected (goroutine already stopped or never started)")
	}
	liStreamMu.Unlock()
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

func (w *MoonlightCgoWrapper) SendMoonlightKey(c int16, a, m int8) { if liStartConnectionActive.Load() { C.do_send_key(C.short(c), C.char(a), C.char(m)) } }
func (w *MoonlightCgoWrapper) SendMoonlightMouseMove(dx, dy int16) { if liStartConnectionActive.Load() { C.do_send_mouse_move(C.short(dx), C.short(dy)) } }
func (w *MoonlightCgoWrapper) SendMoonlightMousePosition(x, y, rw, rh int16) { if liStartConnectionActive.Load() { C.do_send_mouse_position(C.short(x), C.short(y), C.short(rw), C.short(rh)) } }
func (w *MoonlightCgoWrapper) SendMoonlightMouseButton(a int8, b int) { if liStartConnectionActive.Load() { C.do_send_mouse_button(C.char(a), C.int(b)) } }
func (w *MoonlightCgoWrapper) SendMoonlightScroll(c int8) { if liStartConnectionActive.Load() { C.do_send_scroll(C.schar(c)) } }
func (w *MoonlightCgoWrapper) SendMoonlightControllerEvent(cn uint16, am uint16, b uint16, lt uint8, rt uint8, lx int16, ly int16, rx int16, ry int16) {
	if liStartConnectionActive.Load() {
		C.do_send_multi_controller(C.ushort(cn), C.ushort(am), C.ushort(b), C.uchar(lt), C.uchar(rt), C.short(lx), C.short(ly), C.short(rx), C.short(ry))
	}
}

//export goMoonlightStage
func goMoonlightStage(s, r, e C.int) {
	stages := []string{"none", "platform-init", "name-resolution", "audio-stream-init", "rtsp-handshake", "control-stream-init", "video-stream-init", "input-stream-init", "control-stream-start", "video-stream-start", "audio-stream-start", "input-stream-start"}
	name := "unknown"; if int(s) < len(stages) { name = stages[s] }
	if r == 0 { logrus.Infof("🌕 [Moonlight] ► %s …", name) } else if r == 1 { logrus.Infof("🌕 [Moonlight] ✅ %s", name) } else { logrus.Errorf("🌕 [Moonlight] ❌ %s failed (%d)", name, int(e)) }
}
//export goMoonlightConnected
func goMoonlightConnected() { logrus.Info("🌕 [Moonlight] connected ✅") }
//export goMoonlightTerminated
func goMoonlightTerminated(e C.int) {
	logrus.Errorf("🌕 [Moonlight/Android] ❌ terminated: code=%d active=%v", int(e), liStartConnectionActive.Load())
	activeStreamTermErr = fmt.Errorf("terminated %d", int(e))
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
	vtFrameCallbackMu.Lock(); cb := vtFrameCallback; vtFrameCallbackMu.Unlock()
	if cb == nil { return }
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
func SetVTFrameCallback(cb func(image.Image)) { vtFrameCallbackMu.Lock(); vtFrameCallback = cb; vtFrameCallbackMu.Unlock() }
