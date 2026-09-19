package service

import "testing"

// resetFrameSmoothingState mirrors resetAIVisionState (ai_vision_test.go) --
// this file's only process-wide state is the enabled flag.
func resetFrameSmoothingState(t *testing.T) {
	t.Helper()
	frameSmoothingEnabled.Store(false)
	t.Cleanup(func() { frameSmoothingEnabled.Store(false) })
}

func TestSetFrameSmoothingEnabledTogglesState(t *testing.T) {
	resetFrameSmoothingState(t)

	if FrameSmoothingEnabled() {
		t.Fatal("FrameSmoothingEnabled() should start false")
	}
	SetFrameSmoothingEnabled(true)
	if !FrameSmoothingEnabled() {
		t.Fatal("FrameSmoothingEnabled() = false after SetFrameSmoothingEnabled(true)")
	}
	SetFrameSmoothingEnabled(false)
	if FrameSmoothingEnabled() {
		t.Fatal("FrameSmoothingEnabled() = true after SetFrameSmoothingEnabled(false)")
	}
}

func TestUpdateExpectedIntervalMs(t *testing.T) {
	tests := []struct {
		name           string
		prevEma        float64
		newIntervalMs  float64
		wantExactValue float64 // used when prevEma<=0 (seed) or newIntervalMs<=0 (unchanged)
		wantExact      bool
	}{
		{"seeds baseline from first measurement", 0, 16.6, 16.6, true},
		{"seeds baseline when prevEma negative (uninitialized sentinel)", -1, 16.6, 16.6, true},
		{"rejects zero interval, keeps prior baseline", 16.6, 0, 16.6, true},
		{"rejects negative interval, keeps prior baseline", 16.6, -5, 16.6, true},
		// Regression: pinned from a real direct-LAN session where several
		// already-buffered frames flushed back-to-back right after
		// connecting produced a ~3ms "interval" (see app.log:
		// "concealing a stall (gap=20ms expected=3ms t=4.00)") -- a
		// baseline that low then falsely flagged the very next ordinary
		// frame as a stall. No real steady-state stream sustains beyond
		// ~240fps (~4.16ms), so anything faster must be rejected the same
		// as a zero/negative sample.
		{"rejects implausibly fast burst sample, keeps prior baseline", 16.6, 3.0, 16.6, true},
		{"accepts a sample right at the plausibility floor", 0, frameSmoothingMinPlausibleIntervalMs, frameSmoothingMinPlausibleIntervalMs, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateExpectedIntervalMs(tt.prevEma, tt.newIntervalMs)
			if tt.wantExact && got != tt.wantExactValue {
				t.Errorf("updateExpectedIntervalMs(%v, %v) = %v, want %v", tt.prevEma, tt.newIntervalMs, got, tt.wantExactValue)
			}
		})
	}

	// Converges toward a new steady interval without ever overshooting past
	// it or snapping instantly (EMA, not a raw replace).
	ema := 16.6
	for i := 0; i < 50; i++ {
		ema = updateExpectedIntervalMs(ema, 33.3)
	}
	if ema < 32.0 || ema > 33.3 {
		t.Errorf("EMA after 50 steady 33.3ms samples = %v, want converged near 33.3 (within [32.0, 33.3])", ema)
	}

	// A single blip must not corrupt the baseline much.
	steady := 16.6
	for i := 0; i < 20; i++ {
		steady = updateExpectedIntervalMs(steady, 16.6)
	}
	afterBlip := updateExpectedIntervalMs(steady, 200.0) // one bad sample (e.g. a stall)
	if afterBlip <= steady {
		t.Fatalf("one high sample should nudge EMA upward, got %v from baseline %v", afterBlip, steady)
	}
	if afterBlip > steady+(200.0-steady)*frameSmoothingEMAAlpha+0.001 {
		t.Errorf("single blip moved EMA further than one alpha-weighted step: %v -> %v", steady, afterBlip)
	}
}

func TestDecideConcealment(t *testing.T) {
	tests := []struct {
		name               string
		elapsedMs          float64
		expectedMs         float64
		consecutive        int
		wantConceal        bool
		wantTMin, wantTMax float32
		checkTBounds       bool
	}{
		{
			name: "no baseline yet never conceals", elapsedMs: 100, expectedMs: 0, consecutive: 0,
			wantConceal: false,
		},
		{
			name: "non-positive elapsed never conceals", elapsedMs: 0, expectedMs: 16.6, consecutive: 0,
			wantConceal: false,
		},
		{
			name:      "within normal jitter (below trigger ratio) does not conceal",
			elapsedMs: 16.6 * 1.2, expectedMs: 16.6, consecutive: 0,
			wantConceal: false,
		},
		{
			// Just below max(expected*ratio, expected+floor) at 16.6ms
			// (~60fps, where the ratio term dominates -- see the table
			// below) -- a robust "does not conceal" boundary check, not
			// reliant on exact floating-point equality against the
			// threshold itself.
			name:      "just below the trigger threshold does not conceal",
			elapsedMs: 16.6*frameSmoothingTriggerRatio - 0.5, expectedMs: 16.6, consecutive: 0,
			wantConceal: false,
		},
		{
			name:      "just past the trigger threshold conceals with a small t",
			elapsedMs: 16.6*frameSmoothingTriggerRatio + 0.5, expectedMs: 16.6, consecutive: 0,
			wantConceal: true, checkTBounds: true, wantTMin: frameSmoothingMinT, wantTMax: 1.5,
		},
		{
			name:      "far past expected interval conceals with a larger t",
			elapsedMs: 16.6 * 3.0, expectedMs: 16.6, consecutive: 1,
			wantConceal: true, checkTBounds: true, wantTMin: 1.5, wantTMax: float32(frameSmoothingMaxConsecutive),
		},
		// Regression cases pinned from a real direct-LAN test session
		// (see app.log): at a high, healthy framerate (expected~8ms) and at
		// a typical 60fps-ish one (expected~17ms), ordinary jitter kept
		// tripping the OLD 1.3x-only threshold ("concealing a stall
		// (gap=11ms expected=8ms ...)", "(gap=25ms expected=17ms ...)"),
		// constantly swapping good real frames for lower-fidelity
		// synthesized ones -- which was the actual cause of the reported
		// choppiness, not the network. Neither of these must conceal.
		{
			name:      "regression: healthy high-fps jitter (8ms/11ms) does not conceal",
			elapsedMs: 11, expectedMs: 8, consecutive: 0,
			wantConceal: false,
		},
		{
			name:      "regression: healthy ~60fps jitter (17ms/25ms) does not conceal",
			elapsedMs: 25, expectedMs: 17, consecutive: 0,
			wantConceal: false,
		},
		// A second live-test round with the 1.8x/10ms tuning (since
		// replaced by the 2.2x/15ms values above) still misfired on this
		// stream's own render-thread jitter sitting close to that line --
		// pinned as regressions against the current, more conservative
		// tuning too.
		{
			name:      "regression: 12ms/24ms jitter does not conceal",
			elapsedMs: 24, expectedMs: 12, consecutive: 0,
			wantConceal: false,
		},
		{
			name:      "regression: 13ms/24ms jitter does not conceal",
			elapsedMs: 24, expectedMs: 13, consecutive: 0,
			wantConceal: false,
		},
		{
			name:      "regression: 14ms/25ms jitter does not conceal",
			elapsedMs: 25, expectedMs: 14, consecutive: 0,
			wantConceal: false,
		},
		{
			// A genuine multi-frame gap at 60fps (comfortably past the
			// tuned threshold) must still conceal -- the fix above must not
			// have swung so far the other way that real stalls stop
			// triggering.
			name:      "a genuine multi-frame gap at 60fps still conceals",
			elapsedMs: 45, expectedMs: 16.6, consecutive: 0,
			wantConceal: true, checkTBounds: true, wantTMin: frameSmoothingMinT, wantTMax: float32(frameSmoothingMaxConsecutive),
		},
		// Regression for the 2.2x-ratio bug: a ratio >= 2.0 can mathematically
		// never catch a SINGLE dropped frame (gap = ~2x expected, always <
		// a >2x threshold) at any framerate -- live-tested and found to
		// suppress concealment entirely during a real packet-loss/high-
		// latency test. These pin "one frame lost" (gap = ~2x expected) as
		// a trigger at both a typical 30fps and 60fps cadence.
		{
			name:      "a single dropped frame at 60fps (gap=2x expected) conceals",
			elapsedMs: 16.6 * 2.0, expectedMs: 16.6, consecutive: 0,
			wantConceal: true, checkTBounds: true, wantTMin: frameSmoothingMinT, wantTMax: float32(frameSmoothingMaxConsecutive),
		},
		{
			name:      "a single dropped frame at 30fps (gap=2x expected) conceals",
			elapsedMs: 33.3 * 2.0, expectedMs: 33.3, consecutive: 0,
			wantConceal: true, checkTBounds: true, wantTMin: frameSmoothingMinT, wantTMax: float32(frameSmoothingMaxConsecutive),
		},
		{
			name:      "t is capped at frameSmoothingMaxConsecutive even for an extreme gap",
			elapsedMs: 16.6 * 50.0, expectedMs: 16.6, consecutive: 1,
			wantConceal: true, checkTBounds: true,
			wantTMin: float32(frameSmoothingMaxConsecutive), wantTMax: float32(frameSmoothingMaxConsecutive),
		},
		{
			name:      "gives up once consecutiveSynthFrames reaches the cap",
			elapsedMs: 16.6 * 3.0, expectedMs: 16.6, consecutive: frameSmoothingMaxConsecutive,
			wantConceal: false,
		},
		{
			name:      "gives up past the cap too",
			elapsedMs: 16.6 * 3.0, expectedMs: 16.6, consecutive: frameSmoothingMaxConsecutive + 5,
			wantConceal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conceal, extrapolateT := decideConcealment(tt.elapsedMs, tt.expectedMs, tt.consecutive)
			if conceal != tt.wantConceal {
				t.Fatalf("decideConcealment(%v, %v, %d) conceal = %v, want %v",
					tt.elapsedMs, tt.expectedMs, tt.consecutive, conceal, tt.wantConceal)
			}
			if !conceal {
				if extrapolateT != 0 {
					t.Errorf("extrapolateT = %v when not concealing, want 0", extrapolateT)
				}
				return
			}
			if tt.checkTBounds && (extrapolateT < tt.wantTMin || extrapolateT > tt.wantTMax) {
				t.Errorf("extrapolateT = %v, want within [%v, %v]", extrapolateT, tt.wantTMin, tt.wantTMax)
			}
			if extrapolateT > float32(frameSmoothingMaxConsecutive) {
				t.Errorf("extrapolateT = %v exceeds hard cap %v", extrapolateT, frameSmoothingMaxConsecutive)
			}
			if extrapolateT < frameSmoothingMinT {
				t.Errorf("extrapolateT = %v below floor %v", extrapolateT, frameSmoothingMinT)
			}
		})
	}
}

// TestDecideConcealmentMonotonicInElapsed pins the "ramps up" behavior the
// architecture doc promises: for a fixed expected interval and consecutive
// count, a larger gap since the last real frame must never produce a
// smaller extrapolation factor than a shorter gap did.
func TestDecideConcealmentMonotonicInElapsed(t *testing.T) {
	const expected = 16.6
	prevT := float32(0)
	for _, elapsed := range []float64{22, 25, 30, 40, 60, 100, 200} {
		conceal, tVal := decideConcealment(elapsed, expected, 0)
		if !conceal {
			continue
		}
		if tVal < prevT {
			t.Errorf("extrapolateT decreased as elapsed grew: elapsed=%v t=%v, previous t=%v", elapsed, tVal, prevT)
		}
		prevT = tVal
	}
}
