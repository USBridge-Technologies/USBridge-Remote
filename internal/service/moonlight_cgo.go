//go:build (darwin || linux) && !android

package service

/*
#cgo pkg-config: opus openssl
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build -L${SRCDIR}/../../moonlight-common-c/build/enet -lmoonlight-common-c -lenet
#cgo linux LDFLAGS: -lpthread -lm -ldl
#cgo darwin LDFLAGS: -framework VideoToolbox -framework CoreMedia -framework CoreFoundation -framework CoreVideo

#include <Limelight.h>
#include <opus_multistream.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#ifdef __APPLE__
#include <VideoToolbox/VideoToolbox.h>
#include <CoreMedia/CoreMedia.h>
#include <CoreVideo/CoreVideo.h>
#endif

// ── Shared state ─────────────────────────────────────────────────────────────
//
// g_li_active guards do_li_stop against spurious calls when no stream is running.
// Set to 1 by do_li_start on success; cleared to 0 by do_li_stop before joining threads.
static volatile int g_li_active = 0;

#ifndef __APPLE__
// Linux video path: dr_submit writes Annex-B H.264 frames into this pipe.
// Must be set to -1 before LiStopConnection() so dr_submit sees EOF.
static int g_video_pipe_fd = -1;
#endif

// ── Audio state ───────────────────────────────────────────────────────────────
static int g_audio_pipe_fd = -1;
static volatile int g_audio_muted = 0;
static OpusMSDecoder *g_opus_ms_decoder = NULL;
static int g_audio_channels = 2;
static int g_audio_samples_per_frame = 960;

static void set_audio_pipe_fd(int fd) { g_audio_pipe_fd = fd; }
static void set_audio_muted(int muted) { g_audio_muted = muted; }

// ── Connection-listener callbacks ─────────────────────────────────────────────
extern void goMoonlightStage(int stage, int result, int errCode);
extern void goMoonlightConnected(void);
extern void goMoonlightTerminated(int errCode);

static void cl_stage_starting(int s)      { goMoonlightStage(s,  0, 0); }
static void cl_stage_complete(int s)       { goMoonlightStage(s,  1, 0); }
static void cl_stage_failed(int s, int ec) { goMoonlightStage(s, -1, ec); }
static void cl_connected(void)             { goMoonlightConnected(); }
static void cl_terminated(int ec)          { goMoonlightTerminated(ec); }
static void cl_log(const char *fmt, ...)   {}

// ── Audio callbacks ───────────────────────────────────────────────────────────
static int ar_init(int audioConfig, const POPUS_MULTISTREAM_CONFIGURATION cfg, void *ctx, int flags) {
    g_audio_channels = cfg->channelCount;
    g_audio_samples_per_frame = cfg->samplesPerFrame > 0 ? cfg->samplesPerFrame : 960;
    if (g_opus_ms_decoder) {
        opus_multistream_decoder_destroy(g_opus_ms_decoder);
        g_opus_ms_decoder = NULL;
    }
    int error = OPUS_OK;
    g_opus_ms_decoder = opus_multistream_decoder_create(
        cfg->sampleRate, cfg->channelCount,
        cfg->streams, cfg->coupledStreams,
        cfg->mapping, &error);
    return (error == OPUS_OK) ? 0 : -1;
}

static void ar_start(void)   {}
static void ar_stop(void)    {}

static void ar_cleanup(void) {
    if (g_opus_ms_decoder) {
        opus_multistream_decoder_destroy(g_opus_ms_decoder);
        g_opus_ms_decoder = NULL;
    }
}

// ar_decode decodes one Opus packet and writes raw S16LE PCM to g_audio_pipe_fd.
static void ar_decode(char *data, int len) {
    if (!g_opus_ms_decoder) return;
    opus_int16 pcm[5760 * 8];
    int samples = opus_multistream_decode(
        g_opus_ms_decoder,
        (const unsigned char *)data, len,
        pcm, 5760, 0);
    if (samples <= 0) return;
    int fd = g_audio_pipe_fd;
    if (fd < 0) return;
    int byte_count = samples * g_audio_channels * 2;
    if (g_audio_muted) memset(pcm, 0, byte_count);
    ssize_t n = write(fd, pcm, byte_count);
    (void)n;
}

// ── Video-decoder callbacks ───────────────────────────────────────────────────
static int  dr_setup(int fmt, int w, int h, int rate, void *ctx, int flags) { return 0; }
static void dr_start(void)   {}
static void dr_stop(void)    {}
static void dr_cleanup(void) {}

#ifdef __APPLE__
// ═══════════════════════════════════════════════════════════════════════════════
// VideoToolbox hardware decoder (Darwin only)
// ═══════════════════════════════════════════════════════════════════════════════

// Called from Go: receives one decoded RGBA frame per call.
extern void goVTFrame(uint8_t *rgba, int width, int height);
// Called from Go: diagnostic logging from C VT code.
extern void goVTLog(char *msg);

// VT session state
static VTDecompressionSessionRef  g_vt_session  = NULL;
static CMVideoFormatDescriptionRef g_vt_fmt_desc = NULL;

// SPS / PPS buffers (raw NAL data, no start code)
static uint8_t g_sps_data[1024];
static size_t  g_sps_len = 0;
static uint8_t g_pps_data[256];
static size_t  g_pps_len = 0;

// Monotone PTS counter so VT can order async frames.
static uint64_t g_vt_frame_count = 0;

// vt_invalidate: tear down the VT session and format description.
// Must only be called after LiStopConnection() has joined all threads
// (so no more dr_submit calls are in flight).
static void vt_invalidate(void) {
    if (g_vt_session) {
        VTDecompressionSessionWaitForAsynchronousFrames(g_vt_session);
        VTDecompressionSessionInvalidate(g_vt_session);
        CFRelease(g_vt_session);
        g_vt_session = NULL;
    }
    if (g_vt_fmt_desc) {
        CFRelease(g_vt_fmt_desc);
        g_vt_fmt_desc = NULL;
    }
    g_sps_len = 0;
    g_pps_len = 0;
    g_vt_frame_count = 0;
}

// vt_callback: called by VideoToolbox for each decoded frame (from a VT thread).
static void vt_callback(
    void *ctx, void *frameRefCon,
    OSStatus status, VTDecodeInfoFlags flags,
    CVImageBufferRef img, CMTime pts, CMTime dur)
{
    if (status != noErr || img == NULL) return;

    CVPixelBufferLockBaseAddress(img, kCVPixelBufferLock_ReadOnly);

    int w   = (int)CVPixelBufferGetWidth(img);
    int h   = (int)CVPixelBufferGetHeight(img);
    size_t bpr = CVPixelBufferGetBytesPerRow(img);
    const uint8_t *src = (const uint8_t *)CVPixelBufferGetBaseAddress(img);

    // BGRA → RGBA swap into a malloc'd buffer that goVTFrame will copy from.
    uint8_t *rgba = (uint8_t *)malloc((size_t)w * (size_t)h * 4);
    if (rgba) {
        for (int y = 0; y < h; y++) {
            const uint8_t *row = src + (size_t)y * bpr;
            uint8_t *dst       = rgba + (size_t)y * (size_t)w * 4;
            for (int x = 0; x < w; x++, row += 4, dst += 4) {
                dst[0] = row[2]; // R  (BGRA: B=0 G=1 R=2 A=3)
                dst[1] = row[1]; // G
                dst[2] = row[0]; // B
                dst[3] = row[3]; // A
            }
        }
    }

    CVPixelBufferUnlockBaseAddress(img, kCVPixelBufferLock_ReadOnly);

    if (rgba) {
        goVTFrame(rgba, w, h);
        free(rgba);
    }
}

// vt_create_session: build CMVideoFormatDescription from stored SPS+PPS,
// then create a VTDecompressionSession with BGRA output.
// Requires g_sps_len > 0 && g_pps_len > 0.
static int vt_create_session(void) {
    if (g_vt_session) {
        VTDecompressionSessionWaitForAsynchronousFrames(g_vt_session);
        VTDecompressionSessionInvalidate(g_vt_session);
        CFRelease(g_vt_session);
        g_vt_session = NULL;
    }
    if (g_vt_fmt_desc) {
        CFRelease(g_vt_fmt_desc);
        g_vt_fmt_desc = NULL;
    }

    const uint8_t *params[2] = { g_sps_data, g_pps_data };
    size_t         sizes[2]  = { g_sps_len,  g_pps_len  };

    OSStatus s = CMVideoFormatDescriptionCreateFromH264ParameterSets(
        kCFAllocatorDefault, 2, params, sizes, 4, &g_vt_fmt_desc);
    if (s != noErr) {
        goVTLog((char*)"vt_create_session: CMVideoFormatDescriptionCreateFromH264ParameterSets FAILED");
        return -1;
    }

    // Request BGRA output (most universally supported on macOS).
    int32_t fmt = kCVPixelFormatType_32BGRA;
    CFNumberRef cfFmt = CFNumberCreate(NULL, kCFNumberSInt32Type, &fmt);
    const void *keys[]   = { kCVPixelBufferPixelFormatTypeKey };
    const void *values[] = { cfFmt };
    CFDictionaryRef attrs = CFDictionaryCreate(
        NULL, keys, values, 1,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFRelease(cfFmt);

    VTDecompressionOutputCallbackRecord cb = { vt_callback, NULL };
    s = VTDecompressionSessionCreate(
        kCFAllocatorDefault, g_vt_fmt_desc, NULL, attrs, &cb, &g_vt_session);
    CFRelease(attrs);

    if (s != noErr) {
        goVTLog((char*)"vt_create_session: VTDecompressionSessionCreate FAILED");
        CFRelease(g_vt_fmt_desc);
        g_vt_fmt_desc = NULL;
        return -1;
    }

    // Real-time mode: minimize latency, don't buffer extra frames.
    VTSessionSetProperty(g_vt_session, kVTDecompressionPropertyKey_RealTime, kCFBooleanTrue);
    goVTLog((char*)"vt_create_session: VTDecompressionSession created OK");
    return 0;
}

// walk_annexb: iterate over Annex-B encoded data calling fn(nal, len, ctx)
// for each NAL unit (without start code). Handles both 3-byte and 4-byte start codes.
static void walk_annexb(
    const uint8_t *data, int size,
    void (*fn)(const uint8_t *, int, void *), void *ctx)
{
    // Find first start code (pattern 00 00 01)
    int pos = -1;
    for (int i = 0; i <= size - 3; i++) {
        if (data[i] == 0 && data[i+1] == 0 && data[i+2] == 1) {
            pos = i + 3; // byte after the "01"
            break;
        }
    }
    if (pos < 0) return;

    int nal_start = pos;
    while (nal_start < size) {
        // Find next start code
        int next_sc = -1;
        for (int i = nal_start; i <= size - 3; i++) {
            if (data[i] == 0 && data[i+1] == 0 && data[i+2] == 1) {
                next_sc = i;
                break;
            }
        }

        int nal_end, next_nal_start;
        if (next_sc < 0) {
            nal_end       = size;
            next_nal_start = size;
        } else {
            // Strip trailing zero(s) that belong to the 4-byte start code.
            nal_end = next_sc;
            while (nal_end > nal_start && data[nal_end - 1] == 0) nal_end--;
            next_nal_start = next_sc + 3;
        }

        int nal_len = nal_end - nal_start;
        if (nal_len > 0) fn(data + nal_start, nal_len, ctx);
        nal_start = next_nal_start;
    }
}

// Context for vt_handle_nal.
typedef struct {
    uint8_t *avcc;     // AVCC output buffer (malloc'd)
    int      avcc_len;
    int      avcc_cap;
    int      new_params; // SPS or PPS changed
} vt_nal_ctx;

// vt_handle_nal: called by walk_annexb for each NAL unit.
//   SPS/PPS → store in globals, set new_params.
//   Other   → append as AVCC (4-byte big-endian length + data).
static void vt_handle_nal(const uint8_t *nal, int len, void *ptr) {
    vt_nal_ctx *ctx = (vt_nal_ctx *)ptr;
    if (len <= 0) return;

    int nal_type = nal[0] & 0x1F;

    if (nal_type == 7) { // SPS
        if (len <= (int)sizeof(g_sps_data)) {
            if (g_sps_len != (size_t)len || memcmp(g_sps_data, nal, len) != 0) {
                memcpy(g_sps_data, nal, len);
                g_sps_len = len;
                ctx->new_params = 1;
            }
        }
        return;
    }
    if (nal_type == 8) { // PPS
        if (len <= (int)sizeof(g_pps_data)) {
            if (g_pps_len != (size_t)len || memcmp(g_pps_data, nal, len) != 0) {
                memcpy(g_pps_data, nal, len);
                g_pps_len = len;
                ctx->new_params = 1;
            }
        }
        return;
    }

    // Video / SEI / AUD / other: convert to AVCC and append.
    int needed = ctx->avcc_len + 4 + len;
    if (needed > ctx->avcc_cap) {
        int new_cap = needed * 2 + 64;
        uint8_t *nb = (uint8_t *)realloc(ctx->avcc, new_cap);
        if (!nb) return;
        ctx->avcc     = nb;
        ctx->avcc_cap = new_cap;
    }
    uint8_t *p = ctx->avcc + ctx->avcc_len;
    p[0] = (uint8_t)((len >> 24) & 0xFF);
    p[1] = (uint8_t)((len >> 16) & 0xFF);
    p[2] = (uint8_t)((len >>  8) & 0xFF);
    p[3] = (uint8_t)( len        & 0xFF);
    memcpy(p + 4, nal, len);
    ctx->avcc_len += 4 + len;
}

// vt_dr_submit: VideoToolbox path for dr_submit.
// Parses Annex-B, stores SPS/PPS, feeds AVCC data to VTDecompressionSession.
//
// IMPORTANT: No early-return before parsing — IDR frames carry SPS/PPS that we
// need to parse in order to create the VT session. Returning DR_NEED_IDR before
// parsing would cause an infinite IDR-request loop where we never see the SPS/PPS.
static int vt_dr_submit(PDECODE_UNIT du) {
    // Flatten all LENTRY buffers into one contiguous Annex-B blob.
    int total = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) total += e->length;
    if (total <= 0) return DR_OK;

    uint8_t *ab = (uint8_t *)malloc(total);
    if (!ab) return DR_NEED_IDR;
    int off = 0;
    for (PLENTRY e = du->bufferList; e; e = e->next) {
        memcpy(ab + off, e->data, e->length);
        off += e->length;
    }

    // Walk NAL units: store SPS/PPS and build AVCC payload for video NALs.
    vt_nal_ctx ctx = {
        .avcc      = (uint8_t *)malloc(total + 64),
        .avcc_len  = 0,
        .avcc_cap  = total + 64,
        .new_params = 0,
    };
    if (!ctx.avcc) { free(ab); return DR_NEED_IDR; }

    walk_annexb(ab, total, vt_handle_nal, &ctx);
    free(ab);

    // Create/recreate VT session when SPS/PPS are new or session is missing.
    // Checking !g_vt_session handles retry after a previous creation failure.
    if ((ctx.new_params || !g_vt_session) && g_sps_len > 0 && g_pps_len > 0) {
        if (vt_create_session() != 0) {
            free(ctx.avcc);
            return DR_NEED_IDR;
        }
    }

    // If still no session (haven't seen SPS/PPS yet), request IDR.
    if (!g_vt_session) {
        free(ctx.avcc);
        return DR_NEED_IDR;
    }

    int ret = DR_OK;

    if (ctx.avcc_len > 0 && g_vt_fmt_desc) {
        int avcc_len = ctx.avcc_len;

        // Hand avcc buffer ownership to CMBlockBuffer via kCFAllocatorMalloc
        // so CoreMedia calls free() when the block buffer is released.
        CMBlockBufferRef block = NULL;
        OSStatus s = CMBlockBufferCreateWithMemoryBlock(
            kCFAllocatorDefault,
            ctx.avcc,           // memory block
            avcc_len,
            kCFAllocatorMalloc, // CM calls free(ctx.avcc) when released
            NULL, 0, avcc_len, 0,
            &block);
        if (s == noErr) {
            ctx.avcc = NULL; // CM owns the buffer now

            CMSampleTimingInfo timing = {
                .duration              = kCMTimeInvalid,
                .presentationTimeStamp = CMTimeMake((int64_t)g_vt_frame_count++, 60),
                .decodeTimeStamp       = kCMTimeInvalid,
            };
            size_t sample_sz = (size_t)avcc_len;
            CMSampleBufferRef sample = NULL;
            s = CMSampleBufferCreate(
                kCFAllocatorDefault, block, TRUE, NULL, NULL,
                g_vt_fmt_desc, 1, 1, &timing, 1, &sample_sz, &sample);
            CFRelease(block);

            if (s == noErr) {
                VTDecodeFrameFlags df   = kVTDecodeFrame_EnableAsynchronousDecompression;
                VTDecodeInfoFlags  info = 0;
                s = VTDecompressionSessionDecodeFrame(g_vt_session, sample, df, NULL, &info);
                CFRelease(sample);
                if (s != noErr) ret = DR_NEED_IDR;
            } else {
                ret = DR_NEED_IDR;
            }
        } else {
            ret = DR_NEED_IDR;
        }
    }

    if (ctx.avcc) free(ctx.avcc); // free only if CM didn't take ownership
    return ret;
}

static int dr_submit(PDECODE_UNIT du) { return vt_dr_submit(du); }

#else
// ─────────────────────────────────────────────────────────────────────────────
// Linux video path: write Annex-B H.264 directly into the GStreamer pipe.
// ─────────────────────────────────────────────────────────────────────────────
static int dr_submit(PDECODE_UNIT du) {
    int fd = g_video_pipe_fd;
    if (fd < 0) return DR_NEED_IDR;
    for (PLENTRY e = du->bufferList; e; e = e->next) {
        const char *ptr = e->data;
        int remaining   = e->length;
        while (remaining > 0) {
            ssize_t n = write(fd, ptr, remaining);
            if (n <= 0) return DR_NEED_IDR;
            ptr       += n;
            remaining -= n;
        }
    }
    return DR_OK;
}
#endif // __APPLE__

// ── LiStartConnection entrypoint ──────────────────────────────────────────────
//
// IMPORTANT: LiStartConnection is NON-BLOCKING. It starts internal threads
// (video-receive, control, audio, input), calls connectionStarted(), then returns.
// The background threads continue to call dr_submit() until do_li_stop() joins them.
static int do_li_start(
    const char *address,
    const char *appVersion,
    const char *gfeVersion,
    const char *rtspSessionUrl,
    int serverCodecModeSupport,
    int width, int height, int fps, int bitrate,
    const unsigned char *rikey, // 16 bytes
    int rikeyid,
    int pipeFd   // Linux: video pipe fd; Darwin: unused (VT path)
) {
#ifdef __APPLE__
    (void)pipeFd; // VT handles video; no pipe needed
#else
    g_video_pipe_fd = pipeFd;
#endif

    SERVER_INFORMATION srv;
    LiInitializeServerInformation(&srv);
    srv.address               = address;
    srv.serverInfoAppVersion  = appVersion;
    srv.serverInfoGfeVersion  = gfeVersion;
    srv.rtspSessionUrl        = rtspSessionUrl;
    srv.serverCodecModeSupport = serverCodecModeSupport;

    STREAM_CONFIGURATION cfg;
    LiInitializeStreamConfiguration(&cfg);
    cfg.width                 = width;
    cfg.height                = height;
    cfg.fps                   = fps;
    cfg.bitrate               = bitrate;
    cfg.packetSize            = 1200;
    cfg.streamingRemotely     = STREAM_CFG_AUTO;
    cfg.audioConfiguration    = AUDIO_CONFIGURATION_STEREO;
    cfg.supportedVideoFormats = VIDEO_FORMAT_H264;
    cfg.clientRefreshRateX100 = fps * 100;
    cfg.encryptionFlags       = ENCFLG_NONE;
    if (rikey) {
        memcpy(cfg.remoteInputAesKey, rikey, 16);
        cfg.remoteInputAesIv[0] = (char)( rikeyid        & 0xff);
        cfg.remoteInputAesIv[1] = (char)((rikeyid >>  8) & 0xff);
        cfg.remoteInputAesIv[2] = (char)((rikeyid >> 16) & 0xff);
        cfg.remoteInputAesIv[3] = (char)((rikeyid >> 24) & 0xff);
    }

    DECODER_RENDERER_CALLBACKS dr;
    LiInitializeVideoCallbacks(&dr);
    dr.setup           = dr_setup;
    dr.start           = dr_start;
    dr.stop            = dr_stop;
    dr.cleanup         = dr_cleanup;
    dr.submitDecodeUnit = dr_submit;
    dr.capabilities    = CAPABILITY_DIRECT_SUBMIT;

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
#ifndef __APPLE__
        g_video_pipe_fd = -1;
#endif
        return ret;
    }
    g_li_active = 1;
    return 0;
}

// do_li_stop: stop background threads, tear down VT session (Darwin) or clear pipe fd (Linux).
// Idempotent: the g_li_active guard prevents double-stop from corrupting moonlight-common-c state.
static void do_li_stop(void) {
    if (!g_li_active) return;
    g_li_active = 0;
#ifndef __APPLE__
    g_video_pipe_fd = -1; // stop dr_submit writes before joining threads
#endif
    LiStopConnection();   // blocking: joins all background threads
#ifdef __APPLE__
    vt_invalidate();      // safe now — no more dr_submit calls after thread join
#endif
}

// ── Input forwarders ──────────────────────────────────────────────────────────
static void do_send_key(short vkCode, char action, char modifiers) {
    LiSendKeyboardEvent(vkCode, action, modifiers);
}
static void do_send_mouse_move(short dx, short dy) {
    LiSendMouseMoveEvent(dx, dy);
}
static void do_send_mouse_position(short x, short y, short refW, short refH) {
    LiSendMousePositionEvent(x, y, refW, refH);
}
static void do_send_mouse_button(char action, int button) {
    LiSendMouseButtonEvent(action, button);
}
static void do_send_scroll(signed char clicks) {
    LiSendScrollEvent(clicks);
}
static void do_send_multi_controller(
    unsigned short controllerNumber, unsigned short activeGamepadMask,
    unsigned short buttons,
    unsigned char leftTrigger, unsigned char rightTrigger,
    short leftStickX, short leftStickY,
    short rightStickX, short rightStickY)
{
    LiSendMultiControllerEvent(controllerNumber, activeGamepadMask, buttons,
        leftTrigger, rightTrigger,
        leftStickX, leftStickY, rightStickX, rightStickY);
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

// liStartConnectionActive is true while LiStartConnection's background threads run.
var liStartConnectionActive atomic.Bool

// activeStream* coordinates the goroutine waiting for stream termination.
var (
	activeStreamDone    chan struct{}
	activeStreamOnce    sync.Once
	activeStreamTermErr error
)

func closeActiveStreamDone() {
	activeStreamOnce.Do(func() { close(activeStreamDone) })
}

// vtFrameCallback receives decoded RGBA frames from the VideoToolbox path (Darwin).
// Set by moonlight_player_darwin.go before StartStream is called.
var (
	vtFrameCallback   func(image.Image)
	vtFrameCallbackMu sync.Mutex
)

// MoonlightCgoWrapper calls LiStartConnection from moonlight-common-c.
type MoonlightCgoWrapper struct {
	host           string
	pipeWrite      *os.File
	audioPipeWrite *os.File
	audioMuted     bool
}

func NewMoonlightCgoWrapper(host string) *MoonlightCgoWrapper {
	return &MoonlightCgoWrapper{host: host}
}

// StartStream launches LiStartConnection in a goroutine, waits for termination,
// then closes pipeWrite (if non-nil) and calls onStop.
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
	w.pipeWrite      = pipeWrite
	w.audioPipeWrite = audioPipeWrite

	activeStreamDone = make(chan struct{})
	activeStreamOnce = sync.Once{}
	activeStreamTermErr = nil

	host   := C.CString(w.host)
	appVer := C.CString(appVersion)
	gfeVer := C.CString(gfeVersion)
	rtsp   := C.CString("rtsp://" + rtspSessionUrl)

	var cRikey *C.uchar
	if len(rikey) == 16 {
		cRikey = (*C.uchar)(C.CBytes(rikey))
	}

	// pipeFd: on Darwin (VT path) pipeWrite may be nil; pass -1 so do_li_start ignores it.
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

		logrus.Infof("🌕 [Moonlight/CGO] LiStartConnection: host=%s rtsp=%s %dx%d@%d bitrate=%d",
			w.host, "rtsp://"+rtspSessionUrl, width, height, fps, bitrate)

		if audioPipeWrite != nil {
			C.set_audio_pipe_fd(C.int(audioPipeWrite.Fd()))
		}

		ret := C.do_li_start(
			host, appVer, gfeVer, rtsp,
			C.int(serverCodecModeSupport),
			C.int(width), C.int(height), C.int(fps), C.int(bitrate),
			cRikey, C.int(1),
			pipeFd,
		)

		if int(ret) != 0 {
			logrus.Errorf("🌕 [Moonlight/CGO] LiStartConnection setup FAILED: code=%d", int(ret))
			C.set_audio_pipe_fd(-1)
			if pipeWrite != nil {
				_ = pipeWrite.Close()
			}
			if audioPipeWrite != nil {
				_ = audioPipeWrite.Close()
			}
			if onStop != nil {
				onStop(fmt.Errorf("LiStartConnection error code %d", int(ret)))
			}
			return
		}

		logrus.Info("🌕 [Moonlight/CGO] ✅ LiStartConnection setup done — streams active")
		liStartConnectionActive.Store(true)

		<-activeStreamDone

		logrus.Info("🌕 [Moonlight/CGO] termination received — stopping streams")
		C.do_li_stop()
		C.set_audio_pipe_fd(-1)

		// Clear VT frame callback so stale frames don't arrive after reconnect.
		vtFrameCallbackMu.Lock()
		vtFrameCallback = nil
		vtFrameCallbackMu.Unlock()

		liStartConnectionActive.Store(false)

		if pipeWrite != nil {
			_ = pipeWrite.Close()
		}
		if audioPipeWrite != nil {
			_ = audioPipeWrite.Close()
		}

		if onStop != nil {
			onStop(activeStreamTermErr)
		}
	}()

	return nil
}

// StopStream interrupts the active stream. Safe to call when no stream is running.
func (w *MoonlightCgoWrapper) StopStream() {
	if !liStartConnectionActive.Load() {
		logrus.Info("🌕 [Moonlight/CGO] StopStream: no active stream — skipping")
		return
	}
	logrus.Info("🌕 [Moonlight/CGO] StopStream: stopping active stream")
	C.do_li_stop()
	closeActiveStreamDone()
}

// ── CGO-exported Go callbacks (called from C threads) ─────────────────────────

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

// goVTLog is called from C VT code for diagnostic logging.
//
//export goVTLog
func goVTLog(msg *C.char) {
	logrus.Infof("🍎 [Moonlight/VT] %s", C.GoString(msg))
}

var vtFrameCount int64

// goVTFrame is called from the VideoToolbox decompression callback (Darwin).
// rgba points to a C malloc'd BGRA→RGBA-swapped buffer; we copy it into a Go
// image before returning (C frees rgba immediately after this call returns).
//
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
		logrus.Infof("🍎 [Moonlight/VT] ✅ first RGBA frame decoded — %dx%d, VideoToolbox is working!", w, h)
	}
	cb(img)
}

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

// ── Audio mute ────────────────────────────────────────────────────────────────

func (w *MoonlightCgoWrapper) SetAudioMuted(muted bool) {
	w.audioMuted = muted
	if muted {
		C.set_audio_muted(1)
	} else {
		C.set_audio_muted(0)
	}
}

func (w *MoonlightCgoWrapper) GetAudioMuted() bool {
	return w.audioMuted
}
