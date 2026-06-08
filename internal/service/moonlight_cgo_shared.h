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
 */

#pragma once

#include <Limelight.h>
#include <opus_multistream.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

// ── Shared state ──────────────────────────────────────────────────────────────

static volatile int g_li_active          = 0;
static volatile int g_audio_muted        = 0;
static OpusMSDecoder *g_opus_ms_decoder  = NULL;
static int g_audio_channels              = 2;
static int g_audio_samples_per_frame     = 960;

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
static void cl_log(const char *fmt, ...)   { (void)fmt; }

// ── Audio callbacks ────────────────────────────────────────────────────────────

static int ar_init(int audioConfig, const POPUS_MULTISTREAM_CONFIGURATION cfg, void *ctx, int flags) {
    (void)audioConfig; (void)ctx; (void)flags;
    g_audio_channels         = cfg->channelCount;
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
    opus_int16 pcm[5760 * 8];
    int samples = opus_multistream_decode(
        g_opus_ms_decoder,
        (const unsigned char *)data, len,
        pcm, 5760, 0);
    if (samples <= 0) return;
    int byte_count = samples * g_audio_channels * 2;
    if (g_audio_muted) memset(pcm, 0, byte_count);
    platform_ar_decode(pcm, byte_count, samples);
}

// ── Video callbacks ───────────────────────────────────────────────────────────

static int  dr_setup(int fmt, int w, int h, int rate, void *ctx, int flags) {
    (void)fmt; (void)w; (void)h; (void)rate; (void)ctx; (void)flags;
    return 0;
}
static void dr_start(void)   {}
static void dr_stop(void)    {}
static void dr_cleanup(void) {}

static int dr_submit(PDECODE_UNIT du) { return platform_dr_submit(du); }

// ── LiStartConnection entrypoint ──────────────────────────────────────────────
//
// pipeFd is ignored — all platforms decode natively without a pipe.

int do_li_start(
    const char *address,
    const char *appVersion,
    const char *gfeVersion,
    const char *rtspSessionUrl,
    int serverCodecModeSupport,
    int width, int height, int fps, int bitrate,
    const unsigned char *rikey,
    int rikeyid,
    int pipeFd
) {
    (void)pipeFd;

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
    if (ret != 0) return ret;
    g_li_active = 1;
    return 0;
}

void do_li_stop(void) {
    if (!g_li_active) return;
    g_li_active = 0;
    LiStopConnection(); // blocking — joins all background threads
    platform_post_stop();
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
