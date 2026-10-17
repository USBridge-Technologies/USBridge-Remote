package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"time"

	"usbridge_agent/internal/benchvideo"
	"usbridge_agent/internal/hostload"
	"usbridge_agent/internal/monitors"
)

// benchApplication is what the streamer benchmark needs from the app
// beyond Application. Kept separate (and type-asserted) so test fakes of
// Application don't have to grow benchmark methods.
type benchApplication interface {
	BenchPlayer() *benchvideo.Player
	BenchStreamBackends() (active string, available []string)
	SetStreamBackend(kind string) error
	BenchMonitors() ([]monitors.Monitor, error)
	BenchMonitor() string
	// BenchHostGPU names this host's GPU, surfaced on /api/bench/status for
	// the client's Net Graph HUD (see app.App.BenchHostGPU's own doc
	// comment).
	BenchHostGPU() string
	SetBenchMonitor(id string, deferRestart bool) error
	LastBackendSwitch() BackendSwitchTiming
	// BenchVideoOutput names (by prefix) the compositor output the test
	// video has to play on because the active streamer shows only that
	// one, "" when it shows a monitor the video opens on anyway.
	BenchVideoOutput() string
}

// BackendSwitchTiming splits a bench/backend switch for the benchmark's
// statistics: stopping the previous streamer until its ports were free,
// and starting this one until its listener answered. Stopped is the kind
// that was stopped ("" when nothing ran, or the call was a no-op).
type BackendSwitchTiming struct {
	Stopped string `json:"stopped,omitempty"`
	StopMs  int64  `json:"stop_ms"`
	StartMs int64  `json:"start_ms"`
}

// BenchStatus is GET /api/bench/status.
type BenchStatus struct {
	ActiveBackend     string   `json:"active_backend"`
	AvailableBackends []string `json:"available_backends"`
	// ListedBackends: the streamers this host's OS can run at all, installed
	// or not -- what the client's benchmark dialog lists.
	ListedBackends []string        `json:"listed_backends,omitempty"`
	Video          benchvideo.Info `json:"video"`
	// Monitors the benchmark can be pinned to (empty where the host can't
	// enumerate them), and the one it's pinned to now ("" for none).
	Monitors []monitors.Monitor `json:"monitors,omitempty"`
	Monitor  string             `json:"monitor,omitempty"`
	// GPU names this host's GPU -- see benchApplication.BenchHostGPU's doc
	// comment. Empty if the host's platform detection couldn't determine it.
	GPU string `json:"gpu,omitempty"`
}

func (s *Server) benchApp(w http.ResponseWriter) (benchApplication, bool) {
	b, ok := s.app.(benchApplication)
	if !ok {
		s.fail(w, http.StatusNotImplemented, "bench_unsupported", errors.New("this agent build has no benchmark support"))
	}
	return b, ok
}

func (s *Server) benchStatus(w http.ResponseWriter, r *http.Request) {
	b, ok := s.benchApp(w)
	if !ok {
		return
	}
	active, available := b.BenchStreamBackends()
	mons, err := b.BenchMonitors()
	if err != nil && !errors.Is(err, monitors.ErrUnsupported) {
		log.Printf("[api] bench monitors: %v", err)
	}
	s.ok(w, "bench_status", BenchStatus{
		ActiveBackend:     active,
		AvailableBackends: available,
		ListedBackends:    listedBenchBackends(),
		Video:             b.BenchPlayer().Status(),
		Monitors:          mons,
		Monitor:           b.BenchMonitor(),
		GPU:               b.BenchHostGPU(),
	})
}

// benchMonitor pins both streamers' capture, and the test video, to one
// monitor for the benchmark ({"monitor": "<id>"}), or releases the pin
// ({"monitor": ""}), putting each streamer's own monitor back.
func (s *Server) benchMonitor(w http.ResponseWriter, r *http.Request) {
	b, ok := s.benchApp(w)
	if !ok {
		return
	}
	var req struct {
		Monitor string `json:"monitor"`
		// DeferRestart: don't restart the running streamer now, the
		// client's next bench/backend call does (see
		// App.SetBenchMonitor).
		DeferRestart bool `json:"defer_restart"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	if err := b.SetBenchMonitor(req.Monitor, req.DeferRestart); err != nil {
		log.Printf("[api] bench monitor %q: %v", req.Monitor, err)
		s.fail(w, http.StatusBadRequest, "bench_monitor_failed", err)
		return
	}
	s.ok(w, "bench_monitor", map[string]any{"monitor": req.Monitor})
}

// benchBackend switches the stream backend and reports how long the agent
// took to stop the old one and bring the new one up to a bound listener
// (SetStreamBackend already waits for that). The client times its own
// stream start separately, from the moment this returns.
func (s *Server) benchBackend(w http.ResponseWriter, r *http.Request) {
	b, ok := s.benchApp(w)
	if !ok {
		return
	}
	var req struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	// The video would otherwise keep playing across the switch and give
	// the next backend a head start on the content.
	b.BenchPlayer().Stop()
	start := time.Now()
	if err := b.SetStreamBackend(req.Kind); err != nil {
		log.Printf("[api] bench backend switch to %q failed: %v", req.Kind, err)
		s.fail(w, http.StatusInternalServerError, "bench_backend_failed", err)
		return
	}
	switchMs := time.Since(start).Milliseconds()
	active, _ := b.BenchStreamBackends()
	timing := b.LastBackendSwitch()
	log.Printf("[api] bench backend switched to %s in %dms (stop %s %dms, start %dms)", active, switchMs, timing.Stopped, timing.StopMs, timing.StartMs)
	s.ok(w, "bench_backend", map[string]any{
		"active_backend": active,
		"switch_ms":      switchMs,
		"stopped":        timing.Stopped,
		"stop_ms":        timing.StopMs,
		"start_ms":       timing.StartMs,
	})
}

// benchPrepare downloads the benchmark content ahead of the timed runs.
func (s *Server) benchPrepare(w http.ResponseWriter, r *http.Request) {
	b, ok := s.benchApp(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	path, err := b.BenchPlayer().Prepare(ctx)
	if err != nil {
		// Not fatal: Start falls back to a generated pattern.
		s.ok(w, "bench_prepare", map[string]any{"ready": false, "error": err.Error()})
		return
	}
	s.ok(w, "bench_prepare", map[string]any{"ready": true, "content_path": path})
}

func (s *Server) benchVideoStart(w http.ResponseWriter, r *http.Request) {
	b, ok := s.benchApp(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	var target *monitors.Monitor
	if id := b.BenchMonitor(); id != "" {
		mons, err := b.BenchMonitors()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, "bench_video_failed", err)
			return
		}
		m, found := monitors.Find(mons, id)
		if !found {
			s.fail(w, http.StatusInternalServerError, "bench_video_failed", fmt.Errorf("monitor %s is gone", id))
			return
		}
		target = &m
	}
	info, err := b.BenchPlayer().Start(ctx, target, b.BenchVideoOutput())
	if err != nil {
		log.Printf("[api] bench video start failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "bench_video_failed", err)
		return
	}
	s.ok(w, "bench_video_started", info)
}

func (s *Server) benchVideoStop(w http.ResponseWriter, r *http.Request) {
	b, ok := s.benchApp(w)
	if !ok {
		return
	}
	b.BenchPlayer().Stop()
	s.ok(w, "bench_video_stopped", nil)
}

// benchLoadStart starts sampling the host's CPU and GPU load (see
// internal/hostload) for the run the client is about to record.
func (s *Server) benchLoadStart(w http.ResponseWriter, r *http.Request) {
	s.benchLoad.Start()
	s.ok(w, "bench_load_started", map[string]any{"interval_ms": hostload.Interval.Milliseconds()})
}

// benchLoadStop ends the sampling and returns its samples (empty where the
// host can't read its load). Error names why, whenever samples came back
// empty -- see hostload.Sampler.LastError's doc comment for why this used
// to be an agent-log-only detail the client (and whoever read its
// benchmark table) had no way to see.
func (s *Server) benchLoadStop(w http.ResponseWriter, r *http.Request) {
	samples := s.benchLoad.Stop()
	if samples == nil {
		samples = []hostload.Sample{}
	}
	resp := map[string]any{"samples": samples}
	if len(samples) == 0 {
		if err := s.benchLoad.LastError(); err != nil {
			resp["error"] = err.Error()
		}
	}
	s.ok(w, "bench_load", resp)
}

// listedBenchBackends is every streamer that exists for this OS: Punktfunk
// on Linux and Windows, Vibepollo on Windows only.
func listedBenchBackends() []string {
	l := []string{"sunshine", "rustshine"}
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		l = append(l, "punktfunk")
	}
	if runtime.GOOS == "windows" {
		l = append(l, "vibepollo")
	}
	return l
}
