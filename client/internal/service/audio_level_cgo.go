//go:build cgo

package service

/*
extern int g_audio_smooth_level;
static void set_audio_smooth_level(int level) { g_audio_smooth_level = level; }
*/
import "C"

// The video latency/smoothness slider also sizes the audio queue (see
// audio_shed.h): a stall longer than what's queued is heard as crackle.
func init() {
	playoutBufferLevelHook = func(level int) { C.set_audio_smooth_level(C.int(level)) }
}
