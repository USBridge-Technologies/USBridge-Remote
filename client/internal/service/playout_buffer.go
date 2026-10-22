package service

import "sync/atomic"

// The jitter buffer: the moonlight-common-c fork's FramePacer.h, which holds
// each frame just long enough that frames captured at an even cadence also
// reach the screen evenly (Android paces at the display, every other
// platform before decode -- see VideoDepacketizer.c). It costs latency, so
// it is the video settings dialog's latency/smoothness slider wherever
// PlayoutBufferSupported() is true: level 0 is off, 1..PlayoutBufferMaxLevel
// trade more latency for fewer late frames (FramePacer.h's
// FRAME_PACER_LEVEL). Off by default.
var playoutBufferLevel atomic.Int32

// PlayoutBufferMaxLevel matches FramePacer.h's FRAME_PACER_LEVELS.
const PlayoutBufferMaxLevel = 4

// SetPlayoutBufferLevel sets the slider for the next stream start (clamped
// to 0..PlayoutBufferMaxLevel). The video settings dialog applies this from
// Apply/Start; Cancel/close leaves the previous level.
func SetPlayoutBufferLevel(level int) {
	level = ClampPlayoutBufferLevel(level)
	playoutBufferLevel.Store(int32(level))
	if hook := playoutBufferLevelHook; hook != nil {
		hook(level)
	}
}

// playoutBufferLevelHook passes the level on to the native audio outputs
// (audio_level_cgo.go); nil without cgo.
var playoutBufferLevelHook func(level int)

// PlayoutBufferLevel reports the slider's current level.
func PlayoutBufferLevel() int {
	return int(playoutBufferLevel.Load())
}

// ClampPlayoutBufferLevel bounds a stored or dragged level.
func ClampPlayoutBufferLevel(level int) int {
	if level < 0 {
		return 0
	}
	if level > PlayoutBufferMaxLevel {
		return PlayoutBufferMaxLevel
	}
	return level
}
