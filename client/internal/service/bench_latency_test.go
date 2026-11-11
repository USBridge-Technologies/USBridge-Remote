//go:build cgo && (linux || darwin || ios || windows || android)

package service

import (
	"math"
	"testing"
	"time"
)

// benchBarcodeRow draws the agent's barcode for value into a row of luma, the way
// agent/internal/benchrender lays it out on a 1920 wide canvas.
func benchBarcodeRow(width int, value uint32, lead int) []byte {
	row := make([]byte, width)
	for i := range row {
		row[i] = 40 // the scene's dark background
	}
	cell := float64(width-2*lead) / 37
	bits := []bool{true, false, true, false}
	for i := 0; i < 32; i++ {
		bits = append(bits, value>>(31-i)&1 == 1)
	}
	bits = append(bits, true)
	for i, white := range bits {
		v := byte(16)
		if white {
			v = 235
		}
		for x := lead + int(float64(i)*cell); x < lead+int(float64(i+1)*cell); x++ {
			row[x] = v
		}
	}
	return row
}

func TestBarcodeReadsAtAnySizeAndOffset(t *testing.T) {
	for _, c := range []struct{ width, lead int }{{1920, 220}, {1280, 147}, {2560, 293}, {3840, 440}, {2560, 500}, {640, 73}} {
		for _, v := range []uint32{0, 1, 0xFFFFFFFF, 0xA5A5A5A5, 123456789} {
			got, ok := benchDecodeBarcodeRow(benchBarcodeRow(c.width, v, c.lead), 1)
			if !ok || got != v {
				t.Errorf("width %d lead %d value %#x: got %#x ok=%v", c.width, c.lead, v, got, ok)
			}
		}
	}
}

// TestBarcodeReadsDimmedScreen: a target PC dimming its screen scales the whole
// signal (seen live: white cells at 150, the background at 23, through a KVM).
func TestBarcodeReadsDimmedScreen(t *testing.T) {
	for _, scale := range []float64{1, 0.6, 0.35} {
		row := benchBarcodeRow(1920, 0x5EED1234, 220)
		for i, v := range row {
			row[i] = byte(float64(v) * scale)
		}
		if got, ok := benchDecodeBarcodeRow(row, 1); !ok || got != 0x5EED1234 {
			t.Errorf("scale %.2f: got %#x ok=%v", scale, got, ok)
		}
	}
}

func TestBarcodeReadsP010(t *testing.T) {
	row := benchBarcodeRow(1920, 0xDEADBEEF, 220)
	wide := make([]byte, 2*len(row))
	for i, v := range row {
		wide[2*i+1] = v
	}
	if got, ok := benchDecodeBarcodeRow(wide, 2); !ok || got != 0xDEADBEEF {
		t.Fatalf("got %#x ok=%v", got, ok)
	}
}

func TestBarcodeRefusesOtherPictures(t *testing.T) {
	flat := make([]byte, 1920)
	for i := range flat {
		flat[i] = 40
	}
	if _, ok := benchDecodeBarcodeRow(flat, 1); ok {
		t.Error("read a barcode from a flat row")
	}
	for i := 800; i < 1000; i++ {
		flat[i] = 240 // a white window
	}
	if _, ok := benchDecodeBarcodeRow(flat, 1); ok {
		t.Error("read a barcode from a white rectangle")
	}
	row := benchBarcodeRow(1920, 7, 220)
	for x := 220; x < 260; x++ {
		row[x] = 40
	}
	if _, ok := benchDecodeBarcodeRow(row, 1); ok {
		t.Error("read a barcode with a broken guard")
	}
}

func TestLatencySeriesFollowsTheClockOffset(t *testing.T) {
	start := time.UnixMicro(1794159000000000)
	run := &BenchRun{StartedAt: start}
	// The host's clock is 5 s ahead at the start and has gained another 10 ms after 10 s.
	run.ClockSamples = []BenchClockSample{
		{WallUs: start.UnixMicro(), OffsetUs: 5_000_000},
		{WallUs: start.UnixMicro() + 10_000_000, OffsetUs: 5_010_000},
	}
	barAt := func(hostUs int64) uint32 { return uint32(hostUs / 100) }
	// Half way: offset 5.005 s; the picture was drawn 30 ms before it was decoded here.
	wall := start.UnixMicro() + 5_000_000
	run.Latency = []BenchLatencySample{
		{WallUs: wall, Bar: barAt(wall + 5_005_000 - 30_000)},
		// A misread barcode, days off: dropped.
		{WallUs: wall, Bar: barAt(wall + 5_005_000 - 86_400_000_000)},
		// From the future: dropped.
		{WallUs: wall, Bar: barAt(wall + 5_005_000 + 500_000)},
	}
	pts := BenchLatencySeries(run)
	if len(pts) != 1 {
		t.Fatalf("%d points: %v", len(pts), pts)
	}
	if math.Abs(pts[0][1]-30) > 0.2 || math.Abs(pts[0][0]-5) > 0.001 {
		t.Fatalf("point %v", pts[0])
	}
	m := AnalyzeBenchRun(run)
	if m.LatencySamples != 1 || math.Abs(m.LatencyP50-30) > 0.2 {
		t.Fatalf("metrics: %d %f", m.LatencySamples, m.LatencyP50)
	}
}

func TestLatencySeriesNeedsAClock(t *testing.T) {
	run := &BenchRun{Latency: []BenchLatencySample{{WallUs: 1, Bar: 1}}}
	if pts := BenchLatencySeries(run); pts != nil {
		t.Fatalf("%v", pts)
	}
}
