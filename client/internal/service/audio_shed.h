/*
 * audio_shed.h — when a platform audio output drops a decoded frame instead of
 * queueing it. Shared by every platform's audio path (AAudio, WASAPI,
 * PulseAudio, CoreAudio); each one only reports how much it has queued.
 *
 * The host's capture card clocks its audio a little off 48 kHz, and a network
 * burst delivers several frames at once. Either way the device queue only ever
 * grows -- a blocking write then pins it full (Android sat at ~160 ms of lag,
 * the surplus coming out as "write short" glitches). Dropping the newest frame
 * while the queue holds more than AUDIO_SHED_QUEUE_MS keeps the lag bounded:
 * at a typical drift of a few hundred ppm that's one 10 ms frame every few
 * tens of seconds.
 */

#pragma once

#include <stdint.h>

#define AUDIO_SHED_QUEUE_MS 80

// queued_frames: what the device still has to play; floor_frames: the least
// the device itself needs queued (e.g. two hardware bursts), if that's more.
static inline int audio_should_shed(int64_t queued_frames, int sample_rate, int64_t floor_frames) {
    int64_t high = (int64_t)sample_rate * AUDIO_SHED_QUEUE_MS / 1000;
    if (floor_frames > high) high = floor_frames;
    return queued_frames > high;
}
