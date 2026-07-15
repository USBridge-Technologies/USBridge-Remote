//go:build (darwin || ios || linux) && !android && cgo

package service

/*
// Minimal declarations — definitions live in the platform CGO file (same package).
// The linker resolves them from moonlight_cgo_apple.go or moonlight_cgo_linux.go.
#include <stdint.h>
#include <stdlib.h>

extern int do_li_start(
    const char *address, const char *appVersion, const char *gfeVersion,
    const char *rtspSessionUrl, int serverCodecModeSupport, int videoFormat,
    int width, int height, int fps, int bitrate,
    const unsigned char *rikey, int rikeyid, int pipeFd);
extern void do_li_stop(void);
extern void set_audio_pipe_fd(int fd);
extern void set_audio_muted(int muted);
extern void do_send_key(short vkCode, char action, char modifiers);
extern void do_send_mouse_move(short dx, short dy);
extern void do_send_mouse_position(short x, short y, short refW, short refH);
extern void do_send_mouse_button(char action, int button);
extern void do_send_scroll(signed char clicks);
extern void do_send_multi_controller(
    unsigned short controllerNumber, unsigned short activeGamepadMask,
    unsigned short buttons,
    unsigned char leftTrigger, unsigned char rightTrigger,
    short leftStickX, short leftStickY,
    short rightStickX, short rightStickY);
extern void do_send_utf8_text(const char *text, unsigned int len);
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

// vtFrameCallback receives decoded RGBA frames from the hardware decoder.
// Set by the platform-specific player file before StartStream is called.
var (
	vtFrameCallback   func(image.Image)
	vtFrameCallbackMu sync.Mutex
)

var liStartConnectionActive atomic.Bool

var (
	activeStreamDone    chan struct{}
	activeStreamOnce    sync.Once
	activeStreamTermErr error
)

// liStreamMu serializes LiStopConnection / LiStartConnection so they never
// run concurrently. liStreamGen is a generation counter that lets the goroutine
// detect whether it is still the "current" stream before touching shared state.
var (
	liStreamMu  sync.Mutex
	liStreamGen atomic.Uint64
)

func closeActiveStreamDone() {
	activeStreamOnce.Do(func() { close(activeStreamDone) })
}

// MoonlightCgoWrapper wraps LiStartConnection from moonlight-common-c.
type MoonlightCgoWrapper struct {
	host           string
	pipeWrite      *os.File
	audioPipeWrite *os.File
	audioMuted     bool
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
	w.pipeWrite = pipeWrite
	w.audioPipeWrite = audioPipeWrite

	// Hold the stream mutex while stopping any previous connection and resetting
	// state.  This blocks until any in-progress LiStopConnection (from a prior
	// goroutine or from StopStream) has fully returned, preventing concurrent
	// LiStartConnection + LiStopConnection which corrupts moonlight-common-c
	// static state and causes SIGSEGV.
	liStreamMu.Lock()
	C.do_li_stop()
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

	pipeFd := C.int(-1)
	if pipeWrite != nil {
		pipeFd = C.int(pipeWrite.Fd())
	}

	go func() {
		defer C.free(unsafe.Pointer(host))
		defer C.free(unsafe.Pointer(appVer))
		defer C.free(unsafe.Pointer(gfeVer))
		defer C.free(unsafe.Pointer(rtsp))
		if cRikey != nil {
			defer C.free(unsafe.Pointer(cRikey))
		}

		logrus.Infof("🌕 [Moonlight/CGO] LiStartConnection: host=%s %dx%d@%d bitrate=%d",
			w.host, width, height, fps, bitrate)

		if audioPipeWrite != nil {
			C.set_audio_pipe_fd(C.int(audioPipeWrite.Fd()))
		}

		ret := C.do_li_start(
			host, appVer, gfeVer, rtsp,
			C.int(serverCodecModeSupport), C.int(videoFormat),
			C.int(width), C.int(height), C.int(fps), C.int(bitrate),
			cRikey, C.int(1),
			pipeFd,
		)

		if int(ret) != 0 {
			logrus.Errorf("🌕 [Moonlight/CGO] LiStartConnection FAILED: code=%d", int(ret))
			C.set_audio_pipe_fd(-1)
			if pipeWrite != nil {
				_ = pipeWrite.Close()
			}
			if audioPipeWrite != nil {
				_ = audioPipeWrite.Close()
			}
			if onStop != nil && liStreamGen.Load() == myGen {
				onStop(fmt.Errorf("LiStartConnection error code %d", int(ret)))
			}
			return
		}

		logrus.Info("🌕 [Moonlight/CGO] ✅ LiStartConnection setup done — streams active")
		if liStreamGen.Load() == myGen {
			liStartConnectionActive.Store(true)
		}

		<-activeStreamDone

		logrus.Info("🌕 [Moonlight/CGO] termination received — stopping streams")
		// Call LiStopConnection under the mutex so that the next StartStream
		// cannot call LiStartConnection until this stop is fully complete.
		liStreamMu.Lock()
		C.do_li_stop()
		liStreamMu.Unlock()

		C.set_audio_pipe_fd(-1)

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
		if audioPipeWrite != nil {
			_ = audioPipeWrite.Close()
		}

		if onStop != nil && liStreamGen.Load() == myGen {
			onStop(activeStreamTermErr)
		}
	}()

	return nil
}

func (w *MoonlightCgoWrapper) StopStream() {
	logrus.Info("🌕 [Moonlight/CGO] StopStream: stopping")
	liStreamMu.Lock()
	C.do_li_stop()
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

// ── Input methods ─────────────────────────────────────────────────────────────

func (w *MoonlightCgoWrapper) SendMoonlightKey(vkCode int16, action int8, modifiers int8) {
	if !liStartConnectionActive.Load() {
		logrus.Warnf("🌕 [Moonlight/CGO] SendMoonlightKey failed: liStartConnectionActive is false")
		return
	}
	logrus.Infof("🌕 [Moonlight/CGO] SendMoonlightKey vkCode=0x%04X action=%d modifiers=%d", uint16(vkCode), action, modifiers)
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

func (w *MoonlightCgoWrapper) IsInputActive() bool {
	return liStartConnectionActive.Load()
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
	logrus.Errorf("🌕 [Moonlight] ❌ terminated: code=%d (%s)", int(errCode), reason)
	activeStreamTermErr = fmt.Errorf("stream terminated: code=%d (%s)", int(errCode), reason)
	closeActiveStreamDone()
}

//export goVTLog
func goVTLog(msg *C.char) {
	logrus.Infof("🎬 [Moonlight/HW] %s", C.GoString(msg))
}

var vtFrameCount int64

//export goVTFrame
func goVTFrame(rgba *C.uint8_t, width, height, stride C.int) {
	vtFrameCallbackMu.Lock()
	cb := vtFrameCallback
	vtFrameCallbackMu.Unlock()
	if cb == nil {
		return
	}

	cnt := atomic.AddInt64(&vtFrameCount, 1)
	if cnt == 1 {
		logrus.Infof("🎬 [Moonlight/HW] ✅ first RGBA frame — %dx%d", int(width), int(height))
	}

	// When the native GPU overlay (Metal/GL) is active it already received this
	// frame at the C level via metal_video_try_submit / gl_video_try_submit.
	// Skip the 3.5 MB Go image allocation most of the time — only the Go-level
	// frame count is needed for stats. However, pass a real frame on the first
	// 10 frames and every 120th frame so that handleVideoFrame can run
	// updateFrameContentRect → detectDarkInset to detect letterbox/pillarbox
	// bars embedded in the video stream (e.g. Sunshine pillarboxing 4:3 content
	// into a 16:9 stream). Without this, frameContentX/Y stays 0 and
	// PositionToAbsolute never adjusts for in-stream black bars.
	if NativeVideoOverlayIsActive() {
		if cnt > 10 && cnt%120 != 0 {
			// Deliver a nil frame to let handleVideoFrame update its own counter.
			cb(nil)
			return
		}
		// Fall through to create a real image for black-bar detection.
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
