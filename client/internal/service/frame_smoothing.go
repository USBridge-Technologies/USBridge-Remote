package service

import (
	"math"
	"sync/atomic"
)

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
	//
	// Lowered further to 1.65 (2026-09-19, explicit user tradeoff after
	// live telemetry -- see g_conceal_summary_stutter_concealed_to_concealed
	// in vk_video_impl_windows.c): measured presentation-gap stutters were
	// ALL landing at stall-boundary transitions (c2c=0 every window, i.e.
	// zero stutters were ever between two already-concealing ticks), which
	// traced directly to this threshold's own by-design wait -- every
	// single stall pays it before concealment engages at all. The 3
	// healthy-jitter regression cases below (12/24, 13/24, 14/25) now
	// intentionally DO conceal at this tighter threshold, accepting a
	// small risk of false-triggering on ordinary jitter again in exchange
	// for less of that wait. This tradeoff is more affordable now than
	// when 1.9x was originally tuned: concealment quality has since
	// improved substantially (continuous confidence-blend, per-block
	// temporal-prior tie-breaking, tie-break-toward-zero-motion), so a
	// false trigger on genuinely static/near-static content mostly just
	// re-displays something very close to the last real frame instead of
	// visibly smearing, which is what made false triggers costly before.
	frameSmoothingTriggerRatio = 1.65

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
	//
	// Lowered 13.0 -> 8.0 alongside frameSmoothingTriggerRatio above, same
	// 2026-09-19 tradeoff and same reasoning -- see that constant's doc
	// comment.
	frameSmoothingMinOverageMs = 8.0

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
	// to today's behavior (freeze on the last real frame). Each tick is
	// gated by the render thread's own ~8ms poll (WaitForSingleObject(g_event,
	// 8) in vk_render_thread, vk_video_impl_windows.c), so this is
	// approximately (this number * 8)ms of synthesis budget per stall.
	//
	// Was 4 (~32ms) -- far too short for a genuinely bad connection: live
	// telemetry (2026-09-19, frame smoothing summary log) on a sustained
	// bad-wifi connection showed real stalls (rawWanted in the PlayoutBuffer
	// log, i.e. jitter*3 before the buffer's own cap) routinely 150-190ms,
	// and a 4-tick budget only ever covers the first ~32ms of that -- the
	// user-visible result was concealment nominally "running" (~20-25% of
	// frames) but still freezing for the remaining ~120-160ms of nearly
	// every stall, which read as "no smoothness at all" despite concealment
	// technically triggering. 25 (~200ms) comfortably covers this
	// connection's observed stall lengths. This is safe to raise on its own
	// (independent of frameSmoothingMaxT below): each extra tick only
	// re-dispatches the cheap warp shader against the SAME cached flow
	// field (computed once per stall, not per tick) and t stays capped at
	// frameSmoothingMaxT regardless of how many ticks run, so a longer
	// budget means bridging longer gaps with the same bounded-confidence
	// guess repeated/held, not a riskier extrapolation.
	frameSmoothingMaxConsecutive = 25

	// frameSmoothingMaxT is the extrapolation factor's CONFIDENT ceiling,
	// independent of frameSmoothingMaxConsecutive above. These used to be
	// the same constant (t capped at 4.0x), which was too aggressive: a
	// single-level 16x16 block-match flow field is noisy on this app's
	// actual content (a remote desktop/device UI -- mostly static, sharp
	// UI edges, large flat regions, i.e. close to worst case for block
	// matching's aperture problem), and any per-block noise in that flow
	// gets stretched by extrapolateT before being displayed. Live testing
	// on a bad-wifi connection (2026-09-19, see app.log) showed the "смазано
	// вертикально ... картинка прыгает" (vertically smeared, jumping)
	// artifact specifically on concealed frames, worse the longer a stall
	// ran (i.e. the larger t got) -- consistent with noise amplification,
	// not just noise. Capping the reach lower bounds how far a bad guess
	// can be pushed.
	//
	// IMPORTANT: t does not hard-stop here once elapsed time pushes past
	// it -- see frameSmoothingSoftExtraT below. A hard stop was tried
	// first (raising frameSmoothingMaxConsecutive from 4 to 25 alone,
	// For dynamic/gaming content, extrapolating beyond 1.5x amplifies block-matching
	// errors (occlusions, perspective changes) into massive visual smears.
	// We cap confident extrapolation tightly to prevent "warp artifacts" in fast motion.
	frameSmoothingMaxT = 1.25

	// SoftExtraT allows a tiny bit of easing so the frame doesn't freeze completely,
	// but we keep it small to avoid stretching artifacts.
	frameSmoothingSoftExtraT = 0.25

	// frameSmoothingSoftDecayIntervals sets how quickly the soft-extra
	// creep above decays -- in units of expected-intervals of "excess"
	// elapsed time past frameSmoothingMaxT. At excess ==
	// frameSmoothingSoftDecayIntervals, ~63% (1 - 1/e) of
	// frameSmoothingSoftExtraT has been used; at 3x that, ~95%.
	frameSmoothingSoftDecayIntervals = 5.0

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
	// Clamp the interval to avoid network stalls polluting the EMA frame rate.
	// We expect 60fps (16.6ms) or 30fps (33.3ms). Anything over 35ms is a stall, not a slow framerate.
	if newIntervalMs > 35.0 {
		newIntervalMs = 35.0
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
	// We project 0.5 expected intervals into the future because the GPU/display
	// pipeline adds ~1 frame of latency, and we already waited 8ms (~0.5 frames).
	// This makes t exactly 1.0, 2.0, 3.0 for the missing frames.
	rawT := elapsedSinceRealMs/expectedIntervalMs + 0.5
	t := rawT
	if t > frameSmoothingMaxT {
		// Past the confident ceiling: keep easing forward (exponential
		// decay) instead of freezing at a fixed value -- see
		// frameSmoothingSoftExtraT's doc comment for why a hard stop here
		// produced a bit-for-bit static hold for the back half of any long
		// stall on a bad connection.
		excess := rawT - frameSmoothingMaxT
		t = frameSmoothingMaxT + frameSmoothingSoftExtraT*(1-math.Exp(-excess/frameSmoothingSoftDecayIntervals))
	}
	if t < frameSmoothingMinT {
		t = frameSmoothingMinT
	}
	return true, float32(t)
}
