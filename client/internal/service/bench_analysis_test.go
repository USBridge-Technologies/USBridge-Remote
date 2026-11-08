package service

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// synthBenchRun builds a 60fps run of dur seconds with a few injected
// events at the given frame indexes.
func synthBenchRun(backend string, dur int, hostStallAt, netStallAt, lossAt int) *BenchRun {
	const frameUs = 16667
	run := &BenchRun{Backend: backend, ExpectedFPS: 60, StartUs: 1_000_000, SwitchMs: 3000, StartupMs: 1500}
	submit := run.StartUs + 1000
	pts := uint64(50_000_000)
	num := int32(100)
	n := dur * 60
	for i := 0; i < n; i++ {
		ptsStep, arriveStep := uint64(frameUs), uint64(frameUs)
		switch i {
		case hostStallAt: // host captured nothing for 200ms
			ptsStep, arriveStep = 200_000, 200_000
		case netStallAt: // host on time, delivery held back 150ms
			arriveStep = 150_000
		case netStallAt + 1:
			arriveStep = 1000
		case lossAt: // 5 frames lost, recovered with an IDR 120ms later
			num += 5
			ptsStep, arriveStep = 6*frameUs, 120_000
		}
		if i > 0 {
			pts += ptsStep
			submit += arriveStep
			num++
		}
		run.Frames = append(run.Frames, BenchFrame{
			Number: num, IDR: i == lossAt, Size: 20000, HostLatencyMs: 4,
			ReceiveUs: submit - 3000, EnqueueUs: submit - 1000, SubmitUs: submit, PtsUs: pts,
		})
	}
	run.EndUs = submit + frameUs
	for t := run.StartUs; t < run.EndUs; t += 100_000 {
		run.Ticks = append(run.Ticks, BenchTick{AtUs: t, RTTMs: 3, RTTValid: true, PacketsVideo: 100, Rendered: 6, RenderValid: true})
	}
	return run
}

func TestAnalyzeBenchRunClassifiesStalls(t *testing.T) {
	run := synthBenchRun("rustshine", 10, 120, 300, 480)
	m := AnalyzeBenchRun(run)
	if m.StallCount != 3 {
		t.Fatalf("stalls = %d, want 3: %+v", m.StallCount, m.Stalls)
	}
	if m.StallHost != 1 || m.StallNetwork != 1 || m.StallLoss != 1 {
		t.Fatalf("causes host=%d network=%d loss=%d, want 1/1/1: %+v", m.StallHost, m.StallNetwork, m.StallLoss, m.Stalls)
	}
	if m.LossEvents != 1 || m.LostFrames != 5 || m.RecoveredByIDR != 1 {
		t.Fatalf("loss events=%d lost=%d idr=%d", m.LossEvents, m.LostFrames, m.RecoveredByIDR)
	}
	if m.RecoveryMaxMs < 119 || m.RecoveryMaxMs > 121 {
		t.Fatalf("recovery max = %.1fms, want ~120", m.RecoveryMaxMs)
	}
	if !m.HostTimingOK || m.HostLatencyAvg != 4 {
		t.Fatalf("host timing ok=%v avg=%.1f", m.HostTimingOK, m.HostLatencyAvg)
	}
	if m.AvgFPS < 55 || m.AvgFPS > 61 {
		t.Fatalf("avg fps = %.1f", m.AvgFPS)
	}
	if m.NetJitterP95 > 1 {
		t.Fatalf("net jitter p95 = %.2f, want ~0 on a clean run", m.NetJitterP95)
	}
}

func TestAnalyzeBenchRunWithoutHostTimestamps(t *testing.T) {
	run := synthBenchRun("sunshine", 5, -1, 100, -1)
	for i := range run.Frames {
		run.Frames[i].PtsUs = 0
	}
	m := AnalyzeBenchRun(run)
	if m.HostTimingOK {
		t.Fatal("zero timestamps reported as usable")
	}
	if m.StallCount != 1 || m.StallNetwork != 1 {
		t.Fatalf("stalls %+v", m.Stalls)
	}
}

func TestRenderBenchChart(t *testing.T) {
	runs := []*BenchRun{synthBenchRun("sunshine", 30, 600, 900, 1500), synthBenchRun("rustshine", 30, -1, 1200, -1)}
	var metrics []BenchMetrics
	for _, r := range runs {
		metrics = append(metrics, AnalyzeBenchRun(r))
	}
	img := RenderBenchChart(runs, metrics)
	if img.Bounds().Dx() != benchChartW || img.Bounds().Dy() < 500 {
		t.Fatalf("chart size %v", img.Bounds())
	}
	if dir := os.Getenv("BENCH_CHART_OUT"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "bench_chart_test.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnalyzeBenchRunAveragesHostLoad(t *testing.T) {
	run := &BenchRun{HostLoad: []BenchLoad{
		{CPU: 20, StreamerCPU: 1, GPU: map[string]float64{"3d": 30, "encode": 20}, StreamerGPU: map[string]float64{"3d": 10, "encode": 20}},
		{CPU: 40, StreamerCPU: 3, GPU: map[string]float64{"3d": 50, "codec": 40}, StreamerGPU: map[string]float64{"3d": 30, "codec": 40}},
	}}
	m := AnalyzeBenchRun(run)
	if !m.HostLoadValid || m.HostCPUAvg != 30 || m.StreamerCPUAvg != 2 || m.GPU3DAvg != 40 || m.Streamer3DAvg != 20 {
		t.Fatalf("cpu/3d averages: %+v", m)
	}
	// AMD's shared "codec" engine counts as encode and decode.
	if m.StreamerEncodeAvg != 30 || m.StreamerDecodeAvg != 20 {
		t.Fatalf("encode %v decode %v", m.StreamerEncodeAvg, m.StreamerDecodeAvg)
	}
	if AnalyzeBenchRun(&BenchRun{}).HostLoadValid {
		t.Fatal("no samples must not be valid")
	}
}

func TestSizeDiffersOnlyForAKnownOtherSize(t *testing.T) {
	cases := []struct {
		m    BenchMetrics
		want bool
	}{
		{BenchMetrics{WantWidth: 3840, WantHeight: 2160, StreamWidth: 1920, StreamHeight: 1080}, true},
		{BenchMetrics{WantWidth: 3840, WantHeight: 2160, StreamWidth: 3840, StreamHeight: 2160}, false},
		{BenchMetrics{WantWidth: 3840, WantHeight: 2160}, false}, // never sampled
		{BenchMetrics{StreamWidth: 1920, StreamHeight: 1080}, false},
	}
	for _, c := range cases {
		if got := c.m.SizeDiffers(); got != c.want {
			t.Errorf("%+v: %v", c.m, got)
		}
	}
}
