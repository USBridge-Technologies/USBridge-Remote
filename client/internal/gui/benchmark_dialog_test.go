package gui

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/service"
)

type benchRunStub func(ctx context.Context, backends []string, window time.Duration, progress benchmarkProgress) (*benchmarkResult, error)

// benchTestStatus is the fake agent's /api/bench/status data; tests that
// need monitors swap it (see withBenchStatus).
var benchTestStatus = `{"active_backend":"sunshine","available_backends":["sunshine","rustshine"]}`

// benchTestMonitor records the monitor the last benchmark run was started
// with.
var benchTestMonitor atomic.Value

// newBenchTestWindow wires a MainWindow to a fake agent and replaces the
// real benchmark run and result saving for the test.
func newBenchTestWindow(t *testing.T, run benchRunStub) (*MainWindow, fyne.Window) {
	t.Helper()
	i18n.Init("en")
	test.NewTempApp(t)
	w := test.NewTempWindow(t, widget.NewLabel("stream"))
	w.Resize(fyne.NewSize(1200, 900))

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/bench/status" {
			t.Errorf("unexpected agent request %s %s", r.Method, r.URL.Path)
			http.NotFound(rw, r)
			return
		}
		_, _ = rw.Write([]byte(`{"success":true,"data":` + benchTestStatus + `}`))
	}))
	t.Cleanup(srv.Close)

	prevRun, prevSave := benchmarkRunFn, saveBenchmarkResultFn
	benchmarkRunFn = func(_ *MainWindow, ctx context.Context, backends []string, monitor, _ string, _, _ int, window time.Duration, progress benchmarkProgress) (*benchmarkResult, error) {
		benchTestMonitor.Store(monitor)
		return run(ctx, backends, window, progress)
	}
	saveBenchmarkResultFn = func(*benchmarkResult) (string, error) { return "/tmp/bench", nil }
	t.Cleanup(func() {
		benchmarkRunFn, saveBenchmarkResultFn = prevRun, prevSave
		benchmarkBusy.Store(false)
		service.SetNetGraphBanner("")
	})
	benchmarkBusy.Store(false)

	mw := &MainWindow{window: w, usbClient: newTestUSBClient(t, srv), videoWidget: &controller.VideoWidget{}}
	return mw, w
}

func overlayCount(w fyne.Window) int {
	n := 0
	fyne.DoAndWait(func() { n = len(w.Canvas().Overlays().List()) })
	return n
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// findInOverlays returns the first object in the open overlays that match accepts.
func findInOverlays(w fyne.Window, match func(fyne.CanvasObject) bool) fyne.CanvasObject {
	var found fyne.CanvasObject
	fyne.DoAndWait(func() {
		for _, o := range w.Canvas().Overlays().List() {
			for _, c := range test.LaidOutObjects(o) {
				if match(c) {
					found = c
					return
				}
			}
		}
	})
	return found
}

func findButton(w fyne.Window, text string) fyne.CanvasObject {
	return findInOverlays(w, func(o fyne.CanvasObject) bool {
		if b, ok := o.(*widget.Button); ok && b.Text == text {
			return true
		}
		type labeled interface{ Text() string }
		if l, ok := o.(labeled); ok && l.Text() == text {
			if _, tap := o.(fyne.Tappable); tap {
				return true
			}
		}
		return false
	})
}

func openBenchmarkSetup(t *testing.T, mw *MainWindow, w fyne.Window) {
	t.Helper()
	mw.showBenchmarkDialog()
	waitFor(t, "the setup dialog", func() bool { return findButton(w, i18n.Current.BenchStart) != nil })
}

func tap(o fyne.CanvasObject) {
	t, ok := o.(fyne.Tappable)
	if !ok {
		return
	}
	fyne.DoAndWait(func() { test.Tap(t) })
}

func sampleBenchResult() *benchmarkResult {
	return &benchmarkResult{
		CreatedAt: time.Date(2026, 9, 27, 16, 42, 0, 0, time.UTC),
		Duration:  60,
		Runs:      []*service.BenchRun{{Backend: "sunshine", ExpectedFPS: 60}, {Backend: "rustshine", ExpectedFPS: 60}},
		Metrics: []service.BenchMetrics{
			{Backend: "sunshine", AvgFPS: 58.1, Low1FPS: 41, StallCount: 3, Codec: "hevc"},
			{Backend: "rustshine", AvgFPS: 59.7, Low1FPS: 55, StallCount: 0, Codec: "hevc"},
		},
	}
}

// Start closes the setup dialog, runs the benchmark exactly once with the
// ticked streamers, opens nothing over the stream while it runs (an
// overlay hides the native video on Windows), reports progress in the
// HUD, and ends on the results.
func TestBenchmarkStart_ClosesSetupAndNoPopupsDuringRun(t *testing.T) {
	var runs atomic.Int32
	progressed := make(chan struct{})
	release := make(chan struct{})
	var gotBackends []string
	mw, w := newBenchTestWindow(t, func(ctx context.Context, backends []string, window time.Duration, progress benchmarkProgress) (*benchmarkResult, error) {
		runs.Add(1)
		gotBackends = backends
		progress("Sunshine: measuring, 30 s left", 0.5)
		close(progressed)
		<-release
		return sampleBenchResult(), nil
	})

	openBenchmarkSetup(t, mw, w)
	if n := overlayCount(w); n != 1 {
		t.Fatalf("%d overlays with the setup dialog open, want 1", n)
	}
	tap(findButton(w, i18n.Current.BenchStart))

	<-progressed
	// Sample the overlay stack for a while mid-run: nothing may cover the
	// stream at any point, not just at one instant.
	maxOverlays := 0
	for end := time.Now().Add(150 * time.Millisecond); time.Now().Before(end); time.Sleep(2 * time.Millisecond) {
		if n := overlayCount(w); n > maxOverlays {
			maxOverlays = n
		}
	}
	if maxOverlays != 0 {
		t.Fatalf("up to %d overlays open while the benchmark runs, want 0 (setup must close, no progress popup)", maxOverlays)
	}
	if got := service.NetGraphBanner(); got != "Sunshine: measuring, 30 s left  50%" {
		t.Fatalf("HUD banner = %q", got)
	}
	if len(gotBackends) != 2 || gotBackends[0] != "sunshine" || gotBackends[1] != "rustshine" {
		t.Fatalf("benchmarked %v, want [sunshine rustshine]", gotBackends)
	}

	close(release)
	waitFor(t, "the results", func() bool { return findButton(w, i18n.Current.BenchSaveResults) != nil })
	if n := runs.Load(); n != 1 {
		t.Fatalf("benchmark ran %d times, want 1", n)
	}
	if n := overlayCount(w); n != 1 {
		t.Fatalf("%d overlays after the run, want just the results", n)
	}
	if service.NetGraphBanner() != "" {
		t.Fatal("HUD banner left behind after the run")
	}
	if benchmarkBusy.Load() {
		t.Fatal("benchmark still marked busy after it ended")
	}
}

func TestBenchmarkSetup_CancelRunsNothing(t *testing.T) {
	var runs atomic.Int32
	mw, w := newBenchTestWindow(t, func(context.Context, []string, time.Duration, benchmarkProgress) (*benchmarkResult, error) {
		runs.Add(1)
		return sampleBenchResult(), nil
	})

	openBenchmarkSetup(t, mw, w)
	tap(findButton(w, i18n.Current.Cancel))
	waitFor(t, "the setup dialog to close", func() bool { return overlayCount(w) == 0 })
	time.Sleep(50 * time.Millisecond)
	if runs.Load() != 0 {
		t.Fatal("cancel started the benchmark")
	}
	if benchmarkBusy.Load() {
		t.Fatal("cancel left the benchmark marked busy; the menu would never open it again")
	}
	openBenchmarkSetup(t, mw, w) // reopens fine
}

// A second menu click must neither stack a second setup dialog nor start
// anything while the first dialog is open or a run is in progress.
func TestBenchmarkMenu_NoSecondDialogOrRun(t *testing.T) {
	var runs atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{})
	mw, w := newBenchTestWindow(t, func(context.Context, []string, time.Duration, benchmarkProgress) (*benchmarkResult, error) {
		runs.Add(1)
		close(started)
		<-release
		return sampleBenchResult(), nil
	})

	openBenchmarkSetup(t, mw, w)
	mw.showBenchmarkDialog()
	time.Sleep(100 * time.Millisecond)
	if n := overlayCount(w); n != 1 {
		t.Fatalf("%d overlays after a second menu click, want 1", n)
	}

	start := findButton(w, i18n.Current.BenchStart)
	tap(start)
	tap(start) // double click on Start
	<-started
	mw.showBenchmarkDialog() // menu while running
	time.Sleep(100 * time.Millisecond)
	if n := overlayCount(w); n != 0 {
		t.Fatalf("%d overlays opened while the benchmark runs, want 0", n)
	}
	close(release)
	waitFor(t, "the results", func() bool { return findButton(w, i18n.Current.BenchSaveResults) != nil })
	if n := runs.Load(); n != 1 {
		t.Fatalf("benchmark ran %d times, want 1", n)
	}
}

func TestBenchmarkRunError_ShowsErrorAndResets(t *testing.T) {
	mw, w := newBenchTestWindow(t, func(_ context.Context, _ []string, _ time.Duration, progress benchmarkProgress) (*benchmarkResult, error) {
		progress("Sunshine: starting the stream…", 0.1)
		return nil, errors.New("stream: no video within 90s")
	})

	openBenchmarkSetup(t, mw, w)
	tap(findButton(w, i18n.Current.BenchStart))
	waitFor(t, "the error dialog", func() bool { return overlayCount(w) == 1 })
	if benchmarkBusy.Load() {
		t.Fatal("still busy after a failed run")
	}
	if service.NetGraphBanner() != "" {
		t.Fatal("HUD banner left behind after a failed run")
	}
}

// Regression: the results panel laid its body out in a VBox, which squeezed
// the scroll to one line -- only "Saved to …" was visible.
func TestBenchmarkResults_BodyFillsPanel(t *testing.T) {
	mw, w := newBenchTestWindow(t, nil)
	fyne.DoAndWait(func() { mw.showBenchmarkResults(sampleBenchResult(), "/tmp/bench") })

	o := findInOverlays(w, func(o fyne.CanvasObject) bool { _, ok := o.(*container.Scroll); return ok })
	if o == nil {
		t.Fatal("no scroll in the results dialog")
	}
	var h float32
	fyne.DoAndWait(func() { h = o.Size().Height })
	if h < 400 {
		t.Fatalf("results scroll is %.0fpx tall in a 900px window, want most of it", h)
	}
	if findButton(w, i18n.Current.BenchSaveResults) == nil || findButton(w, i18n.Current.Close) == nil {
		t.Fatal("download/close buttons missing")
	}
}

func TestBenchmarkResults_CompactLayout(t *testing.T) {
	mw, w := newBenchTestWindow(t, nil)
	fyne.DoAndWait(func() {
		w.Resize(fyne.NewSize(390, 780))
		mw.showBenchmarkResults(sampleBenchResult(), `C:\Users\bogom\AppData\Roaming\USBridge`)
	})
	if findButton(w, i18n.Current.Close) != nil {
		t.Fatal("compact results should not show Close")
	}
	if findButton(w, i18n.Current.BenchSaveResults) != nil {
		t.Fatal("compact results should use an icon download, not the pill label")
	}
	sz := benchResultsSizeFn(fyne.NewSize(390, 780), nil)
	if sz.Width > 390 || sz.Height > 780 {
		t.Fatalf("compact panel %v larger than canvas", sz)
	}
	if sz.Width < 300 {
		t.Fatalf("compact panel too narrow: %v", sz)
	}
}

func TestWriteBenchmarkZip(t *testing.T) {
	i18n.Init("en")
	res := sampleBenchResult()
	var buf bytes.Buffer
	if err := writeBenchmarkZip(&buf, res); err != nil {
		t.Fatalf("writeBenchmarkZip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name], _ = io.ReadAll(rc)
		rc.Close()
	}
	if len(files) != 2 {
		t.Fatalf("zip has %d files, want results.json and chart.png", len(files))
	}
	var got benchmarkResult
	if err := json.Unmarshal(files["results.json"], &got); err != nil {
		t.Fatalf("results.json: %v", err)
	}
	if len(got.Metrics) != 2 || got.Metrics[1].AvgFPS != 59.7 {
		t.Fatalf("results.json round trip lost data: %+v", got.Metrics)
	}
	if _, err := png.Decode(bytes.NewReader(files["chart.png"])); err != nil {
		t.Fatalf("chart.png: %v", err)
	}
}

func TestBenchmarkBestMask_TiedDisplayHighlightsBoth(t *testing.T) {
	oks := []bool{true, true}
	got := benchmarkBestMask([]float64{60.04, 59.96}, oks, false, "%.1f")
	if !got[0] || !got[1] {
		t.Fatalf("60.04 and 59.96 both show as 60.0 fps, want both green, got %v", got)
	}
	got = benchmarkBestMask([]float64{60.2, 59.9}, oks, false, "%.1f")
	if !got[0] || got[1] {
		t.Fatalf("60.2 vs 59.9 should green only the first, got %v", got)
	}
	got = benchmarkBestMask([]float64{12.04, 12.01}, oks, true, "%.1f ms")
	if !got[0] || !got[1] {
		t.Fatalf("lower-is-better 12.04 and 12.01 both show as 12.0, want both green, got %v", got)
	}
}
