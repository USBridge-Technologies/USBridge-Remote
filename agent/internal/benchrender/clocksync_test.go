package benchrender

import (
	"math"
	"math/rand"
	"testing"
)

// TestFitClockDrift: readings of a host clock 1.234 s ahead and 37 ppm fast, every
// 250 ms for 20 s, most with a few hundred us of one-sided queueing. The fit must
// land within 10 us of the true clock at the newest reading, where it is used.
func TestFitClockDrift(t *testing.T) {
	const off, drift = 1.234e6, 37e-6
	host := func(mono float64) float64 { return 1.8e15 + off + mono*(1+drift) }
	rng := rand.New(rand.NewSource(1))
	var reads []clockReading
	for i := 0; i < 80; i++ {
		mono := 5e6 + float64(i)*250e3
		rtt := 300.0
		if rng.Intn(3) > 0 {
			rtt += rng.Float64() * 2000
		}
		// The host read its clock somewhere in the round trip, not at the midpoint.
		skew := (rng.Float64() - 0.5) * (rtt - 280)
		reads = append(reads, clockReading{mono: mono, host: host(mono + skew), rtt: rtt})
	}
	a, b := fitClock(reads)
	last := reads[len(reads)-1].mono
	if e := math.Abs(a + b*last - host(last)); e > 10 {
		t.Fatalf("error %.1f us at the newest reading (drift fit %.2f ppm)", e, (b-1)*1e6)
	}
	if math.Abs((b-1)-drift) > 2e-6 {
		t.Fatalf("drift %.2f ppm, want 37", (b-1)*1e6)
	}
}
