package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/service"
)

// Streamer benchmark (gear menu → Run benchmark): for each selected
// streamer the agent switches its backend, the client starts a fresh
// stream and times it to the first frame, then the agent starts the same
// fast-moving test video (agent/internal/benchvideo) from its beginning and
// the client records every frame for a fixed window. Startup is measured
// before the window opens, so the streamers' different start times never
// leak into the comparison; everything inside the window is the same
// content from the same point.

// benchmarkResult is what gets saved as results.json next to chart.png.
type benchmarkResult struct {
	CreatedAt time.Time              `json:"created_at"`
	Duration  float64                `json:"duration_sec"`
	Metrics   []service.BenchMetrics `json:"metrics"`
	Runs      []*service.BenchRun    `json:"runs"`
}

// benchmarkSettleTime lets the new stream get past its first IDR and
// bitrate ramp before the content starts -- the same for every streamer.
const benchmarkSettleTime = time.Second

type benchmarkProgress func(text string, fraction float64)

// runBenchmark performs the whole benchmark; it blocks, so call it from a
// goroutine. The stream and the host's original streamer are restored on
// every exit path.
// monitor (a host monitor ID, "" to leave each streamer's own) pins both
// streamers' capture and the test video to one monitor for the whole run.
// codec ("" for the saved one) is the codec every run streams with. width/
// height (0, 0 for the saved resolution) is the resolution every run
// streams at.
func (mw *MainWindow) runBenchmark(ctx context.Context, backends []string, monitor, codec string, width, height int, window time.Duration, progress benchmarkProgress) (*benchmarkResult, error) {
	client := mw.usbClient
	vw := mw.videoWidget
	if client == nil || vw == nil {
		return nil, errors.New(i18n.Current.ErrorNoConnection)
	}

	progress(i18n.Current.BenchStepStatus, 0)
	status, err := client.BenchStatus()
	if err != nil {
		return nil, err
	}
	original := status.ActiveBackend
	wasStreaming := vw.IsStreaming()

	netGraphWas := service.NetGraphEnabled()
	service.SetNetGraphEnabled(true)

	controller.SetBenchmarkCodec(codec)
	if codec != "" {
		logrus.Infof("📈 [Benchmark] streaming with codec %s", codec)
	}
	controller.SetBenchmarkResolution(width, height)
	if width > 0 && height > 0 {
		logrus.Infof("📈 [Benchmark] streaming at %dx%d", width, height)
	}

	var recorder service.BenchRecorder
	defer func() {
		recorder.Stop()
		// Before the restore below restarts the user's own stream.
		controller.SetBenchmarkCodec("")
		controller.SetBenchmarkResolution(0, 0)
		_ = client.BenchVideoStop()
		if !netGraphWas {
			service.SetNetGraphEnabled(false)
		}
		vw.BenchmarkStopStream()
		// Putting the host back takes a streamer restart (~25 s for
		// Sunshine) and the stream's own start; the results don't depend
		// on it, so they show right away and this finishes behind them.
		go restoreBenchmarkHost(client, vw, monitor, original, wasStreaming)
	}()

	// The pin's streamer restart is left to the first run's backend switch
	// (see BenchSetMonitor).
	if monitor != "" {
		if err := client.BenchSetMonitor(monitor); err != nil {
			return nil, fmt.Errorf("monitor %s: %w", monitor, err)
		}
		logrus.Infof("📈 [Benchmark] capturing and playing on host monitor %s", monitor)
	}

	progress(i18n.Current.BenchStepPrepare, 0)
	if ready, note, err := client.BenchPrepare(); err != nil {
		return nil, err
	} else if !ready {
		logrus.Warnf("📈 [Benchmark] test video unavailable (%s); the host falls back to a generated pattern", note)
	}

	fps, width, height := vw.BenchmarkStreamConfig()
	result := &benchmarkResult{CreatedAt: time.Now(), Duration: window.Seconds()}
	steps := float64(len(backends))
	for i, kind := range backends {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		label := service.BenchBackendLabel(kind)
		base := float64(i) / steps
		run := &service.BenchRun{Backend: kind, ExpectedFPS: fps, Width: width, Height: height}
		result.Runs = append(result.Runs, run)
		err := mw.benchmarkOne(ctx, run, &recorder, window, func(text string, f float64) {
			progress(fmt.Sprintf(text, label), base+f/steps)
		}, func(text string, left int, f float64) {
			progress(fmt.Sprintf(text, label, left), base+f/steps)
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			run.Error = err.Error()
			logrus.Warnf("📈 [Benchmark] %s: %v", kind, err)
		}
	}
	for _, r := range result.Runs {
		result.Metrics = append(result.Metrics, service.AnalyzeBenchRun(r))
	}
	return result, nil
}

// benchmarkStreamer is the part of the video widget restoreBenchmarkHost
// needs.
type benchmarkStreamer interface {
	BenchmarkStartStream(timeout time.Duration) (time.Duration, error)
}

// restoreBenchmarkHost puts the host back the way the user had it after a
// benchmark: each streamer's own monitor, the original streamer, and the
// stream if one was running. The monitor release leaves the restart to the
// backend call right after it, which runs even when the original streamer
// is already active, so the host restarts at most once.
func restoreBenchmarkHost(client *api.USBClient, vw benchmarkStreamer, monitor, original string, wasStreaming bool) {
	start := time.Now()
	if monitor != "" {
		if err := client.BenchSetMonitor(""); err != nil {
			logrus.Warnf("📈 [Benchmark] restoring the streamers' monitors: %v", err)
		}
	}
	if original != "" {
		if sw, err := client.BenchSetBackend(original); err != nil {
			logrus.Warnf("📈 [Benchmark] restoring backend %s: %v", original, err)
		} else {
			logrus.Infof("📈 [Benchmark] host restored to %s: stop %s %.0fms, start %.0fms", original, sw.Stopped, sw.StopMs, sw.StartMs)
		}
	}
	if wasStreaming {
		if _, err := vw.BenchmarkStartStream(60 * time.Second); err != nil {
			logrus.Warnf("📈 [Benchmark] restarting the stream: %v", err)
		}
	}
	logrus.Infof("📈 [Benchmark] host restored in %s", time.Since(start).Round(time.Millisecond))
}

func (mw *MainWindow) benchmarkOne(ctx context.Context, run *service.BenchRun, recorder *service.BenchRecorder, window time.Duration,
	step func(text string, fraction float64), countdown func(text string, left int, fraction float64)) error {
	client, vw := mw.usbClient, mw.videoWidget

	vw.BenchmarkStopStream()
	_ = client.BenchVideoStop()

	step(i18n.Current.BenchStepSwitch, 0.02)
	sw, err := client.BenchSetBackend(run.Backend)
	if err != nil {
		return fmt.Errorf("switch: %w", err)
	}
	run.SwitchMs = sw.SwitchMs
	run.StopMs, run.StartMs, run.StoppedBackend = sw.StopMs, sw.StartMs, sw.Stopped

	step(i18n.Current.BenchStepStart, 0.06)
	startup, err := vw.BenchmarkStartStream(90 * time.Second)
	if err != nil {
		return fmt.Errorf("stream: %w", err)
	}
	run.StartupMs = float64(startup.Microseconds()) / 1000
	// The first frame can land before LiStartConnection returns, and the
	// codec is only reported once it has: wait for it instead of reading
	// it once (which left the Codec row at n/a).
	for deadline := time.Now().Add(5 * time.Second); ; {
		if codec, ok := vw.NegotiatedVideoCodecName(); ok {
			run.Codec = codec
			break
		}
		if time.Now().After(deadline) || sleepCtx(ctx, 100*time.Millisecond) != nil {
			break
		}
	}
	logrus.Infof("📈 [Benchmark] %s: switch %.0fms (stop %s %.0fms, start %.0fms), first frame %.0fms, codec %s",
		run.Backend, run.SwitchMs, run.StoppedBackend, run.StopMs, run.StartMs, run.StartupMs, run.Codec)

	if err := sleepCtx(ctx, benchmarkSettleTime); err != nil {
		return err
	}
	step(i18n.Current.BenchStepVideo, 0.1)
	video, err := client.BenchVideoStartFPS(run.ExpectedFPS)
	if err != nil {
		return fmt.Errorf("video: %w", err)
	}
	run.Content = video.Content
	run.Monitor = video.Monitor
	if err := benchVideoPlacementError(video); err != nil {
		return err
	}

	recorder.Start(run)
	loadErr := client.BenchLoadStart()
	if loadErr != nil {
		logrus.Infof("📈 [Benchmark] host load not sampled: %v", loadErr)
	}
	// Stopped with the recording, so both cover the same window; the defer
	// covers a cancelled run.
	stopLoad := func() {
		if loadErr != nil {
			return
		}
		loadErr = errors.New("stopped")
		raw, err := client.BenchLoadStop()
		if err == nil {
			err = json.Unmarshal(raw, &run.HostLoad)
		}
		if err != nil {
			logrus.Warnf("📈 [Benchmark] reading the host load: %v", err)
		}
	}
	defer stopLoad()
	started := time.Now()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		elapsed := time.Since(started)
		if elapsed >= window {
			break
		}
		countdown(i18n.Current.BenchStepRecord, int((window-elapsed).Seconds()+0.99), 0.1+0.9*elapsed.Seconds()/window.Seconds())
		select {
		case <-ctx.Done():
			recorder.Stop()
			return ctx.Err()
		case <-ticker.C:
		}
	}
	recorder.Stop()
	stopLoad()
	_ = client.BenchVideoStop()
	vw.BenchmarkStopStream()
	logrus.Infof("📈 [Benchmark] %s: recorded %d frames", run.Backend, len(run.Frames))
	return nil
}

// benchVideoPlacementError fails a run whose test video isn't on the monitor
// the benchmark pinned: the streamer would then be measured on a different
// picture than the other one. A window the agent couldn't see is let
// through (it can't tell), with a warning.
func benchVideoPlacementError(v api.BenchVideo) error {
	if v.RequestedMonitor == "" {
		return nil
	}
	if v.Monitor == "" {
		logrus.Warnf("📈 [Benchmark] the host couldn't confirm the test video is on %s", v.RequestedMonitor)
		return nil
	}
	if v.Monitor != v.RequestedMonitor {
		return fmt.Errorf("test video opened on %s instead of %s", v.Monitor, v.RequestedMonitor)
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// saveBenchmarkResult writes results.json and chart.png into a new
// timestamped folder and returns it.
func saveBenchmarkResult(res *benchmarkResult) (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "usbridge-client", "benchmarks", res.CreatedAt.Format("20060102_150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(res, "", " ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "results.json"), data, 0o644); err != nil {
		return "", err
	}
	f, err := os.Create(filepath.Join(dir, "chart.png"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, service.RenderBenchChart(res.Runs, res.Metrics)); err != nil {
		return "", err
	}
	return dir, nil
}
