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
	}{
		{"no baseline yet never conceals", 100, 0, 0, false},
		{"non-positive elapsed never conceals", 0, 16.6, 0, false},
		{"always conceals proactively", 16.6, 16.6, 0, true},
		{"far past expected interval conceals", 16.6 * 3.0, 16.6, 1, true},
		{"gives up past the cap", 16.6, 16.6, 30, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotConceal, _ := decideConcealment(tt.elapsedMs, tt.expectedMs, tt.consecutive)
			if gotConceal != tt.wantConceal {
				t.Errorf("decideConcealment(%v, %v, %v) conceal = %v, want %v", tt.elapsedMs, tt.expectedMs, tt.consecutive, gotConceal, tt.wantConceal)
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
