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
extern void do_li_interrupt(void);
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
extern void do_send_pen(unsigned char eventType, unsigned char toolType, unsigned char penButtons,
                        float x, float y, float pressureOrDistance,
                        unsigned short rotation, unsigned char tilt);
extern void do_get_rtp_video_stats(uint32_t *out);
extern int do_get_estimated_rtt_info(uint32_t *out);
extern uint16_t do_get_last_host_latency_tenths_ms(void);
extern uint64_t do_get_playout_jitter_us(void);
extern uint64_t do_get_playout_applied_delay_us(void);
*/
import "C"

import (
	"fmt"
	"net"

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

// RTPVideoStats mirrors Limelight.h's RTP_VIDEO_STATS. FecFailed > 0 means a
// frame's FEC redundancy wasn't enough to recover a lost packet -- the
// depacketizer discards that decode unit and (per moonlight-common-c's
// RtpVideoQueue) whatever reference-frame chain it belonged to, which is
// exactly the failure mode that shows up as a corrupted P-frame on screen
// until the next IDR.
type RTPVideoStats struct {
	PacketCountVideo        uint32
	PacketCountFec          uint32
	PacketCountFecRecovered uint32
	PacketCountFecFailed    uint32
	PacketCountOOS          uint32
	PacketCountInvalid      uint32
	PacketCountFecInvalid   uint32
}

// GetRTPVideoStats reads moonlight-common-c's running RTP video counters.
// Safe to call at any time (even with no active session -- moonlight-common-c
// zero-initializes these statics), but only meaningful once a stream is up.
func GetRTPVideoStats() RTPVideoStats {
	var raw [7]C.uint32_t
	C.do_get_rtp_video_stats(&raw[0])
	return RTPVideoStats{
		PacketCountVideo:        uint32(raw[0]),
		PacketCountFec:          uint32(raw[1]),
		PacketCountFecRecovered: uint32(raw[2]),
		PacketCountFecFailed:    uint32(raw[3]),
		PacketCountOOS:          uint32(raw[4]),
		PacketCountInvalid:      uint32(raw[5]),
		PacketCountFecInvalid:   uint32(raw[6]),
	}
}

// GetEstimatedRttInfo reads moonlight-common-c's smoothed RTT estimate
// (LiGetEstimatedRttInfo). ok is false when there's no active session or no
// estimate yet -- both fields are then meaningless, not just zero.
func GetEstimatedRttInfo() (rttMs, rttVarianceMs float64, ok bool) {
	var raw [2]C.uint32_t
	got := C.do_get_estimated_rtt_info(&raw[0])
	if got == 0 {
		return 0, 0, false
	}
	return float64(raw[0]), float64(raw[1]), true
}

// GetLastHostLatencyMs reads the most recent frame's host processing
// latency, as reported by the server in its standard Sunshine-protocol
// frame header (DECODE_UNIT.frameHostProcessingLatency -- see
// moonlight_cgo_shared.h's dr_submit/do_get_last_host_latency_tenths_ms).
// Limelight.h documents exactly 0 as "the host doesn't provide the latency
// data", but in practice a real 0 is indistinguishable from that: the host
// also reports (or simply stops updating) 0 whenever a frame's picture
// didn't change and nothing was actually encoded, which is a normal,
// frequent condition, not a rare "unsupported" edge case -- so 0 is treated
// as a genuine measurement here (valid is always true) rather than hidden.
func GetLastHostLatencyMs() (ms float64, valid bool) {
	tenths := uint16(C.do_get_last_host_latency_tenths_ms())
	return float64(tenths) / 10.0, true
}

// GetPlayoutJitterMs reads the client-side adaptive playout buffer's live
// jitter estimate (LiGetPlayoutJitterUs) -- arrival-time variance measured
// locally from received frames' RTP timestamps, distinct from
// GetEstimatedRttInfo's network-level RTT variance. 0 before the first
// jitter sample exists.
func GetPlayoutJitterMs() float64 {
	return float64(uint64(C.do_get_playout_jitter_us())) / 1000.0
}

// GetPlayoutAppliedDelayMs reads the playout buffer's currently-applied
// extra delay (LiGetPlayoutAppliedDelayUs) -- how much it's actually
// stretching frame release right now to absorb GetPlayoutJitterMs's
// measured jitter.
func GetPlayoutAppliedDelayMs() float64 {
	return float64(uint64(C.do_get_playout_applied_delay_us())) / 1000.0
}

// init wires net_graph.go's platform-agnostic network-stats hook to the
// getters above -- same "core stays tag-free, platform files wire the
// hooks" split as metal_video_darwin.go's own init() for the render/decode/
// push hooks. This file's build tag (darwin/ios/linux, not windows/android)
// means net_graph.go simply reads zero-value stats on the platforms that
// don't have this wired yet (see net_graph.go's package doc comment).
func init() {
	netGraphNetworkStatsFn = func() netGraphRawNetworkStats {
		rtp := GetRTPVideoStats()
		rttMs, rttVarianceMs, rttOk := GetEstimatedRttInfo()
		hostLatencyMs, hostLatencyOk := GetLastHostLatencyMs()
		return netGraphRawNetworkStats{
			PacketCountVideo:        rtp.PacketCountVideo,
			PacketCountFec:          rtp.PacketCountFec,
			PacketCountFecRecovered: rtp.PacketCountFecRecovered,
			PacketCountFecFailed:    rtp.PacketCountFecFailed,
			PacketCountOOS:          rtp.PacketCountOOS,
			PacketCountInvalid:      rtp.PacketCountInvalid,
			RTTMs:                   rttMs,
			RTTVarianceMs:           rttVarianceMs,
			RTTValid:                rttOk,
			HostLatencyMs:           hostLatencyMs,
			HostLatencyValid:        hostLatencyOk,
			JitterMs:                GetPlayoutJitterMs(),
			PlayoutDelayMs:          GetPlayoutAppliedDelayMs(),
		}
	}
}

// startRTPStatsLoggerIfEnabled logs GetRTPVideoStats() periodically for the
// lifetime of `done` when USBRIDGE_LOG_RTP_STATS is set -- opt-in so it never
// runs in normal client builds.
func startRTPStatsLoggerIfEnabled(done <-chan struct{}) {
	if os.Getenv("USBRIDGE_LOG_RTP_STATS") == "" {
		return
	}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var prev RTPVideoStats
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				s := GetRTPVideoStats()
				logrus.Infof("🌕 [Moonlight/RTPStats] video=%d fec=%d recovered=%d failed=%d(+%d) oos=%d invalid=%d fecInvalid=%d",
					s.PacketCountVideo, s.PacketCountFec, s.PacketCountFecRecovered,
					s.PacketCountFecFailed, s.PacketCountFecFailed-prev.PacketCountFecFailed,
					s.PacketCountOOS, s.PacketCountInvalid, s.PacketCountFecInvalid)
				prev = s
			}
		}
	}()
}

// vtFrameCallback receives decoded RGBA frames from the hardware decoder.
// Set by the platform-specific player file before StartStream is called.
var (
	vtFrameCallback   func(image.Image)
	vtFrameCallbackMu sync.Mutex
)

var liStartConnectionActive atomic.Bool

// negotiatedVideoFormat holds the VIDEO_FORMAT_* value moonlight-common-c
// reported via dr_setup(NegotiatedVideoFormat, ...) — the server's actual
// codec choice for the current session, as opposed to what the client asked
// for. -1 means "no session has reported a negotiated format yet".
var negotiatedVideoFormat atomic.Int32

func init() {
	negotiatedVideoFormat.Store(-1)
}

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
// call at any time. Interrupting first unblocks any in-progress
// LiStartConnection() quickly, then waiting on liStartMu guarantees
// do_li_start has fully returned before do_li_stop() touches
// moonlight-common-c's shared static state.
func stopConnectionSafely() {
	C.do_li_interrupt()
	liStartMu.Lock()
	defer liStartMu.Unlock()
	C.do_li_stop()
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

		liStartMu.Lock()
		// If another StartStream or StopStream occurred while we waited for
		// the lock, abort this stale attempt.
		if liStreamGen.Load() != myGen {
			liStartMu.Unlock()
			logrus.Info("🌕 [Moonlight/CGO] Aborting stale stream start")
			return
		}

		ret := C.do_li_start(
			host, appVer, gfeVer, rtsp,
			C.int(serverCodecModeSupport), C.int(videoFormat),
			C.int(width), C.int(height), C.int(fps), C.int(bitrate),
			cRikey, C.int(1),
			pipeFd,
		)
		liStartMu.Unlock()

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
		// Unconditionally true -- see the Android cgo file's identical fix
		// for why gating this the same way as the generation-checked reset
		// below caused a real bug: under reconnect races this branch could
		// run for a stale generation and skip the store, leaving
		// IsInputActive() stuck false (and every mouse/keyboard send, all
		// gated on it, silently dropped) even though video/audio kept
		// streaming fine. This goroutine's own do_li_start really did just
		// succeed, so the store is always correct and idempotent here.
		liStartConnectionActive.Store(true)
		startRTPStatsLoggerIfEnabled(activeStreamDone)

		<-activeStreamDone

		logrus.Info("🌕 [Moonlight/CGO] termination received — stopping streams")
		// Call LiStopConnection under the mutex so that the next StartStream
		// cannot call LiStartConnection until this stop is fully complete.
		liStreamMu.Lock()
		stopConnectionSafely()
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

// ── Input methods ─────────────────────────────────────────────────────────────

func (w *MoonlightCgoWrapper) SendMoonlightKey(vkCode int16, action int8, modifiers int8) {
	if !liStartConnectionActive.Load() {
		logrus.Warnf("🌕 [Moonlight/CGO] SendMoonlightKey failed: liStartConnectionActive is false")
		return
	}
	logrus.Debugf("🌕 [Moonlight/CGO] SendMoonlightKey vkCode=0x%04X action=%d modifiers=%d", uint16(vkCode), action, modifiers)
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
	// Clear the negotiated codec so a stale value from this session can't be
	// shown as "currently active" once the stream has actually ended.
	negotiatedVideoFormat.Store(-1)
	closeActiveStreamDone()
}

// videoFormatCodecName maps a VIDEO_FORMAT_* bitmask (Limelight.h) to the
// client's codec name constants ("h264"/"h265"/"av1"). Mirrors the bit
// layout moonlightVideoFormat() in moonlight_service.go encodes, plus the
// mask bits the platform CGO files already use to branch HEVC vs H264
// (VIDEO_FORMAT_MASK_H265 = 0x0F00, VIDEO_FORMAT_MASK_AV1 = 0xF000).
func videoFormatCodecName(format int32) (string, bool) {
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

//export goVideoFormatNegotiated
func goVideoFormatNegotiated(format C.int) {
	negotiatedVideoFormat.Store(int32(format))
	name, ok := videoFormatCodecName(int32(format))
	if !ok {
		logrus.Warnf("🎬 [Moonlight/HW] negotiated video format: unrecognized 0x%04X", int(format))
		logrus.Warnf("🎯 [CODEC-TRACE] dr_setup: server negotiated an UNRECOGNIZED format 0x%04X", int(format))
		return
	}
	logrus.Infof("🎬 [Moonlight/HW] negotiated video format: %s (0x%04X)", name, int(format))
	logrus.Infof("🎯 [CODEC-TRACE] dr_setup: SERVER ACTUALLY NEGOTIATED codec=%s (0x%04X) -- this is the ground truth for what's really streaming, compare against the videoMode logged before Launch()/StartStream above", name, int(format))
}

// NegotiatedVideoCodecName returns the codec moonlight-common-c actually
// negotiated with the server for the current session (from dr_setup's
// NegotiatedVideoFormat), and whether a session has reported one yet. This
// is the authoritative answer to "what codec is really streaming" — unlike
// the client's requested mode or the agent's best-effort guess, it reflects
// what the server actually accepted.
func (w *MoonlightCgoWrapper) NegotiatedVideoCodecName() (string, bool) {
	if !liStartConnectionActive.Load() {
		return "", false
	}
	return videoFormatCodecName(negotiatedVideoFormat.Load())
}

// vtLogThrottleWindow bounds how often an *identical, consecutive* Limelog
// message actually reaches logrus -- see goVTLog's own doc comment for why
// this exists at all. 250ms is short enough that a real burst still shows
// up promptly (the first occurrence of any new message logs immediately,
// unthrottled) and long enough to collapse the kind of tight loop a real
// loss storm produces (confirmed live: "Waiting for IDR frame" logged over
// 150 times inside a ~2s stretch) down to about one line every 250ms
// instead of one per occurrence.
const vtLogThrottleWindow = 250 * time.Millisecond

var (
	vtLogMu      sync.Mutex
	vtLogLastMsg string
	vtLogRepeat  int
	vtLogWinFrom time.Time
)

// goVTLog is moonlight-common-c's single Limelog() entry point on every
// platform (called via ListenerCallbacks.logMessage, Platform.h's Limelog
// macro) -- every one of the "Waiting for IDR frame"/"Waiting for RFI
// frame"/"Invalidate reference frame request sent" lines seen during a real
// loss burst comes through here, often dozens to hundreds of times within a
// second or two while the client keeps retrying against the same ongoing
// loss. Each call was previously a synchronous logrus.Infof unconditionally
// -- a real syscall-backed write (see cmd/setupLogging's async writer for
// the other half of this: even with that in place, the CGO call boundary
// and string formatting per invocation is waste that serves no one once
// the message is a byte-for-byte repeat of the one immediately before it.
//
// Collapses a run of identical, back-to-back messages into a single
// logged line once the run ends (a different message arrives) or every
// vtLogThrottleWindow while it's still ongoing, tagged "(repeated xN)" --
// the *first* occurrence of any message always logs immediately, so a
// genuinely new/rare event is never delayed or hidden by this.
//
//export goVTLog
func goVTLog(msg *C.char) {
	s := C.GoString(msg)
	now := time.Now()

	vtLogMu.Lock()
	if s == vtLogLastMsg && now.Sub(vtLogWinFrom) < vtLogThrottleWindow {
		vtLogRepeat++
		vtLogMu.Unlock()
		return
	}
	prevMsg, prevRepeat := vtLogLastMsg, vtLogRepeat
	vtLogLastMsg = s
	vtLogRepeat = 1
	vtLogWinFrom = now
	vtLogMu.Unlock()

	if prevRepeat > 1 {
		logrus.Infof("🎬 [Moonlight/HW] %s (repeated x%d)", prevMsg, prevRepeat)
	}
	logrus.Infof("🎬 [Moonlight/HW] %s", s)
}

// goAIVisionOverlay is the cgo entry point for the AI Vision live overlay
// (see ai_vision.go): called from deliver_frame in moonlight_cgo_linux.go
// and win_deliver_frame in moonlight_cgo_windows.go (both before the frame
// reaches vk_video_try_submit/gl_video_try_submit) and from the CPU-fallback
// decode path in moonlight_cgo_apple.go, on the exact RGBA buffer that's
// about to be displayed. wrapRGBA below is a zero-copy view over the
// C-owned memory -- ApplyAIVisionOverlay draws into it in place -- valid
// only for the duration of this call, which matches how long the C side
// guarantees the buffer stays alive.
//
// Windows' Vulkan path is NOT a genuine zero-copy GPU-texture handoff like
// Android/iOS's AHardwareBuffer path below: win_deliver_frame already runs
// every decoded frame through sws_scale into a CPU-side RGBA buffer before
// vk_video_try_submit even sees it (that's also what feeds goVTFrame's
// VideoTrace stats), so there's a real CPU-readable buffer to overlay into
// on every frame, same as Linux -- just gated to the RGBA (not GDI/BGRA
// fallback) dst_fmt case, see win_deliver_frame's own comment.
//
// Not wired into the CVImageBufferRef fast path metal_video_try_submit
// takes on macOS when it succeeds (see goAIVisionShouldSample/goAIVisionSample
// below for that path instead), nor into the true AHardwareBuffer path on
// Android/iOS: those hand decoded frames to the GPU without ever producing
// a CPU-readable buffer on every frame, so overlaying them needs an actual
// native compositing layer (macOS: metal_video_impl_darwin.m's
// g_overlay_layer; Android's cursor uses the same pattern, see
// VulkanOverlayBridge.kt) rather than pixel writes here.
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

// goNetGraphOverlay is the cgo entry point for the Net Graph HUD's
// CPU-buffer blit path (net_graph.go's ApplyNetGraphOverlay) -- called from
// moonlight_cgo_linux.go's deliver_frame, right next to goAIVisionOverlay's
// call site above. Not called from moonlight_cgo_apple.go: macOS/iOS use a
// native compositor HUD layer instead (metal_video_impl_darwin.m's
// g_hud_layer / metal_video_impl_ios.m's mirror of it) since their zero-copy
// decode path never produces a CPU-writable buffer -- see net_graph.go's
// ApplyNetGraphOverlay doc comment. Harmless no-op if ever reached on those
// platforms (ApplyNetGraphOverlay's own atomic check).
//
//export goNetGraphOverlay
func goNetGraphOverlay(rgba *C.uint8_t, width, height, stride C.int) {
	if rgba == nil || width <= 0 || height <= 0 || stride <= 0 {
		return
	}
	w, h, s := int(width), int(height), int(stride)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(rgba)), s*h)
	ApplyNetGraphOverlay(buf, w, h, s, false) // Linux's deliver_frame always converts to RGBA
}

// goAIVisionShouldSample is a cheap (atomics + time comparisons, no pixel
// access) pre-check called every frame from vt_callback's Metal fast-path
// branch in moonlight_cgo_apple.go: it lets the C side skip the BGRA→RGBA
// CVPixelBuffer readback entirely on the (overwhelming) majority of frames
// where neither the icon nor the OCR loop is due yet (see ai_vision.go's
// package doc comment for why there are two independent loops/cadences),
// so the zero-copy path stays zero-copy except for the rare frame that
// actually needs to feed one of them. Mirrors (but does not replace) the
// authoritative gating inside maybeKickIconDetection/maybeKickOCR's own
// CompareAndSwap -- a false positive here just means one wasted
// conversion, never a correctness issue.
//
//export goAIVisionShouldSample
func goAIVisionShouldSample() C.int {
	// A pending live-frame request (see internal/api/live_frame.go) needs
	// this frame regardless of the checkbox/pacing below -- it's a
	// one-shot, independent consumer of the same sample.
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

// goAIVisionSample is the macOS Metal fast-path counterpart to
// goAIVisionOverlay: called only on the rare frame goAIVisionShouldSample
// green-lit, with a CPU readback of that one frame converted to RGBA. It
// only feeds the detector (maybeKickIconDetection/maybeKickOCR) -- it must
// NOT draw into buf, unlike goAIVisionOverlay's ApplyAIVisionOverlay,
// because this buffer is a throwaway conversion scratch space, never the
// one actually displayed (Metal renders the CVImageBufferRef's IOSurface
// directly). The completed result reaches the screen via
// pushAIVisionOverlayToMetal's separate compositor-layer path instead (see
// aiVisionMetalPush).
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
		logrus.Infof("🎬 [Moonlight/HW] ✅ first RGBA frame — %dx%d", int(width), int(height))
	}

	// rgba is NULL on any call that exists purely to keep Go-side frame
	// counting/FPS/lastFrameTime stats alive while a native overlay (Metal on
	// macOS/iOS, Vulkan's AHardwareBuffer path on Android) already rendered
	// this frame at the C level without ever producing a CPU-readable buffer
	// -- there is no pixel data here to decode, checked first and
	// unconditionally so it can never fall through to the array-pointer
	// conversion below regardless of the frame-count logic (confirmed live:
	// macOS's Metal path always passes NULL, including on frames 1-10 and
	// every 120th, and it used to crash here with a nil-pointer SIGSEGV
	// before this check existed, because that logic assumed a non-nil
	// pointer always accompanied those specific counts).
	if rgba == nil {
		cb(nil)
		return
	}

	// Overlay already presented this frame at C level. In-stream letterbox is
	// cropped from host vs stream aspect, not from a dark-pixel scan of a CPU
	// copy, so there is no reason to allocate a Go image here.
	if NativeVideoOverlayIsActive() {
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

func (ms *MoonlightService) OpenDataChannel(label string) (net.Conn, error) {
	return nil, fmt.Errorf("DataChannel not supported on MoonlightService")
}
