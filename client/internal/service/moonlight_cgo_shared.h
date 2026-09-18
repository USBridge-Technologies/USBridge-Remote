/*
 * moonlight_cgo_shared.h — shared moonlight C code included by each platform CGO file.
 *
 * Defines: opus decoder lifecycle, connection callbacks, audio/video dispatch stubs,
 *          do_li_start / do_li_stop, input forwarders.
 *
 * Each platform CGO file MUST define the following symbols before #include-ing this:
 *   int  platform_dr_submit(PDECODE_UNIT du)
 *   void platform_ar_init(int channels, int sample_rate)
 *   void platform_ar_cleanup(void)
 *   void platform_ar_decode(const opus_int16 *pcm, int byte_count, int samples)
 *
 * And must forward-declare the Go callbacks it calls:
 *   extern void goMoonlightStage(int stage, int result, int errCode);
 *   extern void goMoonlightConnected(void);
 *   extern void goMoonlightTerminated(int errCode);
 *   extern void goVideoFormatNegotiated(int videoFormat);
 */

#pragma once

#include <Limelight.h>
#include <opus_multistream.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

// ── Shared state ──────────────────────────────────────────────────────────────

static volatile int g_li_active          = 0;
static volatile int g_audio_muted        = 0;
static OpusMSDecoder *g_opus_ms_decoder  = NULL;
static int g_audio_channels              = 2;
static int g_audio_samples_per_frame     = 960;

// Audio diagnostics counters (reset in ar_init, read by platform stats logger).
static volatile uint64_t g_ar_plc_count   = 0; // Opus PLC frames (network packet loss)
static volatile uint64_t g_ar_err_count   = 0; // opus_multistream_decode errors
static volatile uint64_t g_ar_muted_count = 0; // frames silenced because muted

// Most recently received DECODE_UNIT.frameHostProcessingLatency (1/10 ms
// units, 0 = host doesn't provide it -- see Limelight.h's own doc comment
// on that field). Captured once here in the shared dr_submit trampoline
// rather than duplicated in every platform's own platform_dr_submit, so
// net_graph.go's "how fast is the host capturing+encoding" number works
// identically on every platform once the host (rust-shine) actually fills
// this in -- it's a standard Sunshine-protocol field, not something we
// invented, and stock Sunshine/Apollo builds that populate it get the same
// treatment for free.
static volatile uint16_t g_last_host_latency_tenths_ms = 0;

// These functions are called from moonlight_cgo_wrapper.go's TU via extern declarations.
// They must have external (non-static) linkage so the linker can resolve them
// from the platform CGO file's object. Build tags ensure only one platform file
// is compiled per build, so there are no multiple-definition conflicts.
void set_audio_pipe_fd(int fd) { (void)fd; }
void set_audio_muted(int muted) { g_audio_muted = muted; }

// ── Platform interface — defined by each platform CGO file ─────────────────────
// Declarations must come BEFORE the shared callbacks that call them.
extern int  platform_dr_submit(PDECODE_UNIT du);
extern void platform_ar_init(int channels, int sample_rate);
extern void platform_ar_cleanup(void);
extern void platform_ar_decode(const opus_int16 *pcm, int byte_count, int samples);
extern void platform_post_stop(void); // called after LiStopConnection joins threads

// ── Connection-listener callbacks ─────────────────────────────────────────────

static void cl_stage_starting(int s)      { goMoonlightStage(s,  0, 0); }
static void cl_stage_complete(int s)       { goMoonlightStage(s,  1, 0); }
static void cl_stage_failed(int s, int ec) { goMoonlightStage(s, -1, ec); }
static void cl_connected(void)             { goMoonlightConnected(); }
static void cl_terminated(int ec)          { goMoonlightTerminated(ec); }
static void cl_log(const char *fmt, ...) {
    char buf[256];
    va_list ap; va_start(ap, fmt); vsnprintf(buf, sizeof(buf), fmt, ap); va_end(ap);
    // strip trailing newline
    int n = (int)strlen(buf);
    if (n > 0 && buf[n-1] == '\n') buf[n-1] = '\0';
    if (buf[0]) goVTLog(buf);
}

// ── Audio callbacks ────────────────────────────────────────────────────────────

static int ar_init(int audioConfig, const POPUS_MULTISTREAM_CONFIGURATION cfg, void *ctx, int flags) {
    (void)audioConfig; (void)ctx; (void)flags;
    g_audio_channels         = cfg->channelCount;
    g_audio_samples_per_frame = cfg->samplesPerFrame > 0 ? cfg->samplesPerFrame : 960;
    g_ar_plc_count   = 0;
    g_ar_err_count   = 0;
    g_ar_muted_count = 0;
    if (g_opus_ms_decoder) {
        opus_multistream_decoder_destroy(g_opus_ms_decoder);
        g_opus_ms_decoder = NULL;
    }
    int error = OPUS_OK;
    g_opus_ms_decoder = opus_multistream_decoder_create(
        cfg->sampleRate, cfg->channelCount,
        cfg->streams, cfg->coupledStreams,
        cfg->mapping, &error);
    if (error != OPUS_OK) return -1;
    platform_ar_init(g_audio_channels, (int)cfg->sampleRate);
    return 0;
}

static void ar_start(void)   {}
static void ar_stop(void)    {}

static void ar_cleanup(void) {
    platform_ar_cleanup();
    if (g_opus_ms_decoder) {
        opus_multistream_decoder_destroy(g_opus_ms_decoder);
        g_opus_ms_decoder = NULL;
    }
}

static void ar_decode(char *data, int len) {
    if (!g_opus_ms_decoder) return;
    if (data == NULL || len == 0) g_ar_plc_count++;
    opus_int16 pcm[5760 * 8];
    int samples = opus_multistream_decode(
        g_opus_ms_decoder,
        (const unsigned char *)data, len,
        pcm, 5760, 0);
    if (samples <= 0) {
        g_ar_err_count++;
        if (g_ar_err_count <= 5 || g_ar_err_count % 100 == 0) {
            char buf[80];
            snprintf(buf, sizeof(buf), "Opus decode error: %d (total=%llu)",
                     samples, (unsigned long long)g_ar_err_count);
            goVTLog(buf);
        }
        return;
    }
    int byte_count = samples * g_audio_channels * 2;
    if (g_audio_muted) {
        memset(pcm, 0, byte_count);
        g_ar_muted_count++;
    }
    platform_ar_decode(pcm, byte_count, samples);
}

// ── Video callbacks ───────────────────────────────────────────────────────────

// platform_set_video_format lets the platform decoder know which codec was negotiated.
// Implemented in each platform CGO file (apple: sets g_codec_type; others: no-op).
extern void platform_set_video_format(int videoFormat);

static int  dr_setup(int fmt, int w, int h, int rate, void *ctx, int flags) {
    (void)ctx; (void)flags;
    platform_set_video_format(fmt);
    goVideoFormatNegotiated(fmt);
    char buf[128];
    snprintf(buf, sizeof(buf), "dr_setup: negotiated fmt=0x%04X, %dx%d@%d", fmt, w, h, rate);
    goVTLog(buf);
    return 0;
}
static void dr_start(void)   {}
static void dr_stop(void)    {}
static void dr_cleanup(void) {}

static int dr_submit(PDECODE_UNIT du) {
    g_last_host_latency_tenths_ms = du->frameHostProcessingLatency;
    return platform_dr_submit(du);
}

// ── LiStartConnection entrypoint ──────────────────────────────────────────────
//
// pipeFd is ignored — all platforms decode natively without a pipe.

int do_li_start(
    const char *address,
    const char *appVersion,
    const char *gfeVersion,
    const char *rtspSessionUrl,
    int serverCodecModeSupport,
    int videoFormat,
    int width, int height, int fps, int bitrate,
    const unsigned char *rikey,
    int rikeyid,
    int pipeFd
) {
    (void)pipeFd;
    if (videoFormat == 0) videoFormat = VIDEO_FORMAT_H264; // default
    printf("DEBUG: do_li_start(addr=%s, codec_fmt=%d, %dx%d@%d, bit=%d)\n",
           address, videoFormat, width, height, fps, bitrate);

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
    cfg.supportedVideoFormats = videoFormat;
    cfg.clientRefreshRateX100 = fps * 100;
    // Opt into audio encryption exactly like the official Moonlight clients do
    // (moonlight-android/moonlight-qt default to ENCFLG_AUDIO).
    cfg.encryptionFlags       = ENCFLG_AUDIO;
    if (rikey) {
        memcpy(cfg.remoteInputAesKey, rikey, 16);
        // remoteInputAesIv holds the rikeyid in BIG-endian (network) byte order —
        // that is what we sent as "rikeyid" in /launch and what AudioStream.c
        // reads back via BE32() to build the per-packet audio AES-CBC IV.
        // Writing it little-endian corrupted only the first 16 bytes of every
        // decrypted audio packet (CBC feeds the IV into the first block only),
        // so the Opus TOC byte was garbage while the rest of the packet was
        // intact: decode "succeeded" and played garbled noise.
        cfg.remoteInputAesIv[0] = (char)((rikeyid >> 24) & 0xff);
        cfg.remoteInputAesIv[1] = (char)((rikeyid >> 16) & 0xff);
        cfg.remoteInputAesIv[2] = (char)((rikeyid >>  8) & 0xff);
        cfg.remoteInputAesIv[3] = (char)( rikeyid        & 0xff);
    }

    DECODER_RENDERER_CALLBACKS dr;
    LiInitializeVideoCallbacks(&dr);
    dr.setup            = dr_setup;
    dr.start            = dr_start;
    dr.stop             = dr_stop;
    dr.cleanup          = dr_cleanup;
    dr.submitDecodeUnit = dr_submit;
    // CAPABILITY_REFERENCE_FRAME_INVALIDATION_* tells moonlight-common-c
    // this decoder can tolerate the corrupted/partial frames a reference-
    // frame-invalidation recovery produces without crashing -- true of any
    // real H.264/HEVC/AV1 decoder (libavcodec here; same as the official
    // moonlight-qt/moonlight-android clients, which set the identical bits
    // unconditionally for the same reason), never actually used unless the
    // *host* also advertises `x-nv-video[0].refPicInvalidation` in its RTSP
    // DESCRIBE response -- isReferenceFrameInvalidationEnabled() (Misc.c)
    // requires both sides. Without this, a host that does advertise RFI
    // support still gets the client falling back to full on-wire IDR
    // requests with strict IDR-wait blocking on every loss event (confirmed
    // live: a rust-shine host advertising the flag produced no behavior
    // change at all client-side until this was added) -- silently, with no
    // error, since ReferenceFrameInvalidationSupported alone was never
    // enough on its own.
    // CAPABILITY_DIRECT_SUBMIT: an attempt to remove this (to activate
    // moonlight-common-c's own queue+decoder-thread machinery in
    // VideoDepacketizer.c/VideoStream.c, where an adaptive playout jitter
    // buffer was added -- see that file's playoutDelayForFrame) was tried
    // live and reverted. It fixed the buffer (confirmed working: applied
    // delay tracked jitter correctly, stalls dropped sharply), but moving
    // decode/render off the network receive thread onto a separate thread
    // caused a *different*, worse regression: real render throughput to the
    // screen collapsed to ~10-15fps while decode itself kept running at the
    // full ~60fps (confirmed via the VT-decode-fps vs Metal-rendered-fps
    // counters diverging live) -- something about this Metal/CVDisplayLink
    // path doesn't tolerate decode happening off its accustomed thread, and
    // it wasn't safe to leave running while diagnosing further. Keep
    // CAPABILITY_DIRECT_SUBMIT set until that's understood; the jitter
    // buffer code is left in place (harmless, unreachable while this flag
    // is set) for whoever picks this back up.
    dr.capabilities = CAPABILITY_DIRECT_SUBMIT | CAPABILITY_REFERENCE_FRAME_INVALIDATION_AVC | CAPABILITY_REFERENCE_FRAME_INVALIDATION_HEVC | CAPABILITY_REFERENCE_FRAME_INVALIDATION_AV1;

    AUDIO_RENDERER_CALLBACKS ar;
    LiInitializeAudioCallbacks(&ar);
    ar.init               = ar_init;
    ar.start              = ar_start;
    ar.stop               = ar_stop;
    ar.cleanup            = ar_cleanup;
    ar.decodeAndPlaySample = ar_decode;
    // Requests AudioPacketDuration=10ms instead of the 5ms
    // lowest-latency default (SdpGenerator.c) -- the official protocol's
    // own branch for this, not a wire-format deviation: 5ms frames are
    // CELT-only by the Opus spec and can never carry Opus's own inband FEC
    // no matter what the host does, only the fixed-33%/20ms-block outer
    // Reed-Solomon FEC (RtpAudioQueue's 4+2 scheme) protects them. 10ms
    // frames are SILK/Hybrid-eligible, letting a host that enables inband
    // FEC (see rust-shine's OpusEncoder::set_inband_fec) actually recover
    // a single lost packet from the very next one, no RS block wait
    // needed. This client doesn't have a genuinely slow decoder -- the
    // capability bit is repurposed here purely to opt into the duration
    // it happens to gate, matching what the user explicitly chose over
    // the alternative (a custom >10ms duration outside what the real
    // protocol's own SdpGenerator.c logic ever produces).
    ar.capabilities = CAPABILITY_SLOW_OPUS_DECODER;

    CONNECTION_LISTENER_CALLBACKS cl;
    LiInitializeConnectionCallbacks(&cl);
    cl.stageStarting        = cl_stage_starting;
    cl.stageComplete        = cl_stage_complete;
    cl.stageFailed          = cl_stage_failed;
    cl.connectionStarted    = cl_connected;
    cl.connectionTerminated = cl_terminated;
    cl.logMessage           = cl_log;

    int ret = LiStartConnection(&srv, &cfg, &cl, &dr, &ar, NULL, 0, NULL, 0);
    if (ret != 0) return ret;
    g_li_active = 1;
    return 0;
}

// do_get_rtp_video_stats copies LiGetRTPVideoStats()'s 7 uint32 fields into
// `out` (packetCountVideo, packetCountFec, packetCountFecRecovered,
// packetCountFecFailed, packetCountOOS, packetCountInvalid,
// packetCountFecInvalid, in that struct order) — lets the Go side log these
// without a cgo binding per field.
void do_get_rtp_video_stats(uint32_t *out) {
    const RTP_VIDEO_STATS *stats = LiGetRTPVideoStats();
    out[0] = stats->packetCountVideo;
    out[1] = stats->packetCountFec;
    out[2] = stats->packetCountFecRecovered;
    out[3] = stats->packetCountFecFailed;
    out[4] = stats->packetCountOOS;
    out[5] = stats->packetCountInvalid;
    out[6] = stats->packetCountFecInvalid;
}

// do_get_estimated_rtt_info copies LiGetEstimatedRttInfo()'s two uint32
// outputs into `out` (estimatedRtt, estimatedRttVariance, both ms) and
// returns 1 if moonlight-common-c had a real RTT estimate to report, 0
// otherwise (e.g. no active session yet) -- mirrors do_get_rtp_video_stats's
// no-cgo-binding-per-field approach.
int do_get_estimated_rtt_info(uint32_t *out) {
    uint32_t rtt = 0, rttVariance = 0;
    int ok = LiGetEstimatedRttInfo(&rtt, &rttVariance) ? 1 : 0;
    out[0] = rtt;
    out[1] = rttVariance;
    return ok;
}

// do_get_last_host_latency_tenths_ms returns the most recent frame's
// DECODE_UNIT.frameHostProcessingLatency (see g_last_host_latency_tenths_ms's
// doc comment) -- 0 means the host hasn't provided this for the current/most
// recent frame (either an older/non-Sunshine-protocol host, or the host
// simply not filling it in).
uint16_t do_get_last_host_latency_tenths_ms(void) {
    return g_last_host_latency_tenths_ms;
}

// do_get_playout_jitter_us/do_get_playout_applied_delay_us expose
// LiGetPlayoutJitterUs/LiGetPlayoutAppliedDelayUs (VideoDepacketizer.c) --
// client-side arrival-jitter numbers, distinct from the network RTT
// variance do_get_estimated_rtt_info reports.
uint64_t do_get_playout_jitter_us(void) {
    return LiGetPlayoutJitterUs();
}

uint64_t do_get_playout_applied_delay_us(void) {
    return LiGetPlayoutAppliedDelayUs();
}

void do_li_stop(void) {
    if (!g_li_active) return;
    g_li_active = 0;
    LiStopConnection(); // blocking — joins all background threads
    platform_post_stop();
}

// Safe to call at any time, including while a LiStartConnection() is in
// flight on another thread (unlike do_li_stop/LiStopConnection, which is
// documented as not safe to call concurrently with LiStartConnection).
void do_li_interrupt(void) {
    LiInterruptConnection();
}

// ── Input forwarders ──────────────────────────────────────────────────────────

void do_send_key(short vkCode, char action, char modifiers) {
    LiSendKeyboardEvent(vkCode, action, modifiers);
}
void do_send_mouse_move(short dx, short dy) {
    LiSendMouseMoveEvent(dx, dy);
}
void do_send_mouse_position(short x, short y, short refW, short refH) {
    LiSendMousePositionEvent(x, y, refW, refH);
}
void do_send_mouse_button(char action, int button) {
    LiSendMouseButtonEvent(action, button);
}
void do_send_scroll(signed char clicks) {
    LiSendScrollEvent(clicks);
}
void do_send_multi_controller(
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
void do_send_utf8_text(const char *text, unsigned int len) {
    LiSendUtf8TextEvent(text, len);
}
void do_send_pen(unsigned char eventType, unsigned char toolType, unsigned char penButtons,
                  float x, float y, float pressureOrDistance,
                  unsigned short rotation, unsigned char tilt) {
    LiSendPenEvent(eventType, toolType, penButtons, x, y, pressureOrDistance,
        0.0f, 0.0f, rotation, tilt);
}
