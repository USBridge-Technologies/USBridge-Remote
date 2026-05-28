//go:build windows && cgo

package service

/*
#cgo pkg-config: opus openssl
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build -L${SRCDIR}/../../moonlight-common-c/build/enet -lmoonlight-common-c -lenet -lws2_32 -lwinmm

#include <Limelight.h>
#include <stdlib.h>
#include <string.h>
#include <windows.h>
#include <stdint.h>

// Pipe HANDLE that submitDecodeUnit writes H.264 Annex-B frames into.
// Uses Windows HANDLE instead of POSIX fd. INVALID_HANDLE_VALUE means no active stream.
static HANDLE g_video_pipe_handle = INVALID_HANDLE_VALUE;

// ── Connection-listener callbacks ─────────────────────────────────────────

extern void goMoonlightStage(int stage, int result, int errCode);
extern void goMoonlightConnected(void);
extern void goMoonlightTerminated(int errCode);

static void cl_stage_starting(int s)        { goMoonlightStage(s,  0, 0); }
static void cl_stage_complete(int s)         { goMoonlightStage(s,  1, 0); }
static void cl_stage_failed(int s, int ec)   { goMoonlightStage(s, -1, ec); }
static void cl_connected(void)               { goMoonlightConnected(); }
static void cl_terminated(int ec)            { goMoonlightTerminated(ec); }
static void cl_log(const char* fmt, ...)     {}

// ── Video-decoder callbacks ────────────────────────────────────────────────

static int  dr_setup(int fmt, int w, int h, int rate, void* ctx, int flags) { return 0; }
static void dr_start(void)   {}
static void dr_stop(void)    {}
static void dr_cleanup(void) {}

// submitDecodeUnit writes each buffer to the pipe using Windows WriteFile.
// Called from the LiStartConnection video-receive thread — no Go runtime here.
static int dr_submit(PDECODE_UNIT du) {
    HANDLE h = g_video_pipe_handle;
    if (h == INVALID_HANDLE_VALUE) return DR_NEED_IDR;
    for (PLENTRY e = du->bufferList; e; e = e->next) {
        const char* ptr = e->data;
        DWORD remaining = (DWORD)e->length;
        while (remaining > 0) {
            DWORD written = 0;
            if (!WriteFile(h, ptr, remaining, &written, NULL) || written == 0) {
                return DR_NEED_IDR;
            }
            ptr += written;
            remaining -= written;
        }
    }
    return DR_OK;
}

// ── Audio callbacks (muted) ────────────────────────────────────────────────

static int  ar_init(int ac, const POPUS_MULTISTREAM_CONFIGURATION cfg, void* ctx, int flags) { return 0; }
static void ar_start(void)                     {}
static void ar_stop(void)                      {}
static void ar_cleanup(void)                   {}
static void ar_decode(char* data, int len)     {}

// ── LiStartConnection entrypoint ───────────────────────────────────────────

static int do_li_start(
    const char* address,
    const char* appVersion,
    const char* gfeVersion,
    const char* rtspSessionUrl,
    int serverCodecModeSupport,
    int width, int height, int fps, int bitrate,
    const unsigned char* rikey,
    int rikeyid,
    uintptr_t pipeHandleULL   // Windows HANDLE passed as uintptr_t to avoid CGO type issues
) {
    g_video_pipe_handle = (HANDLE)pipeHandleULL;

    SERVER_INFORMATION srv;
    LiInitializeServerInformation(&srv);
    srv.address              = address;
    srv.serverInfoAppVersion = appVersion;
    srv.serverInfoGfeVersion = gfeVersion;
    srv.rtspSessionUrl       = rtspSessionUrl;
    srv.serverCodecModeSupport = serverCodecModeSupport;

    STREAM_CONFIGURATION cfg;
    LiInitializeStreamConfiguration(&cfg);
    cfg.width                  = width;
    cfg.height                 = height;
    cfg.fps                    = fps;
    cfg.bitrate                = bitrate;
    cfg.packetSize             = 1200;
    cfg.streamingRemotely      = STREAM_CFG_AUTO;
    cfg.audioConfiguration     = AUDIO_CONFIGURATION_STEREO;
    cfg.supportedVideoFormats  = VIDEO_FORMAT_H264;
    cfg.clientRefreshRateX100  = fps * 100;
    cfg.encryptionFlags        = ENCFLG_NONE;
    if (rikey) {
        memcpy(cfg.remoteInputAesKey, rikey, 16);
        cfg.remoteInputAesIv[0] = (char)(rikeyid & 0xff);
        cfg.remoteInputAesIv[1] = (char)((rikeyid >> 8) & 0xff);
        cfg.remoteInputAesIv[2] = (char)((rikeyid >> 16) & 0xff);
        cfg.remoteInputAesIv[3] = (char)((rikeyid >> 24) & 0xff);
    }

    DECODER_RENDERER_CALLBACKS dr;
    LiInitializeVideoCallbacks(&dr);
    dr.setup            = dr_setup;
    dr.start            = dr_start;
    dr.stop             = dr_stop;
    dr.cleanup          = dr_cleanup;
    dr.submitDecodeUnit = dr_submit;
    dr.capabilities     = CAPABILITY_DIRECT_SUBMIT;

    AUDIO_RENDERER_CALLBACKS ar;
    LiInitializeAudioCallbacks(&ar);
    ar.init               = ar_init;
    ar.start              = ar_start;
    ar.stop               = ar_stop;
    ar.cleanup            = ar_cleanup;
    ar.decodeAndPlaySample = ar_decode;

    CONNECTION_LISTENER_CALLBACKS cl;
    LiInitializeConnectionCallbacks(&cl);
    cl.stageStarting        = cl_stage_starting;
    cl.stageComplete        = cl_stage_complete;
    cl.stageFailed          = cl_stage_failed;
    cl.connectionStarted    = cl_connected;
    cl.connectionTerminated = cl_terminated;
    cl.logMessage           = cl_log;

    int ret = LiStartConnection(&srv, &cfg, &cl, &dr, &ar, NULL, 0, NULL, 0);
    if (ret != 0) {
        g_video_pipe_handle = INVALID_HANDLE_VALUE;
    }
    return ret;
}

static void do_li_stop(void) {
    if (g_video_pipe_handle == INVALID_HANDLE_VALUE) return;
    g_video_pipe_handle = INVALID_HANDLE_VALUE;
    LiStopConnection();
}

// ── Input forwarders ──────────────────────────────────────────────────────────

static void do_send_key(short vkCode, char action, char modifiers) {
    LiSendKeyboardEvent(vkCode, action, modifiers);
}

static void do_send_mouse_move(short dx, short dy) {
    LiSendMouseMoveEvent(dx, dy);
}

static void do_send_mouse_button(char action, int button) {
    LiSendMouseButtonEvent(action, button);
}

static void do_send_scroll(signed char clicks) {
    LiSendScrollEvent(clicks);
}
*/
import "C"

import (
	"fmt"
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

func closeActiveStreamDone() {
	activeStreamOnce.Do(func() {
		close(activeStreamDone)
	})
}

type MoonlightCgoWrapper struct {
	host       string
	pipeWrite  *os.File
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
	w.pipeWrite = pipeWrite

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

	// On Windows, Fd() returns the Windows HANDLE as uintptr.
	pipeHandle := C.uintptr_t(pipeWrite.Fd())

	go func() {
		defer C.free(unsafe.Pointer(host))
		defer C.free(unsafe.Pointer(appVer))
		defer C.free(unsafe.Pointer(gfeVer))
		defer C.free(unsafe.Pointer(rtsp))
		if cRikey != nil {
			defer C.free(unsafe.Pointer(cRikey))
		}

		logrus.Infof("🌕 [Moonlight/CGO] LiStartConnection starting: host=%s rtsp=%s %dx%d@%d bitrate=%d",
			w.host, "rtsp://"+rtspSessionUrl, width, height, fps, bitrate)

		ret := C.do_li_start(
			host, appVer, gfeVer, rtsp,
			C.int(serverCodecModeSupport),
			C.int(width), C.int(height), C.int(fps), C.int(bitrate),
			cRikey, C.int(1),
			pipeHandle,
		)

		if int(ret) != 0 {
			logrus.Errorf("🌕 [Moonlight/CGO] LiStartConnection setup FAILED: code=%d", int(ret))
			_ = pipeWrite.Close()
			if onStop != nil {
				onStop(fmt.Errorf("LiStartConnection error code %d", int(ret)))
			}
			return
		}

		logrus.Info("🌕 [Moonlight/CGO] ✅ LiStartConnection setup done — streams active, waiting for termination")
		liStartConnectionActive.Store(true)

		<-activeStreamDone

		logrus.Info("🌕 [Moonlight/CGO] termination received — stopping streams and closing pipe")
		C.do_li_stop()

		liStartConnectionActive.Store(false)
		_ = pipeWrite.Close()

		if onStop != nil {
			onStop(activeStreamTermErr)
		}
	}()

	return nil
}

func (w *MoonlightCgoWrapper) StopStream() {
	if !liStartConnectionActive.Load() {
		logrus.Info("🌕 [Moonlight/CGO] StopStream: no active stream — skipping")
		return
	}
	logrus.Info("🌕 [Moonlight/CGO] StopStream: stopping active stream")
	C.do_li_stop()
	closeActiveStreamDone()
}

func (w *MoonlightCgoWrapper) SetAudioMuted(muted bool) { w.audioMuted = muted }
func (w *MoonlightCgoWrapper) GetAudioMuted() bool      { return w.audioMuted }

// ── Input methods ─────────────────────────────────────────────────────────────

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

// ── CGO-exported Go callbacks ──────────────────────────────────────────────

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
	logrus.Info("🌕 [Moonlight] stream connected ✅ — frames should start flowing")
}

//export goMoonlightTerminated
func goMoonlightTerminated(errCode C.int) {
	reason := "unknown"
	switch int(errCode) {
	case 0:
		reason = "clean disconnect"
	case -100:
		reason = "connection reset by server"
	case -101:
		reason = "server closed connection"
	case -102:
		reason = "no IDR frame received"
	case -200:
		reason = "video decode failed"
	case -300:
		reason = "control stream error"
	case -400:
		reason = "input stream error"
	}
	logrus.Errorf("🌕 [Moonlight] ❌ stream terminated by server: code=%d (%s)", int(errCode), reason)
	activeStreamTermErr = fmt.Errorf("stream terminated by server: code=%d (%s)", int(errCode), reason)
	closeActiveStreamDone()
}
