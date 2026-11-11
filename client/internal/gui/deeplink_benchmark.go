package gui

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"usbridge-client/internal/models"
)

// deepLinkBenchmark is the benchmark an immediate connect link asks for:
//
//	usbridge://connect?host=...&master_key=...&immediate=true&benchmark=30&bench_codec=h265
//
// benchmark is the recording window in seconds ("true"/"1": the dialog's default);
// bench_codec forces one codec (h264, h265, av1) instead of the saved one.
type deepLinkBenchmark struct {
	window time.Duration
	codec  string
}

// parseDeepLinkBenchmark reads the link's benchmark request, if it has one.
func parseDeepLinkBenchmark(uri string) (deepLinkBenchmark, bool) {
	u, err := url.Parse(uri)
	if err != nil {
		return deepLinkBenchmark{}, false
	}
	q := u.Query()
	v := strings.TrimSpace(strings.ToLower(q.Get("benchmark")))
	if v == "" || v == "0" || v == "false" {
		return deepLinkBenchmark{}, false
	}
	b := deepLinkBenchmark{window: benchmarkWindows[1].d}
	if n, err := strconv.Atoi(v); err == nil && n > 1 {
		b.window = time.Duration(min(n, 600)) * time.Second
	}
	switch strings.ToLower(q.Get("bench_codec")) {
	case "h264", "avc":
		b.codec = models.VideoModeH264
	case "h265", "hevc":
		b.codec = models.VideoModeH265
	case "av1":
		b.codec = models.VideoModeAV1
	}
	return b, true
}

// runDeepLinkBenchmark waits for the link's connection to stream, then runs the
// benchmark the setup dialog would with its defaults: every streamer the host has,
// its primary monitor, the stream's own resolution. The results open as usual.
func (mw *MainWindow) runDeepLinkBenchmark(opts deepLinkBenchmark) {
	go func() {
		deadline := time.Now().Add(2 * time.Minute)
		for {
			if mw.isClosing.Load() {
				return
			}
			if mw.usbClient != nil && mw.videoWidget != nil && mw.videoWidget.IsStreaming() {
				break
			}
			if time.Now().After(deadline) {
				logrus.Warnf("📈 [Benchmark] deep link: no stream within 2 min, benchmark not started")
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		time.Sleep(3 * time.Second) // let the first stream settle like a user would
		if !benchmarkBusy.CompareAndSwap(false, true) {
			logrus.Warnf("📈 [Benchmark] deep link: a benchmark is already running")
			return
		}
		status, err := mw.usbClient.BenchStatus()
		if err != nil {
			benchmarkBusy.Store(false)
			logrus.Warnf("📈 [Benchmark] deep link: host status: %v", err)
			return
		}
		backends := status.AvailableBackends
		if len(backends) == 0 {
			benchmarkBusy.Store(false)
			logrus.Warnf("📈 [Benchmark] deep link: the host lists no streamer")
			return
		}
		monitor := ""
		if choices, def := benchMonitorChoices(status.Monitors); len(choices) > 0 {
			monitor = choices[def].id
		}
		logrus.Infof("📈 [Benchmark] deep link: starting %v on %q for %s", backends, monitor, opts.window)
		mw.startBenchmark(backends, monitor, opts.codec, 0, 0, opts.window)
	}()
}
