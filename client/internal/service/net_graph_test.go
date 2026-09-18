package service

import (
	"sync/atomic"
	"testing"
)

// resetNetGraphState clears every Net Graph package-level var before (and
// restores it after) a test -- same rationale as ai_vision_test.go's
// resetAIVisionState: this package's state is process-wide, so tests must
// not leak it into each other.
func resetNetGraphState(t *testing.T) {
	t.Helper()
	savedClear := netGraphMetalClear
	clearState := func() {
		netGraphEnabled.Store(false)
		netGraphMu.Lock()
		netGraphSamples = nil
		netGraphMu.Unlock()
		netGraphPrevMu.Lock()
		netGraphPrevRaw = netGraphRawNetworkStats{}
		netGraphPrevValid = false
		netGraphPrevMu.Unlock()
	}
	clearState()
	t.Cleanup(func() {
		clearState()
		netGraphMetalClear = savedClear
	})
}

func TestSetNetGraphEnabledTogglesState(t *testing.T) {
	resetNetGraphState(t)

	if NetGraphEnabled() {
		t.Fatal("NetGraphEnabled() should start false")
	}
	SetNetGraphEnabled(true)
	if !NetGraphEnabled() {
		t.Fatal("NetGraphEnabled() = false after SetNetGraphEnabled(true)")
	}
	SetNetGraphEnabled(false)
	if NetGraphEnabled() {
		t.Fatal("NetGraphEnabled() = true after SetNetGraphEnabled(false)")
	}
}

// TestSetNetGraphEnabledFalseDropsSamplesAndClearsMetal pins the two side
// effects turning the checkbox off must have: the cached sample ring buffer
// is dropped (so a stale HUD image can't be rebuilt from old data) and the
// native Metal compositor HUD layer is cleared via netGraphMetalClear.
func TestSetNetGraphEnabledFalseDropsSamplesAndClearsMetal(t *testing.T) {
	resetNetGraphState(t)

	netGraphMu.Lock()
	netGraphSamples = []NetGraphSample{{RTTMs: 20, RTTValid: true}}
	netGraphMu.Unlock()

	var cleared int32
	netGraphMetalClear = func() { atomic.AddInt32(&cleared, 1) }

	SetNetGraphEnabled(true)
	SetNetGraphEnabled(false)

	netGraphMu.Lock()
	samples := netGraphSamples
	netGraphMu.Unlock()
	if samples != nil {
		t.Error("disabling Net Graph must drop the cached sample ring buffer")
	}
	if atomic.LoadInt32(&cleared) != 1 {
		t.Errorf("disabling Net Graph must call netGraphMetalClear once, got %d calls", cleared)
	}
}

// TestSetNetGraphEnabledResetsBaseline pins the fix for the "re-enabling
// after a while causes a bogus loss/FEC spike" case described in
// SetNetGraphEnabled's doc comment: re-enabling must force
// netGraphPrevValid back to false so the next sample doesn't diff against
// a stale baseline.
func TestSetNetGraphEnabledResetsBaseline(t *testing.T) {
	resetNetGraphState(t)

	netGraphPrevMu.Lock()
	netGraphPrevRaw = netGraphRawNetworkStats{PacketCountVideo: 1000}
	netGraphPrevValid = true
	netGraphPrevMu.Unlock()

	SetNetGraphEnabled(true)

	netGraphPrevMu.Lock()
	valid := netGraphPrevValid
	netGraphPrevMu.Unlock()
	if valid {
		t.Error("SetNetGraphEnabled(true) must reset netGraphPrevValid so the next tick establishes a fresh baseline")
	}
}

func TestNetGraphDeltaU32(t *testing.T) {
	cases := []struct {
		cur, prev, want uint32
	}{
		{10, 5, 5},
		{5, 5, 0},
		{5, 10, 0}, // counter reset (new session) -- must clamp, not wrap to ~4 billion
		{0, 0, 0},
	}
	for _, c := range cases {
		if got := netGraphDeltaU32(c.cur, c.prev); got != c.want {
			t.Errorf("netGraphDeltaU32(%d, %d) = %d, want %d", c.cur, c.prev, got, c.want)
		}
	}
}

// TestCollectNetGraphSampleFirstTickHasNoDeltas pins that the very first
// sample after a fresh baseline (hadPrev == false) reports zero packet
// deltas rather than diffing against a zero-valued netGraphRawNetworkStats
// and reporting a bogus one-time spike equal to the host's entire
// cumulative counters.
func TestCollectNetGraphSampleFirstTickHasNoDeltas(t *testing.T) {
	resetNetGraphState(t)

	savedFn := netGraphNetworkStatsFn
	t.Cleanup(func() { netGraphNetworkStatsFn = savedFn })
	netGraphNetworkStatsFn = func() netGraphRawNetworkStats {
		return netGraphRawNetworkStats{
			PacketCountVideo: 5000,
			PacketCountFec:   500,
			RTTMs:            15,
			RTTValid:         true,
		}
	}

	first := collectNetGraphSample()
	if first.PacketsVideo != 0 || first.PacketsFec != 0 {
		t.Errorf("first sample after a fresh baseline must have zero deltas, got PacketsVideo=%d PacketsFec=%d", first.PacketsVideo, first.PacketsFec)
	}
	if !first.RTTValid || first.RTTMs != 15 {
		t.Errorf("first sample must still report the current RTT, got valid=%v ms=%v", first.RTTValid, first.RTTMs)
	}

	netGraphNetworkStatsFn = func() netGraphRawNetworkStats {
		return netGraphRawNetworkStats{
			PacketCountVideo: 5010,
			PacketCountFec:   505,
			RTTMs:            16,
			RTTValid:         true,
		}
	}
	second := collectNetGraphSample()
	if second.PacketsVideo != 10 || second.PacketsFec != 5 {
		t.Errorf("second sample deltas = video=%d fec=%d, want video=10 fec=5", second.PacketsVideo, second.PacketsFec)
	}
}

func TestBuildNetGraphHUDSizeAndEmptyInput(t *testing.T) {
	img := buildNetGraphHUD(nil)
	if img == nil {
		t.Fatal("buildNetGraphHUD(nil) must not return nil")
	}
	if img.Bounds().Dx() != netGraphCanvasW || img.Bounds().Dy() != netGraphCanvasH {
		t.Errorf("buildNetGraphHUD canvas = %dx%d, want %dx%d", img.Bounds().Dx(), img.Bounds().Dy(), netGraphCanvasW, netGraphCanvasH)
	}
}

// TestBuildNetGraphHUDLossEventIsVisible pins the requirement driving
// netGraphDrawEventGraph's "minimum visible pixels" design: even a single
// FEC-failed packet in one tick must paint at least one bad-colored pixel
// in the event graph's column, not round down to invisible.
func TestBuildNetGraphHUDLossEventIsVisible(t *testing.T) {
	samples := make([]NetGraphSample, 0, 10)
	for i := 0; i < 9; i++ {
		samples = append(samples, NetGraphSample{RTTValid: true, RTTMs: 10})
	}
	samples = append(samples, NetGraphSample{RTTValid: true, RTTMs: 10, FecFailed: 1})

	img := buildNetGraphHUD(samples)

	found := false
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c == netGraphBad {
				found = true
			}
		}
	}
	if !found {
		t.Error("a single FecFailed event in the sample history must paint at least one netGraphBad pixel")
	}
}
