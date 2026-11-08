package service

import (
	"math"
	"sort"
)

// Streamer benchmark analysis: turns a BenchRun into metrics grouped the
// way the comparison needs them -- smoothness as the viewer saw it, then
// separately the host (capture cadence + encode time), the network, and
// how the stream recovered from lost frames -- so a difference between two
// streamers can be pinned on the part that actually caused it.

// Stall causes.
const (
	BenchCauseLoss    = "loss"    // frames never arrived (lost or dropped awaiting recovery)
	BenchCauseHost    = "host"    // the host captured/encoded nothing for that long
	BenchCauseNetwork = "network" // the host produced frames on time, they arrived late
)

// BenchStall is one visible freeze: a gap between two consecutive frames
// handed to the decoder that is long enough to see.
type BenchStall struct {
	AtMs       float64 `json:"at_ms"` // since the start of the measurement window
	DurationMs float64 `json:"duration_ms"`
	Cause      string  `json:"cause"`
	LostFrames int     `json:"lost_frames,omitempty"`
	// Recovery is how a loss was repaired: "idr" (full keyframe) or "rfi"
	// (reference frame invalidation: the next frame was a P-frame).
	Recovery string `json:"recovery,omitempty"`
	// HostGapMs is the host's own capture-time gap across the stall.
	HostGapMs float64 `json:"host_gap_ms,omitempty"`
}

// BenchMetrics is the summary of one run.
type BenchMetrics struct {
	Backend     string  `json:"backend"`
	Codec       string  `json:"codec,omitempty"`
	DurationSec float64 `json:"duration_sec"`
	SwitchMs    float64 `json:"switch_ms"`
	StopMs      float64 `json:"stop_ms,omitempty"`
	StartMs     float64 `json:"start_ms,omitempty"`
	StartupMs   float64 `json:"startup_ms"`

	// Smoothness (frames handed to the decoder).
	Frames        int     `json:"frames"`
	AvgFPS        float64 `json:"avg_fps"`
	Low1FPS       float64 `json:"low1_fps"` // 1% low: 1000 / 99th percentile frame interval
	IntervalP50   float64 `json:"interval_p50_ms"`
	IntervalP95   float64 `json:"interval_p95_ms"`
	IntervalP99   float64 `json:"interval_p99_ms"`
	IntervalMax   float64 `json:"interval_max_ms"`
	IntervalStd   float64 `json:"interval_std_ms"`
	Hitches       int     `json:"hitches"` // > 2 frame times, below the stall threshold
	StallCount    int     `json:"stall_count"`
	StallTotalMs  float64 `json:"stall_total_ms"`
	StallHost     int     `json:"stall_host"`
	StallNetwork  int     `json:"stall_network"`
	StallLoss     int     `json:"stall_loss"`
	RenderFPS     float64 `json:"render_fps"`
	RenderValid   bool    `json:"render_valid"`
	StallThreshMs float64 `json:"stall_threshold_ms"`

	// Host: capture cadence from the host's own timestamps, encode time
	// from the per-frame header.
	// StreamWidth/StreamHeight differ from WantWidth/WantHeight when SizeDiffers: the
	// run's figures are for another picture and don't compare with the others'.
	WantWidth    int `json:"want_width,omitempty"`
	WantHeight   int `json:"want_height,omitempty"`
	StreamWidth  int `json:"stream_width,omitempty"`
	StreamHeight int `json:"stream_height,omitempty"`

	HostLatencyAvg float64 `json:"host_latency_avg_ms"`
	HostLatencyP95 float64 `json:"host_latency_p95_ms"`
	HostLatencyMax float64 `json:"host_latency_max_ms"`
	HostCadenceStd float64 `json:"host_cadence_std_ms"`
	HostCadenceMax float64 `json:"host_cadence_max_ms"`
	HostFPS        float64 `json:"host_fps"`
	HostTimingOK   bool    `json:"host_timing_ok"`
	BitrateMbps    float64 `json:"bitrate_mbps"`
	IDRFrames      int     `json:"idr_frames"`

	// Host load averages over the window (percent; CPU of all cores, GPU
	// per engine type). HostLoadValid is false without samples.
	HostLoadValid     bool    `json:"host_load_valid"`
	HostCPUAvg        float64 `json:"host_cpu_avg_pct"`
	StreamerCPUAvg    float64 `json:"streamer_cpu_avg_pct"`
	GPU3DAvg          float64 `json:"gpu_3d_avg_pct"`
	GPUEncodeAvg      float64 `json:"gpu_encode_avg_pct"`
	GPUDecodeAvg      float64 `json:"gpu_decode_avg_pct"`
	Streamer3DAvg     float64 `json:"streamer_gpu_3d_avg_pct"`
	StreamerEncodeAvg float64 `json:"streamer_gpu_encode_avg_pct"`
	StreamerDecodeAvg float64 `json:"streamer_gpu_decode_avg_pct"`

	// From the host drawing a picture to this client having it decoded, by the time
	// barcode in sampled frames. LatencySamples is 0 without it.
	LatencySamples int     `json:"latency_samples,omitempty"`
	LatencyP50     float64 `json:"latency_p50_ms,omitempty"`
	LatencyP95     float64 `json:"latency_p95_ms,omitempty"`
	LatencyP99     float64 `json:"latency_p99_ms,omitempty"`

	// Network.
	RTTAvg          float64 `json:"rtt_avg_ms"`
	RTTMax          float64 `json:"rtt_max_ms"`
	NetJitterAvg    float64 `json:"net_jitter_avg_ms"` // |Δarrival − Δcapture| per frame
	NetJitterP95    float64 `json:"net_jitter_p95_ms"`
	TransferAvg     float64 `json:"transfer_avg_ms"` // first packet → frame complete
	TransferP95     float64 `json:"transfer_p95_ms"`
	PacketLossPct   float64 `json:"packet_loss_pct"`
	FecRecovered    uint64  `json:"fec_recovered"`
	FecFailed       uint64  `json:"fec_failed"`
	PlayoutDelayAvg float64 `json:"playout_delay_avg_ms"`

	// Recovery from lost frames.
	LossEvents     int     `json:"loss_events"`
	LostFrames     int     `json:"lost_frames"`
	RecoveryAvgMs  float64 `json:"recovery_avg_ms"`
	RecoveryMaxMs  float64 `json:"recovery_max_ms"`
	RecoveredByIDR int     `json:"recovered_by_idr"`
	RecoveredByRFI int     `json:"recovered_by_rfi"`

	Stalls []BenchStall `json:"stalls"`
	Error  string       `json:"error,omitempty"`
}

// BenchFrameInterval is one frame's gap to its predecessor, for charts.
type BenchFrameInterval struct {
	AtMs       float64
	IntervalMs float64
	HostGapMs  float64 // NaN when host timestamps are unusable
	HostMs     float64
	NetJitter  float64 // NaN when host timestamps are unusable
	Lost       int
}

// BenchIntervals lists every frame-to-frame gap of the run.
func BenchIntervals(run *BenchRun) []BenchFrameInterval {
	if run == nil || len(run.Frames) < 2 {
		return nil
	}
	ptsOK := benchHostTimingUsable(run.Frames)
	out := make([]BenchFrameInterval, 0, len(run.Frames)-1)
	for i := 1; i < len(run.Frames); i++ {
		a, b := run.Frames[i-1], run.Frames[i]
		iv := BenchFrameInterval{
			AtMs:       benchUsToMs(b.SubmitUs - run.StartUs),
			IntervalMs: benchUsDiffMs(b.SubmitUs, a.SubmitUs),
			HostGapMs:  math.NaN(),
			NetJitter:  math.NaN(),
			HostMs:     b.HostLatencyMs,
			Lost:       benchLostBetween(a, b),
		}
		if ptsOK && b.PtsUs > a.PtsUs {
			iv.HostGapMs = benchUsDiffMs(b.PtsUs, a.PtsUs)
			iv.NetJitter = math.Abs(benchUsDiffMs(b.EnqueueUs, a.EnqueueUs) - iv.HostGapMs)
		}
		out = append(out, iv)
	}
	return out
}

// SizeDiffers reports a stream of another size than the one asked for.
func (m BenchMetrics) SizeDiffers() bool {
	return m.WantWidth > 0 && m.StreamWidth > 0 && (m.WantWidth != m.StreamWidth || m.WantHeight != m.StreamHeight)
}

// AnalyzeBenchRun computes the summary metrics of one run.
func AnalyzeBenchRun(run *BenchRun) BenchMetrics {
	m := BenchMetrics{}
	if run == nil {
		return m
	}
	m.Backend = run.Backend
	m.Codec = run.Codec
	m.WantWidth, m.WantHeight = run.Width, run.Height
	m.StreamWidth, m.StreamHeight = run.StreamWidth, run.StreamHeight
	if pts := BenchLatencySeries(run); len(pts) > 0 {
		v := make([]float64, len(pts))
		for i, p := range pts {
			v[i] = p[1]
		}
		sort.Float64s(v)
		at := func(q float64) float64 { return v[min(len(v)-1, int(float64(len(v))*q))] }
		m.LatencySamples, m.LatencyP50, m.LatencyP95, m.LatencyP99 = len(v), at(0.5), at(0.95), at(0.99)
	}
	m.SwitchMs = run.SwitchMs
	m.StopMs = run.StopMs
	m.StartMs = run.StartMs
	m.StartupMs = run.StartupMs
	m.Error = run.Error
	if run.EndUs > run.StartUs {
		m.DurationSec = float64(run.EndUs-run.StartUs) / 1e6
	}
	m.Frames = len(run.Frames)
	benchLoadAverages(run.HostLoad, &m)

	ivs := BenchIntervals(run)
	expected := 1000.0 / 60
	if run.ExpectedFPS > 0 {
		expected = 1000.0 / float64(run.ExpectedFPS)
	}
	intervals := make([]float64, 0, len(ivs))
	for _, iv := range ivs {
		intervals = append(intervals, iv.IntervalMs)
	}
	if len(intervals) > 0 {
		sorted := append([]float64(nil), intervals...)
		sort.Float64s(sorted)
		m.IntervalP50 = benchPercentile(sorted, 50)
		m.IntervalP95 = benchPercentile(sorted, 95)
		m.IntervalP99 = benchPercentile(sorted, 99)
		m.IntervalMax = sorted[len(sorted)-1]
		m.IntervalStd = benchStddev(intervals)
		if m.IntervalP99 > 0 {
			m.Low1FPS = 1000 / m.IntervalP99
		}
		// The content repaints at 60Hz; if the stream runs slower than the
		// configured rate the median is the honest "one frame" here.
		if m.IntervalP50 > expected {
			expected = m.IntervalP50
		}
	}
	if m.DurationSec > 0 {
		m.AvgFPS = float64(m.Frames) / m.DurationSec
	}
	// A freeze: at least three frame times and at least 50ms -- the point
	// where a pause reads as a stutter rather than jitter.
	m.StallThreshMs = math.Max(3*expected, 50)

	ptsOK := benchHostTimingUsable(run.Frames)
	m.HostTimingOK = ptsOK
	var recoveries []float64
	for i, iv := range ivs {
		b := run.Frames[i+1]
		if iv.Lost > 0 {
			m.LossEvents++
			m.LostFrames += iv.Lost
			recoveries = append(recoveries, iv.IntervalMs)
			if b.IDR {
				m.RecoveredByIDR++
			} else {
				m.RecoveredByRFI++
			}
		}
		if iv.IntervalMs >= m.StallThreshMs {
			st := BenchStall{AtMs: iv.AtMs - iv.IntervalMs, DurationMs: iv.IntervalMs, LostFrames: iv.Lost}
			if !math.IsNaN(iv.HostGapMs) {
				st.HostGapMs = iv.HostGapMs
			}
			switch {
			case iv.Lost > 0:
				st.Cause = BenchCauseLoss
				st.Recovery = "rfi"
				if b.IDR {
					st.Recovery = "idr"
				}
				m.StallLoss++
			case ptsOK && !math.IsNaN(iv.HostGapMs) && iv.HostGapMs >= 0.6*iv.IntervalMs:
				st.Cause = BenchCauseHost
				m.StallHost++
			default:
				st.Cause = BenchCauseNetwork
				m.StallNetwork++
			}
			m.Stalls = append(m.Stalls, st)
			m.StallTotalMs += iv.IntervalMs
		} else if iv.IntervalMs > 2*expected {
			m.Hitches++
		}
	}
	m.StallCount = len(m.Stalls)
	if len(recoveries) > 0 {
		m.RecoveryAvgMs = benchMean(recoveries)
		m.RecoveryMaxMs = benchMaxOf(recoveries)
	}

	// Host.
	var hostLat, cadence, netJitter, transfer []float64
	var bytes uint64
	for i, f := range run.Frames {
		hostLat = append(hostLat, f.HostLatencyMs)
		bytes += uint64(f.Size)
		if f.IDR && i > 0 {
			m.IDRFrames++
		}
		if f.EnqueueUs >= f.ReceiveUs && f.ReceiveUs > 0 {
			transfer = append(transfer, benchUsDiffMs(f.EnqueueUs, f.ReceiveUs))
		}
	}
	for _, iv := range ivs {
		if !math.IsNaN(iv.HostGapMs) {
			cadence = append(cadence, iv.HostGapMs)
		}
		if !math.IsNaN(iv.NetJitter) {
			netJitter = append(netJitter, iv.NetJitter)
		}
	}
	if len(hostLat) > 0 {
		sorted := append([]float64(nil), hostLat...)
		sort.Float64s(sorted)
		m.HostLatencyAvg = benchMean(hostLat)
		m.HostLatencyP95 = benchPercentile(sorted, 95)
		m.HostLatencyMax = sorted[len(sorted)-1]
	}
	if len(cadence) > 0 {
		m.HostCadenceStd = benchStddev(cadence)
		m.HostCadenceMax = benchMaxOf(cadence)
		if avg := benchMean(cadence); avg > 0 {
			m.HostFPS = 1000 / avg
		}
	}
	if m.DurationSec > 0 {
		m.BitrateMbps = float64(bytes) * 8 / m.DurationSec / 1e6
	}

	// Network.
	if len(netJitter) > 0 {
		sorted := append([]float64(nil), netJitter...)
		sort.Float64s(sorted)
		m.NetJitterAvg = benchMean(netJitter)
		m.NetJitterP95 = benchPercentile(sorted, 95)
	}
	if len(transfer) > 0 {
		sorted := append([]float64(nil), transfer...)
		sort.Float64s(sorted)
		m.TransferAvg = benchMean(transfer)
		m.TransferP95 = benchPercentile(sorted, 95)
	}
	var rtts, playout []float64
	var pkts, lost uint64
	var rendered int64
	renderTicks := 0
	for _, t := range run.Ticks {
		if t.RTTValid {
			rtts = append(rtts, t.RTTMs)
		}
		playout = append(playout, t.PlayoutDelayMs)
		pkts += uint64(t.PacketsVideo) + uint64(t.PacketsFec)
		lost += uint64(t.FecRecovered) + uint64(t.FecFailed) + uint64(t.PacketsInvalid)
		m.FecRecovered += uint64(t.FecRecovered)
		m.FecFailed += uint64(t.FecFailed)
		if t.RenderValid {
			rendered += t.Rendered
			renderTicks++
		}
	}
	if len(rtts) > 0 {
		m.RTTAvg = benchMean(rtts)
		m.RTTMax = benchMaxOf(rtts)
	}
	if len(playout) > 0 {
		m.PlayoutDelayAvg = benchMean(playout)
	}
	if pkts > 0 {
		m.PacketLossPct = float64(lost) / float64(pkts) * 100
	}
	if renderTicks > 0 {
		m.RenderValid = true
		m.RenderFPS = float64(rendered) / (float64(renderTicks) * benchTickInterval.Seconds())
	}
	return m
}

// BenchFPSPerSecond buckets delivered frames into one-second fps values.
func BenchFPSPerSecond(run *BenchRun) []float64 {
	if run == nil || run.EndUs <= run.StartUs {
		return nil
	}
	n := int(math.Ceil(float64(run.EndUs-run.StartUs) / 1e6))
	out := make([]float64, n)
	for _, f := range run.Frames {
		i := int((f.SubmitUs - run.StartUs) / 1e6)
		if i >= 0 && i < n {
			out[i]++
		}
	}
	// The last bucket is usually partial.
	if tail := float64(run.EndUs-run.StartUs)/1e6 - float64(n-1); n > 0 && tail > 0 && tail < 1 {
		out[n-1] /= tail
	}
	return out
}

// benchHostTimingUsable reports whether the host's capture timestamps are
// real: a streamer that leaves them at 0 (or constant) can't be used to
// split host from network, and the analysis says so instead of guessing.
func benchHostTimingUsable(frames []BenchFrame) bool {
	if len(frames) < 10 {
		return false
	}
	increasing := 0
	for i := 1; i < len(frames); i++ {
		if frames[i].PtsUs > frames[i-1].PtsUs {
			increasing++
		}
	}
	return increasing*10 >= (len(frames)-1)*9
}

func benchLostBetween(a, b BenchFrame) int {
	d := int(b.Number) - int(a.Number) - 1
	if d < 0 {
		return 0
	}
	return d
}

func benchUsDiffMs(a, b uint64) float64 {
	return float64(int64(a-b)) / 1000
}

func benchUsToMs(us uint64) float64 { return float64(us) / 1000 }

func benchMean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func benchStddev(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	m := benchMean(v)
	s := 0.0
	for _, x := range v {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(v)-1))
}

func benchMaxOf(v []float64) float64 {
	m := math.Inf(-1)
	for _, x := range v {
		m = math.Max(m, x)
	}
	if math.IsInf(m, -1) {
		return 0
	}
	return m
}

// percentile of an ascending slice, nearest-rank.
func benchPercentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

// BenchGPUEncode/BenchGPUDecode read an engine-type map's encode and
// decode load; a shared encode+decode block ("codec", AMD) counts as both.
func BenchGPUEncode(g map[string]float64) float64 { return math.Max(g["encode"], g["codec"]) }
func BenchGPUDecode(g map[string]float64) float64 { return math.Max(g["decode"], g["codec"]) }

func benchLoadAverages(load []BenchLoad, m *BenchMetrics) {
	if len(load) == 0 {
		return
	}
	n := float64(len(load))
	for _, l := range load {
		m.HostCPUAvg += l.CPU / n
		m.StreamerCPUAvg += l.StreamerCPU / n
		m.GPU3DAvg += l.GPU["3d"] / n
		m.GPUEncodeAvg += BenchGPUEncode(l.GPU) / n
		m.GPUDecodeAvg += BenchGPUDecode(l.GPU) / n
		m.Streamer3DAvg += l.StreamerGPU["3d"] / n
		m.StreamerEncodeAvg += BenchGPUEncode(l.StreamerGPU) / n
		m.StreamerDecodeAvg += BenchGPUDecode(l.StreamerGPU) / n
	}
	m.HostLoadValid = true
}

// benchLatencyMaxMs: above this a reading is a stale or misread barcode, not a delay.
const benchLatencyMaxMs = 10000

// BenchLatencySeries turns the run's barcode readings into [seconds into the run,
// latency ms], following the clock offset as it was measured around each reading:
// the two wall clocks were seen moving by over 10 ms against each other within minutes.
func BenchLatencySeries(run *BenchRun) [][2]float64 {
	if run == nil || len(run.ClockSamples) == 0 {
		return nil
	}
	clock := run.ClockSamples
	startUs := run.StartedAt.UnixMicro()
	offsetAt := func(wall int64) float64 {
		i := sort.Search(len(clock), func(i int) bool { return clock[i].WallUs >= wall })
		switch i {
		case 0:
			return clock[0].OffsetUs
		case len(clock):
			return clock[len(clock)-1].OffsetUs
		}
		a, b := clock[i-1], clock[i]
		return a.OffsetUs + (b.OffsetUs-a.OffsetUs)*float64(wall-a.WallUs)/float64(b.WallUs-a.WallUs)
	}
	out := make([][2]float64, 0, len(run.Latency))
	for _, s := range run.Latency {
		host := float64(s.WallUs) + offsetAt(s.WallUs)
		units := uint32(int64(host / 100))
		ms := float64(int32(units-s.Bar)) / 10
		if ms <= 0 || ms > benchLatencyMaxMs {
			continue
		}
		out = append(out, [2]float64{float64(s.WallUs-startUs) / 1e6, ms})
	}
	return out
}
