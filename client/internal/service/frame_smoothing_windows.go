//go:build windows && cgo

package service

/*
#include <stdint.h>

extern void vk_video_set_concealment_enabled(int enabled);
extern void vk_video_get_conceal_stats(long long *concealed_frames, int *concealing);
*/
import "C"

import "os"

// init wires frame_smoothing.go's platform-agnostic toggle to
// vk_video_impl_windows.c's g_conceal_enabled atomic -- same "core stays
// tag-free, platform file wires the hook" split as ai_vision_windows.go/
// net_graph_windows.go's init() functions.
func init() {
	frameSmoothingSetEnabledHook = func(enabled bool) {
		e := C.int(0)
		if enabled {
			e = 1
		}
		C.vk_video_set_concealment_enabled(e)
	}
	// USBRIDGE_FRAME_SMOOTHING=1: force the feature on at startup without
	// clicking the checkbox -- a debug/QA aid (e.g. driving a build via a
	// deep link with no UI interaction). No effect unless set; the checkbox
	// remains the normal path. Must come after the hook is wired above
	// (same init(), sequential) so SetFrameSmoothingEnabled's call into the
	// native setter isn't silently dropped by a nil hook.
	if os.Getenv("USBRIDGE_FRAME_SMOOTHING") == "1" {
		SetFrameSmoothingEnabled(true)
	}
}

// FrameSmoothingStats mirrors vk_video_get_conceal_stats for the Net Graph
// HUD and any other caller that wants to show concealment activity.
type FrameSmoothingStats struct {
	ConcealedFrames int64
	Concealing      bool
}

// GetFrameSmoothingStats returns the running count of synthesized
// (motion-extrapolated) frames presented so far and whether the render
// thread is mid-stall right now.
func GetFrameSmoothingStats() FrameSmoothingStats {
	var n C.longlong
	var concealing C.int
	C.vk_video_get_conceal_stats(&n, &concealing)
	return FrameSmoothingStats{
		ConcealedFrames: int64(n),
		Concealing:      concealing != 0,
	}
}

// goFrameSmoothingDecide is vk_render_frame_conceal's (vk_video_impl_windows.c)
// entry point into decideConcealment (frame_smoothing.go) -- the render
// thread executes the decision, Go makes it. Returns 1/0 instead of a Go
// bool since this crosses the cgo boundary; extrapolateTOut is written only
// when this returns 1 (left untouched otherwise, matching the C caller's
// own "only read it when conceal is true" usage).
//
//export goFrameSmoothingDecide
func goFrameSmoothingDecide(elapsedMs, expectedMs C.double, consecutive C.int, extrapolateTOut *C.float) C.int {
	conceal, t := decideConcealment(float64(elapsedMs), float64(expectedMs), int(consecutive))
	if !conceal {
		return 0
	}
	if extrapolateTOut != nil {
		*extrapolateTOut = C.float(t)
	}
	return 1
}

// goFrameSmoothingUpdateInterval is vk_render_frame's entry point into
// updateExpectedIntervalMs (frame_smoothing.go), called once per real frame
// render to fold its measured arrival interval into the rolling EMA
// baseline decideConcealment compares future gaps against.
//
//export goFrameSmoothingUpdateInterval
func goFrameSmoothingUpdateInterval(prevEma, newIntervalMs C.double) C.double {
	return C.double(updateExpectedIntervalMs(float64(prevEma), float64(newIntervalMs)))
}
