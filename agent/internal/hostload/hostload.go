// Package hostload samples the host's CPU and GPU load for the streamer
// benchmark: whole-machine CPU, the streamer processes' CPU, and GPU
// utilization per engine type (3D, video encode, video decode, copy), for
// the whole machine and for the streamer processes alone. Windows reads the
// same performance counters Task Manager shows; elsewhere Start works and
// the run simply has no samples.
package hostload

import (
	"strings"
	"sync"
	"time"
)

// Sample is one reading. CPU is a share of the whole machine (all cores =
// 100); GPU is per engine type as Task Manager shows it (the busiest
// engine of that type), keyed "3d", "encode", "decode", "copy", ...
type Sample struct {
	AtMs        int64              `json:"at_ms"` // since Start
	CPU         float64            `json:"cpu"`
	StreamerCPU float64            `json:"streamer_cpu"`
	GPU         map[string]float64 `json:"gpu,omitempty"`
	StreamerGPU map[string]float64 `json:"streamer_gpu,omitempty"`
}

// Interval is how often a running Sampler reads the counters.
const Interval = 500 * time.Millisecond

// streamerNames are the streamer processes' image names, lower case,
// without ".exe".
// punktfunk splits the work across processes (the host, plus the capture and
// encode workers where NVENC/capture actually run), so all three count.
var streamerNames = []string{
	"sunshine", "usbridge-streamer",
	"punktfunk-host", "punktfunk-encode-worker", "punktfunk-capture-worker",
}

func isStreamer(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	// Performance counters name a second instance "sunshine#1".
	if i := strings.IndexByte(name, '#'); i >= 0 {
		name = name[:i]
	}
	for _, n := range streamerNames {
		if name == n {
			return true
		}
	}
	return false
}

// Sampler records samples between Start and Stop.
type Sampler struct {
	mu      sync.Mutex
	samples []Sample
	stop    chan struct{}
	done    chan struct{}
	// lastErr is why the most recent run produced zero samples (newReader
	// failing outright -- e.g. this platform's hostload_other.go stub, or a
	// real PDH failure on Windows), nil once at least one sample landed.
	// Previously this only ever reached logf (silent on every platform but
	// Windows, and even there only visible in the agent's own log file) --
	// the client/benchmark table just saw an empty list and showed every
	// host-load row as "n/a" with no way to tell "genuinely not supported
	// here" apart from "something's actually broken, go look". See
	// LastError's doc comment for how this surfaces now.
	lastErr error
}

// Start begins a new recording, dropping any previous one.
func (s *Sampler) Start() {
	s.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = nil
	s.lastErr = nil
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go s.loop(s.stop, s.done)
}

// LastError reports why Start's most recent run produced no samples --
// nil once sampling is actually working (individual per-tick read()
// errors inside loop are already just skipped, not fatal). Safe to call
// any time, including while a recording is in progress or before the
// first Start. See internal/api/bench.go's benchLoadStop, which surfaces
// this to the client instead of letting an empty samples list pass as
// unexplained.
func (s *Sampler) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// Stop ends the recording and returns its samples.
func (s *Sampler) Stop() []Sample {
	s.mu.Lock()
	stop, done := s.stop, s.done
	s.stop, s.done = nil, nil
	s.mu.Unlock()
	if stop == nil {
		return nil
	}
	close(stop)
	<-done
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.samples
	s.samples = nil
	return out
}

func (s *Sampler) loop(stop, done chan struct{}) {
	defer close(done)
	r, err := newReader()
	if err != nil {
		logf("host load sampling unavailable: %v", err)
		s.mu.Lock()
		s.lastErr = err
		s.mu.Unlock()
		return
	}
	defer r.close()
	start := time.Now()
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		sample, err := r.read()
		if err != nil {
			// Recorded but not fatal: one bad tick (e.g. a transient PDH
			// hiccup) must not stop the rest of the run. Only matters as
			// LastError's answer if every tick fails this way and the run
			// ends with zero samples -- otherwise a later successful tick's
			// nil overwrites it below.
			s.mu.Lock()
			s.lastErr = err
			s.mu.Unlock()
			continue
		}
		sample.AtMs = time.Since(start).Milliseconds()
		s.mu.Lock()
		s.samples = append(s.samples, sample)
		s.lastErr = nil
		s.mu.Unlock()
	}
}
