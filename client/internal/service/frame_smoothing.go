package service

import "sync/atomic"

// Frame Smoothing is an optional, opt-in fallback for the Windows/Vulkan
// render path (see vk_video_impl_windows.c's g_of_backend / warp_extrapolate
// compute pass): when the network stalls and the next real decoded frame is
// late, instead of leaving the display frozen on the last real frame until
// it finally arrives, the render thread motion-extrapolates a synthetic
// frame from the last two real frames and presents that instead -- purely a
// concealment measure for the gap. The instant a real frame is ready it
// takes over immediately; this never delays or reorders real frames, so the
// happy path (no stall) is completely unaffected.
//
// Same split as AI Vision/Net Graph (ai_vision.go, net_graph.go): this file
// has no build tag and touches no cgo. All the *decision* math -- is the
// current gap since the last real frame long enough to justify concealing
// it, and how far forward should the extrapolation reach -- lives here as
// pure functions over plain float64/int inputs so it's unit testable
// without a GPU. The native render thread (vk_video_impl_windows.c) calls
// into these via a cgo export and only ever executes the decision, never
// makes it.
const (
	// frameSmoothingTriggerRatio: a gap must exceed this multiple of the
	// measured expected inter-frame interval before it's treated as "late"
	// rather than ordinary frame-to-frame jitter. 1.3x leaves headroom for
	// normal pacing wobble while still catching a genuinely missed frame
	// quickly (at 60fps/~16.6ms expected, that's a ~29.9ms trigger).
	//
	// History, three rounds of live-testing against a real direct-LAN
	// stream (see app.log) plus one real packet-loss test:
	//   1.3x: fired constantly on completely ordinary frame-to-frame jitter
	//     ("gap=11ms expected=8ms", "gap=25ms expected=17ms") -- every one
	//     of those "stalls" replaced a perfectly good real frame with a
	//     lower-fidelity synthesized one, which is what actually caused the
	//     reported choppiness, not the network.
	//   1.8x + a 10ms floor: better, but this stream's own render-thread
	//     jitter sits close enough to that line ("gap=24ms expected=12ms",
	//     "gap=25ms expected=14ms") that it still fired repeatedly on
	//     nothing more than normal wobble.
	//   2.2x + a 15ms floor: fixed the false positives above, but then
	//     failed to trigger at all during a real loss/high-latency-packet
	//     test. The bug: a ratio >= 2.0 can mathematically NEVER catch a
	//     single genuinely dropped frame, at any framerate -- one dropped
	//     frame means the next one arrives after almost exactly 2x the
	//     expected interval, and 2x always fails a ">2.2x" test. 2.2x wasn't
	//     "conservative", it was "essentially never fires" for the most
	//     common real stall shape.
	// 1.9x (just under the 2x "one dropped frame" line) + a 13ms floor
	// reliably catches a single dropped/delayed frame at typical 30-120fps
	// while still clearing the healthy-jitter false positives above (see
	// the regression cases in frame_smoothing_test.go for the exact
	// pinned numbers from both failure modes).
	frameSmoothingTriggerRatio = 1.9

	// frameSmoothingMinOverageMs is a second, absolute-ms floor on top of
	// the ratio above: the trigger threshold is
	// max(expected*frameSmoothingTriggerRatio, expected+frameSmoothingMinOverageMs).
	// The ratio alone isn't enough at high framerates -- at expected=8ms
	// (~125fps, seen live on this direct-LAN test), even a generous ratio is
	// only a few ms of overage, well inside normal jitter. This floor
	// guarantees a meaningful absolute gap regardless of how small the
	// expected interval is. (At very high framerates this floor means a
	// single dropped frame there may not trigger concealment -- an
	// accepted tradeoff: a missed frame at 120fps+ is a ~8ms blip, both
	// hard to distinguish from noise and barely perceptible on its own.)
	frameSmoothingMinOverageMs = 13.0

	// frameSmoothingMinPlausibleIntervalMs: updateExpectedIntervalMs ignores
	// any measured interval below this as a bad sample, the same way it
	// already ignores <=0 -- live-tested and caught a real corrupted-baseline
	// case (app.log: "expected=3ms" right after connecting, from several
	// already-buffered frames flushing back-to-back as the decoder caught
	// up) that then falsely triggered concealment on the very next
	// completely ordinary frame. No real display pipeline legitimately
	// sustains beyond ~240fps, so intervals faster than that are burst/
	// startup artifacts, not the stream's actual steady-state cadence.
	frameSmoothingMinPlausibleIntervalMs = 4.0

	// frameSmoothingMinT is the smallest extrapolation factor ever reported
	// once concealment has triggered -- avoids an almost-zero warp that
	// would be visually indistinguishable from just re-presenting frame N
	// wastefully every render tick.
	frameSmoothingMinT = 0.1

	// frameSmoothingMaxConsecutive caps how many consecutive synthesized
	// frames get displayed after a stall before giving up and falling back
	// to today's behavior (freeze on the last real frame). Motion
	// extrapolation compounds error the further it's pushed forward, so on
	// a long stall it's better to stop guessing than to keep drifting
	// toward visible garbage. Also doubles as the extrapolation factor's
	// hard ceiling (t never exceeds this many expected-intervals forward).
	frameSmoothingMaxConsecutive = 4

	// frameSmoothingEMAAlpha weights how quickly the rolling expected
	// inter-frame interval reacts to a new real measured interval. Low
	// enough that a single stall/recovery blip doesn't itself corrupt the
	// baseline the next stall is measured against.
	frameSmoothingEMAAlpha = 0.2
)

var frameSmoothingEnabled atomic.Bool

// frameSmoothingSetEnabledHook mirrors ai_vision.go's aiVisionMetalPush/
// aiVisionMetalClear -- nil until a platform-specific init() wires it (see
// frame_smoothing_windows.go). Reading/calling through a nil hook never
// happens here (SetFrameSmoothingEnabled guards the call), same contract as
// the other platform hooks in this codebase.
var frameSmoothingSetEnabledHook func(enabled bool)

// SetFrameSmoothingEnabled turns the concealment fallback on or off. Wired
// to a checkbox in the video settings popup, next to AI Vision/Net Graph
// (see gui/view/video_start_dialog.go) -- takes effect immediately, default
// off (opt-in beta): unlike AI Vision there's no cached state to drop here,
// since a synthesized frame is only ever displayed transiently and the next
// real frame (or the disabled check itself) immediately supersedes it.
func SetFrameSmoothingEnabled(enabled bool) {
	frameSmoothingEnabled.Store(enabled)
	if hook := frameSmoothingSetEnabledHook; hook != nil {
		hook(enabled)
	}
}

// FrameSmoothingEnabled reports the checkbox's current state.
func FrameSmoothingEnabled() bool {
	return frameSmoothingEnabled.Load()
}

// updateExpectedIntervalMs folds one newly measured real inter-frame
// interval into the rolling EMA baseline used to decide what counts as
// "late". prevEma <= 0 means no baseline yet -- seed it directly with the
// first measurement rather than blending against nothing. newIntervalMs
// below frameSmoothingMinPlausibleIntervalMs (which includes <= 0) is
// treated as a bad/impossible sample -- either a duplicate timestamp or a
// burst of already-buffered frames flushing back-to-back (e.g. right after
// connecting) -- and left out of the average entirely, since a real
// display pipeline's steady-state cadence never legitimately gets that
// fast.
func updateExpectedIntervalMs(prevEma, newIntervalMs float64) float64 {
	if newIntervalMs < frameSmoothingMinPlausibleIntervalMs {
		return prevEma
	}
	if prevEma <= 0 {
		return newIntervalMs
	}
	return prevEma + frameSmoothingEMAAlpha*(newIntervalMs-prevEma)
}

// decideConcealment is called once per render tick while no new real frame
// has arrived yet. elapsedSinceRealMs is wall-clock time since the last
// real frame was presented; expectedIntervalMs is the current rolling
// baseline from updateExpectedIntervalMs; consecutiveSynthFrames is how
// many synthesized frames have already been presented for the current
// stall (0 on the first late tick). Returns whether to conceal this tick
// and, if so, how far forward (in units of expected-intervals past frame N)
// the warp/extrapolate compute pass should reach.
//
// Deliberately conservative: no baseline yet, a non-positive elapsed
// reading, or a stall that's already run past frameSmoothingMaxConsecutive
// synthesized frames all report "don't conceal" so the caller falls back to
// its existing behavior (presenting nothing new, i.e. the display stays on
// whatever was last drawn) rather than extrapolating indefinitely into an
// increasingly wrong guess.
func decideConcealment(elapsedSinceRealMs, expectedIntervalMs float64, consecutiveSynthFrames int) (conceal bool, extrapolateT float32) {
	if expectedIntervalMs <= 0 || elapsedSinceRealMs <= 0 {
		return false, 0
	}
	if consecutiveSynthFrames >= frameSmoothingMaxConsecutive {
		return false, 0
	}
	threshold := expectedIntervalMs * frameSmoothingTriggerRatio
	if floor := expectedIntervalMs + frameSmoothingMinOverageMs; floor > threshold {
		threshold = floor
	}
	if elapsedSinceRealMs < threshold {
		return false, 0
	}

	t := elapsedSinceRealMs/expectedIntervalMs - 1.0
	if t < frameSmoothingMinT {
		t = frameSmoothingMinT
	}
	if maxT := float64(frameSmoothingMaxConsecutive); t > maxT {
		t = maxT
	}
	return true, float32(t)
}
