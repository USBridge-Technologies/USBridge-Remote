/*
 * audio_shed.h — how much decoded audio a platform output keeps queued: when
 * it starts playing (audio_prebuffer_ms) and when it drops a decoded frame
 * instead of queueing it (audio_should_shed). Shared by every platform's
 * audio path (AAudio, WASAPI, PulseAudio, CoreAudio); each one only reports
 * how much it has queued.
 *
 * The host's capture card clocks its audio a little off 48 kHz, and a network
 * burst delivers several frames at once. Either way the device queue only ever
 * grows -- a blocking write then pins it full (Android sat at ~160 ms of lag,
 * the surplus coming out as "write short" glitches). Dropping the newest frame
 * while the queue holds more than the bound keeps the lag bounded: at a
 * typical drift of a few hundred ppm that's one 10 ms frame every few tens of
 * seconds.
 *
 * Both follow the video latency/smoothness slider (g_audio_smooth_level, set
 * with service.SetPlayoutBufferLevel): a Wi-Fi stall longer than what's
 * queued is an underrun, heard as crackle, so the smoother levels queue more.
 */

#pragma once

#include <stdint.h>

// 0 (off) .. 4 (smoothest), see audio_level.c.
extern int g_audio_smooth_level;

// Queued before (re)starting playback, per slider level; the shed bound sits
// AUDIO_SHED_HEADROOM_MS above it, room for a burst to land.
static const int AUDIO_PREBUFFER_MS[5] = { 40, 40, 60, 80, 120 };
#define AUDIO_SHED_HEADROOM_MS 40
// The largest bound any level asks for -- device buffers must hold it.
#define AUDIO_QUEUE_MAX_MS (120 + AUDIO_SHED_HEADROOM_MS)

static inline int audio_prebuffer_ms(void) {
    int level = g_audio_smooth_level;
    if (level < 0) level = 0;
    if (level > 4) level = 4;
    return AUDIO_PREBUFFER_MS[level];
}

// queued_frames: what the device still has to play; floor_frames: the least
// the device itself needs queued (e.g. two hardware bursts), if that's more.
static inline int audio_should_shed(int64_t queued_frames, int sample_rate, int64_t floor_frames) {
    int64_t high = (int64_t)sample_rate * (audio_prebuffer_ms() + AUDIO_SHED_HEADROOM_MS) / 1000;
    if (floor_frames > high) high = floor_frames;
    return queued_frames > high;
}
