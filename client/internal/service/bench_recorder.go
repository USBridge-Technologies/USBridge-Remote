package service

import (
	"sync"
	"time"
)

// Streamer benchmark recorder: captures every video frame the host
// delivers (bench_frames.c, fed from dr_submit) plus a 10Hz series of the
// connection counters the Net Graph HUD shows, for one timed run against
// one streamer. bench_analysis.go turns a BenchRun into metrics;
// bench_chart.go draws it.
//
// Same "nil hook = no data" contract as net_graph.go: on a platform without
// the cgo recorder (web) Start still works and the run simply has no
// frames.

// BenchFrame is one frame as received from the host.
type BenchFrame struct {
	Number        int32   `json:"n"`
	IDR           bool    `json:"idr,omitempty"`
	Size          uint32  `json:"size"`
	HostLatencyMs float64 `json:"host_ms"`
	// Client clock (LiGetMicroseconds): first packet in, frame reassembled,
	// frame handed to the decoder (after the playout buffer).
	ReceiveUs uint64 `json:"rx_us"`
	EnqueueUs uint64 `json:"enq_us"`
	SubmitUs  uint64 `json:"sub_us"`
	// Host clock: when the host captured this frame.
	PtsUs uint64 `json:"pts_us"`
}

// BenchLatencySample is one time barcode read from a decoded frame: the client's wall
// clock when the frame was ready, and the host's when it drew the picture (see
// agent/internal/benchrender: the wall clock's low 32 bits in 100 us units).
type BenchLatencySample struct {
	WallUs int64  `json:"wall_us"`
	Bar    uint32 `json:"bar"`
}

// BenchClockSample is one reading of the host's wall clock against this client's:
// host minus client, from the quickest of a few round trips.
type BenchClockSample struct {
	WallUs   int64   `json:"wall_us"`
	OffsetUs float64 `json:"offset_us"`
	RTTMs    float64 `json:"rtt_ms"`
}

// BenchTick is one 100ms sample of the connection counters.
type BenchTick struct {
	AtUs           uint64  `json:"at_us"` // client clock, same as BenchFrame
	RTTMs          float64 `json:"rtt_ms"`
	RTTVarianceMs  float64 `json:"rtt_var_ms"`
	RTTValid       bool    `json:"rtt_valid"`
	JitterMs       float64 `json:"jitter_ms"`
	PlayoutDelayMs float64 `json:"playout_ms"`
	PacketsVideo   uint32  `json:"pkts"`
	PacketsFec     uint32  `json:"pkts_fec"`
	FecRecovered   uint32  `json:"fec_rec"`
	FecFailed      uint32  `json:"fec_fail"`
	PacketsInvalid uint32  `json:"pkts_bad"`
	// Rendered is how many frames the client's renderer presented this
	// tick; RenderValid is false where no renderer counter is wired.
	Rendered    int64 `json:"rendered"`
	RenderValid bool  `json:"render_valid"`
}

// BenchLoad is one reading of the host's load, as the agent samples it
// (agent/internal/hostload): whole-machine CPU and the streamer processes'
// share (percent of all cores), GPU utilization per engine type ("3d",
// "encode", "decode", "copy", "codec") for the whole machine and for the
// streamer alone.
type BenchLoad struct {
	AtMs        int64              `json:"at_ms"` // since the agent started sampling
	CPU         float64            `json:"cpu"`
	StreamerCPU float64            `json:"streamer_cpu"`
	GPU         map[string]float64 `json:"gpu,omitempty"`
	StreamerGPU map[string]float64 `json:"streamer_gpu,omitempty"`
}

// BenchRun is everything recorded for one streamer.
type BenchRun struct {
	Backend     string    `json:"backend"`
	StartedAt   time.Time `json:"started_at"`
	ExpectedFPS int       `json:"expected_fps"`
	Width       int       `json:"width,omitempty"`
	Height      int       `json:"height,omitempty"`
	// SwitchMs: agent stopped the previous streamer and brought this one
	// up. StartupMs: client asked for a stream until its first frame.
	// Measured before recording starts and kept out of every other metric.
	SwitchMs  float64 `json:"switch_ms"`
	StartupMs float64 `json:"startup_ms"`
	// SwitchMs split: stopping the previous streamer (StoppedBackend)
	// until its ports were free, and starting this one until it answered.
	// Zero from an agent that doesn't report them.
	StopMs         float64 `json:"stop_ms,omitempty"`
	StartMs        float64 `json:"start_ms,omitempty"`
	StoppedBackend string  `json:"stopped_backend,omitempty"`
	// StreamWidth/StreamHeight are the frames as they were decoded, against Width/Height
	// asked for: a streamer may send its monitor's own size. 0 when none was sampled.
	StreamWidth  int `json:"stream_width,omitempty"`
	StreamHeight int `json:"stream_height,omitempty"`
	// StartUs/EndUs bound the measurement window on the client clock.
	StartUs     uint64       `json:"start_us"`
	EndUs       uint64       `json:"end_us"`
	Frames      []BenchFrame `json:"frames"`
	Ticks       []BenchTick  `json:"ticks"`
	// Latency needs the test window's barcode, and ClockSamples an agent that can
	// tell the time: without both a run just has no latency figures.
	Latency      []BenchLatencySample `json:"latency,omitempty"`
	ClockSamples []BenchClockSample   `json:"clock,omitempty"`
	RingDropped uint32       `json:"ring_dropped,omitempty"`
	Content     string       `json:"content,omitempty"`
	// Monitor is the host monitor the test video played on, as the agent
	// saw its window ("" when it couldn't tell or none was pinned).
	Monitor string `json:"monitor,omitempty"`
	// Codec is what NegotiatedVideoCodecName reported once this run's
	// stream was up -- the server's actual choice, not the client's
	// request, same ground-truth distinction as the Net Graph HUD's own
	// codec line. Empty when the platform doesn't wire that hook yet.
	Codec string `json:"codec,omitempty"`
	// HostLoad is the host's CPU/GPU load over the window, sampled by the
	// agent from the moment recording started; empty from an agent (or a
	// host OS) that doesn't sample it.
	HostLoad []BenchLoad `json:"host_load,omitempty"`
	Error    string      `json:"error,omitempty"`
}

var (
	benchFramesEnableFn  func(on bool)
	benchFramesDrainFn   func(dst []BenchFrame) []BenchFrame
	benchLatencyDrainFn  func(dst []BenchLatencySample) []BenchLatencySample
	benchStreamSizeFn    func() (width, height int)
	benchFramesDroppedFn func() uint32
	benchNowUsFn         func() uint64
	// benchRenderedFramesFn returns the renderer's running count of
	// presented frames (Vulkan/GL paths), nil where there is none.
	benchRenderedFramesFn func() (int64, bool)
)

const benchTickInterval = 100 * time.Millisecond

// BenchRecorder records one run at a time.
type BenchRecorder struct {
	mu   sync.Mutex
	run  *BenchRun
	stop chan struct{}
	done chan struct{}
}

func benchNowUs() uint64 {
	if fn := benchNowUsFn; fn != nil {
		return fn()
	}
	return uint64(time.Now().UnixMicro())
}

// Start begins recording into run (whose metadata the caller fills in).
func (r *BenchRecorder) Start(run *BenchRun) {
	r.Stop()
	r.mu.Lock()
	defer r.mu.Unlock()
	run.StartedAt = time.Now()
	run.StartUs = benchNowUs()
	r.run = run
	r.stop = make(chan struct{})
	r.done = make(chan struct{})
	if fn := benchLatencyDrainFn; fn != nil {
		fn(nil) // whatever the previous run left
	}
	if fn := benchStreamSizeFn; fn != nil {
		fn()
	}
	if fn := benchFramesEnableFn; fn != nil {
		fn(true)
	}
	go r.loop(run, r.stop, r.done)
}

// Stop ends the current recording and returns it (nil if none).
func (r *BenchRecorder) Stop() *BenchRun {
	r.mu.Lock()
	run, stop, done := r.run, r.stop, r.done
	r.run, r.stop, r.done = nil, nil, nil
	r.mu.Unlock()
	if run == nil {
		return nil
	}
	close(stop)
	<-done
	if fn := benchFramesEnableFn; fn != nil {
		fn(false)
	}
	if fn := benchFramesDrainFn; fn != nil {
		run.Frames = fn(run.Frames)
	}
	if fn := benchLatencyDrainFn; fn != nil {
		run.Latency = fn(run.Latency)
	}
	if fn := benchFramesDroppedFn; fn != nil {
		run.RingDropped = fn()
	}
	if fn := benchStreamSizeFn; fn != nil {
		run.StreamWidth, run.StreamHeight = fn()
	}
	run.EndUs = benchNowUs()
	// A frame whose first packet arrived before the window opened belongs
	// to the previous content state; drop it so both runs start clean.
	kept := run.Frames[:0]
	for _, f := range run.Frames {
		if f.SubmitUs >= run.StartUs {
			kept = append(kept, f)
		}
	}
	run.Frames = kept
	return run
}

func (r *BenchRecorder) loop(run *BenchRun, stop, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(benchTickInterval)
	defer ticker.Stop()

	var prev netGraphRawNetworkStats
	havePrev := false
	var prevRendered int64
	haveRendered := false
	if fn := netGraphNetworkStatsFn; fn != nil {
		prev, havePrev = fn(), true
	}
	if fn := benchRenderedFramesFn; fn != nil {
		prevRendered, haveRendered = fn()
	}
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		t := BenchTick{AtUs: benchNowUs()}
		if fn := netGraphNetworkStatsFn; fn != nil {
			raw := fn()
			t.RTTMs, t.RTTVarianceMs, t.RTTValid = raw.RTTMs, raw.RTTVarianceMs, raw.RTTValid
			t.JitterMs, t.PlayoutDelayMs = raw.JitterMs, raw.PlayoutDelayMs
			if havePrev {
				t.PacketsVideo = netGraphDeltaU32(raw.PacketCountVideo, prev.PacketCountVideo)
				t.PacketsFec = netGraphDeltaU32(raw.PacketCountFec, prev.PacketCountFec)
				t.FecRecovered = netGraphDeltaU32(raw.PacketCountFecRecovered, prev.PacketCountFecRecovered)
				t.FecFailed = netGraphDeltaU32(raw.PacketCountFecFailed, prev.PacketCountFecFailed)
				t.PacketsInvalid = netGraphDeltaU32(raw.PacketCountInvalid, prev.PacketCountInvalid)
			}
			prev, havePrev = raw, true
		}
		if fn := benchRenderedFramesFn; fn != nil {
			if n, ok := fn(); ok {
				if haveRendered && n >= prevRendered {
					t.Rendered, t.RenderValid = n-prevRendered, true
				}
				prevRendered, haveRendered = n, true
			}
		}
		var frames []BenchFrame
		if fn := benchFramesDrainFn; fn != nil {
			frames = fn(nil)
		}
		r.mu.Lock()
		run.Ticks = append(run.Ticks, t)
		run.Frames = append(run.Frames, frames...)
		if fn := benchLatencyDrainFn; fn != nil {
			run.Latency = fn(run.Latency)
		}
		r.mu.Unlock()
	}
}

// BenchSupported reports whether this build records per-frame data (the
// native moonlight path); the web client has no dr_submit to hook.
func BenchSupported() bool { return benchFramesDrainFn != nil }
