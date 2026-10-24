package gui

import (
	"bytes"
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
)

var benchmarkWindows = []struct {
	label string
	d     time.Duration
}{{"30 s", 30 * time.Second}, {"60 s", time.Minute}, {"2 min", 2 * time.Minute}, {"5 min", 5 * time.Minute}}

// benchmarkBusy is set from opening the setup dialog until the benchmark
// ends (or the dialog is cancelled), so a repeated menu click can't stack a
// second dialog or start a second run on top of the first.
var benchmarkBusy atomic.Bool

// benchmarkRunFn and saveBenchmarkResultFn are variables only so tests can
// drive the dialog flow without a host.
var (
	benchmarkRunFn        = (*MainWindow).runBenchmark
	saveBenchmarkResultFn = saveBenchmarkResult
)

// showBenchmarkDialog asks which streamers to compare and for how long.
func (mw *MainWindow) showBenchmarkDialog() {
	if mw.usbClient == nil || mw.videoWidget == nil {
		return
	}
	if !benchmarkBusy.CompareAndSwap(false, true) {
		return
	}
	client := mw.usbClient
	go func() {
		status, err := client.BenchStatus()
		fyne.Do(func() {
			if err != nil {
				benchmarkBusy.Store(false)
				view.ShowErrorDialog(fmt.Errorf("%s: %v", i18n.Current.BenchFailed, err), mw.window)
				return
			}
			mw.showBenchmarkSetup(status.ListedBackends, status.AvailableBackends, status.Monitors)
		})
	}()
}

// benchMonitorChoice is one entry of the setup dialog's monitor list.
type benchMonitorChoice struct {
	label string
	id    string
}

// benchMonitorChoices labels the host's monitors for the setup dialog and
// picks the default: the primary monitor, else the first listed.
func benchMonitorChoices(mons []api.BenchMonitor) (choices []benchMonitorChoice, def int) {
	for i, m := range mons {
		name := m.Name
		short := strings.TrimPrefix(m.ID, `\\.\`)
		if name == "" {
			name = short
		} else if short != m.ID {
			name += " (" + short + ")"
		}
		label := fmt.Sprintf("%s · %dx%d", name, m.Width, m.Height)
		if m.Primary {
			label += " · " + i18n.Current.BenchMonitorPrimary
			def = i
		}
		choices = append(choices, benchMonitorChoice{label: label, id: m.ID})
	}
	return choices, def
}

func (mw *MainWindow) showBenchmarkSetup(listed, available []string, mons []api.BenchMonitor) {
	has := map[string]bool{}
	for _, b := range available {
		has[b] = true
	}
	// Every streamer is listed (greyed out when the host lacks one), so it is
	// clear which ones a benchmark could compare after installing them.
	// Only those the host's OS has; an older agent doesn't say, so it gets
	// the old list (Punktfunk only where installed).
	kinds := listed
	if len(kinds) == 0 {
		kinds = []string{"sunshine", "rustshine"}
		if has["punktfunk"] {
			kinds = append(kinds, "punktfunk")
		}
	}
	checks := map[string]view.DialogToggle{}
	var rows []fyne.CanvasObject
	for _, kind := range kinds {
		c := view.NewDialogToggle(has[kind], nil)
		if !has[kind] {
			c.Disable()
		}
		checks[kind] = c
		desc := ""
		if !has[kind] {
			desc = i18n.Current.BenchNotInstalled
		}
		rows = append(rows, view.NewDialogToggleRow(c, service.BenchBackendLabel(kind), desc, 0))
	}

	var durLabels []string
	for _, w := range benchmarkWindows {
		durLabels = append(durLabels, w.label)
	}
	durSel := view.NewDialogPicker(durLabels, benchmarkWindows[1].label, nil)

	// Both streamers capture, and the test video plays on, the one monitor
	// picked here -- otherwise each streamer used its own saved monitor and
	// the video landed wherever the player opened. Hidden when the host
	// can't list its monitors (then nothing is pinned).
	choices, defChoice := benchMonitorChoices(mons)
	var monSel *view.HeaderDropdown
	fields := []fyne.CanvasObject{}
	if len(choices) > 0 {
		var monLabels []string
		for _, c := range choices {
			monLabels = append(monLabels, c.label)
		}
		monSel = view.NewDialogPicker(monLabels, choices[defChoice].label, nil)
		fields = append(fields, view.NewDialogField(i18n.Current.BenchMonitor, monSel))
	}
	codecSel := view.NewDialogPicker(benchCodecLabels(), benchCodecs[0].label(), nil)
	fields = append(fields, view.NewDialogField(i18n.Current.BenchCodec, codecSel))
	resSel := view.NewDialogPicker(benchResolutionLabels(), benchResolutions[0].label(), nil)
	fields = append(fields, view.NewDialogField(i18n.Current.BenchResolution, resSel))
	fields = append(fields, view.NewDialogField(i18n.Current.BenchDuration, durSel))

	bodyKids := []fyne.CanvasObject{
		view.NewDialogHint(i18n.Current.BenchHint, 0),
		view.NewDialogVSpace(8),
		view.NewDialogSurfaceCard(container.NewVBox(rows...)),
		view.NewDialogVSpace(10),
	}
	for _, f := range fields {
		bodyKids = append(bodyKids, f, view.NewDialogVSpace(8))
	}
	body := container.NewVBox(bodyKids...)

	view.ShowBrandFormDialog(view.BrandFormDialogSpec{
		Parent:     mw.window,
		Title:      i18n.Current.BenchTitle,
		Body:       body,
		CancelText: i18n.Current.Cancel,
		ApplyText:  i18n.Current.BenchStart,
		OnCancel: func() {
			benchmarkBusy.Store(false)
		},
		OnApply: func() {
			var picked []string
			for _, kind := range kinds {
				if checks[kind].IsChecked() && !checks[kind].Disabled() {
					picked = append(picked, kind)
				}
			}
			if len(picked) == 0 {
				benchmarkBusy.Store(false)
				view.ShowInfoDialog(i18n.Current.BenchTitle, i18n.Current.BenchNeedOne, mw.window)
				return
			}
			window := benchmarkWindows[1].d
			for _, w := range benchmarkWindows {
				if w.label == durSel.Selected {
					window = w.d
				}
			}
			monitor := ""
			if monSel != nil {
				for _, c := range choices {
					if c.label == monSel.Selected {
						monitor = c.id
					}
				}
			}
			codec := ""
			for _, c := range benchCodecs {
				if c.label() == codecSel.Selected {
					codec = c.mode
				}
			}
			resWidth, resHeight := 0, 0
			for _, r := range benchResolutions {
				if r.label() == resSel.Selected {
					resWidth, resHeight = r.width, r.height
				}
			}
			mw.startBenchmark(picked, monitor, codec, resWidth, resHeight, window)
		},
	})
}

// benchCodec is one entry of the setup dialog's codec pick: the saved
// codec (mode ""), or one forced for every streamer so both are compared
// on the same codec.
type benchCodec struct {
	mode string
	name string
}

func (c benchCodec) label() string {
	if c.mode == "" {
		return i18n.Current.BenchCodecSaved
	}
	return c.name
}

var benchCodecs = []benchCodec{{"", ""}, {models.VideoModeH264, "H.264"}, {models.VideoModeH265, "HEVC (H.265)"}, {models.VideoModeAV1, "AV1"}}

func benchCodecLabels() []string {
	var out []string
	for _, c := range benchCodecs {
		out = append(out, c.label())
	}
	return out
}

// benchResolution is one entry of the setup dialog's resolution pick: the
// saved resolution (0x0), or one forced for every streamer so both are
// compared at the same resolution -- the benchmark's test content
// (agent/internal/benchvideo) is itself native 1080p60, so picking above
// that upscales the source rather than testing a sharper one; the point is
// forcing both streamers to the same capture/encode resolution, not a
// higher-fidelity test clip.
type benchResolution struct {
	width, height int
}

func (r benchResolution) label() string {
	if r.width == 0 || r.height == 0 {
		return i18n.Current.BenchResolutionSaved
	}
	return fmt.Sprintf("%dx%d", r.width, r.height)
}

var benchResolutions = []benchResolution{{0, 0}, {1280, 720}, {1920, 1080}, {2560, 1440}, {3840, 2160}}

func benchResolutionLabels() []string {
	var out []string
	for _, r := range benchResolutions {
		out = append(out, r.label())
	}
	return out
}

// showBenchStyledDialog shows body in the Add Connection / Video chrome
// (accent bar, left title, corner X, hairline footer). footerButtons is
// the row pinned under the body (Cancel left / Apply right).
func showBenchStyledDialog(parent fyne.Window, title string, body, footerButtons fyne.CanvasObject,
	sizeFn func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size, onClose func()) *widget.PopUp {
	return showBenchStyledDialogEx(parent, title, body, footerButtons, sizeFn, onClose, false)
}

func showBenchStyledDialogEx(parent fyne.Window, title string, body, footerButtons fyne.CanvasObject,
	sizeFn func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size, onClose func(), tightFooter bool) *widget.PopUp {
	var popup *widget.PopUp
	closeOnce := func() {
		if onClose != nil {
			onClose()
			onClose = nil
		}
		if popup != nil {
			popup.Hide()
		}
	}
	var panel fyne.CanvasObject
	if tightFooter {
		panel = view.AssembleBrandDialogChromeTightFooter(title, body, footerButtons, closeOnce)
	} else {
		panel = view.AssembleBrandDialogChrome(title, body, footerButtons, closeOnce)
	}
	popup = view.ShowOverlayPopup(parent, view.OverlayPopupSpec{
		Panel:     panel,
		DimColor:  color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: sizeFn,
		PanelPos: func(canvasSize fyne.Size, panelSize fyne.Size) fyne.Position {
			x := (canvasSize.Width - panelSize.Width) / 2
			if view.UseCompactLayout(canvasSize.Width) {
				return fyne.NewPos(x, view.CompactOverlayTopMargin(canvasSize))
			}
			return fyne.NewPos(x, (canvasSize.Height-panelSize.Height)/2)
		},
	})
	return popup
}

// benchResultsSizeFn sizes the results panel to most of the window
// regardless of its (large, scrollable) content's own MinSize -- matches
// the fyne dialog.NewCustom-based version's previous d.Resize(sz*0.94).
func benchResultsSizeFn(canvasSize fyne.Size, _ fyne.CanvasObject) fyne.Size {
	if view.UseCompactLayout(canvasSize.Width) {
		inset := theme.InnerPadding() + 6
		if inset < 12 {
			inset = 12
		}
		top := view.CompactOverlayTopMargin(canvasSize)
		return fyne.NewSize(
			maxFloat32(1, canvasSize.Width-inset*2),
			maxFloat32(1, canvasSize.Height-top-6),
		)
	}
	return fyne.NewSize(canvasSize.Width*0.94, canvasSize.Height*0.94)
}

func benchResultsCompact(w fyne.Window) bool {
	if w == nil || w.Canvas() == nil {
		return view.UseCompactLayout(0)
	}
	return view.UseCompactLayout(w.Canvas().Size().Width)
}

func (mw *MainWindow) setBenchmarkFooterBusy(on bool, hint string) {
	for _, s := range mw.benchmarkFooterHints {
		if s == nil {
			continue
		}
		if hint != "" {
			s.SetHint(hint)
		}
		if on {
			s.Start()
			continue
		}
		s.Stop()
	}
}

func (mw *MainWindow) startBenchmark(backends []string, monitor, codec string, width, height int, window time.Duration) {
	// No progress popup: the setup dialog closes on Start and nothing else
	// opens until the results. Any Fyne overlay over the stream hides the
	// native video (black picture on Windows, see
	// VideoWidget.syncCanvasOverlayHidden) -- exactly the stream being
	// measured -- so progress goes to the Net Graph HUD, which the
	// benchmark turns on anyway, and a Control-footer spinner like Devices'
	// "connecting device" chip.
	ctx, cancel := context.WithCancel(context.Background())
	fyne.Do(func() { mw.setBenchmarkFooterBusy(true, i18n.Current.BenchFooterBusy) })

	go func() {
		res, err := benchmarkRunFn(mw, ctx, backends, monitor, codec, width, height, window, func(text string, f float64) {
			service.SetNetGraphBanner(fmt.Sprintf("%s  %.0f%%", text, math.Min(math.Max(f, 0), 1)*100))
			hint := strings.TrimSpace(text)
			if hint == "" {
				hint = i18n.Current.BenchFooterBusy
			}
			fyne.Do(func() { mw.setBenchmarkFooterBusy(true, hint) })
		})
		service.SetNetGraphBanner("")
		benchmarkBusy.Store(false)
		fyne.Do(func() { mw.setBenchmarkFooterBusy(false, "") })
		cancelled := ctx.Err() != nil
		cancel()
		if cancelled {
			return
		}
		if err != nil {
			fyne.Do(func() { view.ShowErrorDialog(fmt.Errorf("%s: %v", i18n.Current.BenchFailed, err), mw.window) })
			return
		}
		dir, err := saveBenchmarkResultFn(res)
		if err != nil {
			logrus.Warnf("📈 [Benchmark] saving results: %v", err)
		}
		for _, m := range res.Metrics {
			// AvgFPS/Low1FPS count frames handed to the decoder (delivery),
			// not frames actually drawn to the screen -- a renderer-side
			// stall (e.g. the display link itself ticking slower than the
			// decoder feeds it) can leave this number looking fine while the
			// viewer sees a slideshow. RenderFPS is the real presented-frame
			// counter; call it out here too, not just in the results table,
			// so that gap is visible at a glance in the log.
			renderPart := "render fps n/a"
			if m.RenderValid {
				renderPart = fmt.Sprintf("render %.1f fps", m.RenderFPS)
			}
			logrus.Infof("📈 [Benchmark] %s: startup %.0f+%.0fms, %.1f fps (1%% low %.1f, %s), %d stalls (%d loss/%d host/%d net), host %.1fms p95 %.1fms, net jitter p95 %.1fms, rtt %.1fms, loss %.2f%%, recovery avg %.0fms (idr %d, rfi %d)",
				m.Backend, m.SwitchMs, m.StartupMs, m.AvgFPS, m.Low1FPS, renderPart, m.StallCount, m.StallLoss, m.StallHost, m.StallNetwork,
				m.HostLatencyAvg, m.HostLatencyP95, m.NetJitterP95, m.RTTAvg, m.PacketLossPct, m.RecoveryAvgMs, m.RecoveredByIDR, m.RecoveredByRFI)
		}
		fyne.Do(func() { mw.showBenchmarkResults(res, dir) })
	}()
}

type benchmarkRow struct {
	section string
	label   string
	value   func(m service.BenchMetrics) (float64, bool)
	format  string
	lower   bool // lower is better
}

func benchmarkRows() []benchmarkRow {
	always := func(f func(service.BenchMetrics) float64) func(service.BenchMetrics) (float64, bool) {
		return func(m service.BenchMetrics) (float64, bool) { return f(m), true }
	}
	host := func(f func(service.BenchMetrics) float64) func(service.BenchMetrics) (float64, bool) {
		return func(m service.BenchMetrics) (float64, bool) { return f(m), m.HostTimingOK }
	}
	load := func(f func(service.BenchMetrics) float64) func(service.BenchMetrics) (float64, bool) {
		return func(m service.BenchMetrics) (float64, bool) { return f(m), m.HostLoadValid }
	}
	return []benchmarkRow{
		{"", "Codec", func(m service.BenchMetrics) (float64, bool) { return 0, m.Codec != "" }, "", true},

		{"Startup (measured separately)", "Host streamer switch", always(func(m service.BenchMetrics) float64 { return m.SwitchMs / 1000 }), "%.2f s", true},
		{"", "  stop previous streamer", func(m service.BenchMetrics) (float64, bool) { return m.StopMs / 1000, m.StopMs+m.StartMs > 0 }, "%.2f s", true},
		{"", "  start this streamer", func(m service.BenchMetrics) (float64, bool) { return m.StartMs / 1000, m.StopMs+m.StartMs > 0 }, "%.2f s", true},
		{"", "Stream start → first frame", always(func(m service.BenchMetrics) float64 { return m.StartupMs / 1000 }), "%.2f s", true},
		{"", "Total", always(func(m service.BenchMetrics) float64 { return (m.SwitchMs + m.StartupMs) / 1000 }), "%.2f s", true},

		{"Smoothness (what you see)", "Average fps", always(func(m service.BenchMetrics) float64 { return m.AvgFPS }), "%.1f", false},
		{"", "1% low fps", always(func(m service.BenchMetrics) float64 { return m.Low1FPS }), "%.1f", false},
		{"", "Frame time p50 / p95", always(func(m service.BenchMetrics) float64 { return m.IntervalP95 }), "", true},
		{"", "Frame time p99", always(func(m service.BenchMetrics) float64 { return m.IntervalP99 }), "%.1f ms", true},
		{"", "Worst frame time", always(func(m service.BenchMetrics) float64 { return m.IntervalMax }), "%.0f ms", true},
		{"", "Frame time std-dev", always(func(m service.BenchMetrics) float64 { return m.IntervalStd }), "%.2f ms", true},
		{"", "Stalls", always(func(m service.BenchMetrics) float64 { return float64(m.StallCount) }), "%.0f", true},
		{"", "Time frozen", always(func(m service.BenchMetrics) float64 { return m.StallTotalMs }), "%.0f ms", true},
		{"", "Hitches (>2 frames)", always(func(m service.BenchMetrics) float64 { return float64(m.Hitches) }), "%.0f", true},
		{"", "Client render fps", func(m service.BenchMetrics) (float64, bool) { return m.RenderFPS, m.RenderValid }, "%.1f", false},

		{"Host (capture + encode)", "Encode time avg", always(func(m service.BenchMetrics) float64 { return m.HostLatencyAvg }), "%.2f ms", true},
		{"", "Encode time p95", always(func(m service.BenchMetrics) float64 { return m.HostLatencyP95 }), "%.2f ms", true},
		{"", "Encode time max", always(func(m service.BenchMetrics) float64 { return m.HostLatencyMax }), "%.1f ms", true},
		{"", "Capture rate", host(func(m service.BenchMetrics) float64 { return m.HostFPS }), "%.1f fps", false},
		{"", "Capture pacing std-dev", host(func(m service.BenchMetrics) float64 { return m.HostCadenceStd }), "%.2f ms", true},
		{"", "Longest capture gap", host(func(m service.BenchMetrics) float64 { return m.HostCadenceMax }), "%.0f ms", true},
		{"", "Stalls caused by host", always(func(m service.BenchMetrics) float64 { return float64(m.StallHost) }), "%.0f", true},
		{"", "Bitrate", always(func(m service.BenchMetrics) float64 { return m.BitrateMbps }), "%.1f Mbps", false},
		{"", "Keyframes (IDR)", always(func(m service.BenchMetrics) float64 { return float64(m.IDRFrames) }), "%.0f", true},

		{"Host load (whole run, agent counters)", "GPU 3D, streamer", load(func(m service.BenchMetrics) float64 { return m.Streamer3DAvg }), "%.1f %%", true},
		{"", "GPU video encode, streamer", load(func(m service.BenchMetrics) float64 { return m.StreamerEncodeAvg }), "%.1f %%", true},
		{"", "GPU video decode, streamer", load(func(m service.BenchMetrics) float64 { return m.StreamerDecodeAvg }), "%.1f %%", true},
		{"", "CPU, streamer (all cores)", load(func(m service.BenchMetrics) float64 { return m.StreamerCPUAvg }), "%.1f %%", true},
		{"", "GPU 3D, whole host", load(func(m service.BenchMetrics) float64 { return m.GPU3DAvg }), "%.1f %%", true},
		{"", "GPU video encode, whole host", load(func(m service.BenchMetrics) float64 { return m.GPUEncodeAvg }), "%.1f %%", true},
		{"", "GPU video decode, whole host", load(func(m service.BenchMetrics) float64 { return m.GPUDecodeAvg }), "%.1f %%", true},
		{"", "CPU, whole host", load(func(m service.BenchMetrics) float64 { return m.HostCPUAvg }), "%.1f %%", true},

		{"Network", "RTT avg", always(func(m service.BenchMetrics) float64 { return m.RTTAvg }), "%.1f ms", true},
		{"", "RTT max", always(func(m service.BenchMetrics) float64 { return m.RTTMax }), "%.1f ms", true},
		{"", "Network jitter avg", host(func(m service.BenchMetrics) float64 { return m.NetJitterAvg }), "%.2f ms", true},
		{"", "Network jitter p95", host(func(m service.BenchMetrics) float64 { return m.NetJitterP95 }), "%.2f ms", true},
		{"", "Frame transfer avg", always(func(m service.BenchMetrics) float64 { return m.TransferAvg }), "%.2f ms", true},
		{"", "Frame transfer p95", always(func(m service.BenchMetrics) float64 { return m.TransferP95 }), "%.2f ms", true},
		{"", "Packet loss (incl. FEC-repaired)", always(func(m service.BenchMetrics) float64 { return m.PacketLossPct }), "%.3f %%", true},
		{"", "FEC failed", always(func(m service.BenchMetrics) float64 { return float64(m.FecFailed) }), "%.0f", true},
		{"", "Stalls caused by network", always(func(m service.BenchMetrics) float64 { return float64(m.StallNetwork) }), "%.0f", true},

		{"Recovery from lost frames", "Loss events", always(func(m service.BenchMetrics) float64 { return float64(m.LossEvents) }), "%.0f", true},
		{"", "Frames lost", always(func(m service.BenchMetrics) float64 { return float64(m.LostFrames) }), "%.0f", true},
		{"", "Recovery time avg", func(m service.BenchMetrics) (float64, bool) { return m.RecoveryAvgMs, m.LossEvents > 0 }, "%.0f ms", true},
		{"", "Recovery time max", func(m service.BenchMetrics) (float64, bool) { return m.RecoveryMaxMs, m.LossEvents > 0 }, "%.0f ms", true},
		{"", "Recovered by IDR / RFI", always(func(m service.BenchMetrics) float64 { return float64(m.RecoveredByIDR) }), "", true},
	}
}

// benchBestColor marks the best value of a row or summary tile.
var benchBestColor = color.NRGBA{R: 0x4a, G: 0xd6, B: 0x6d, A: 0xff}

func benchText(s string, c color.Color, size float32, bold bool) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size
	t.TextStyle.Bold = bold
	return t
}

func benchEllipsize(s string, size, maxW float32, col color.Color) *canvas.Text {
	t := canvas.NewText(s, col)
	t.TextSize = size
	if maxW <= 8 || t.MinSize().Width <= maxW {
		return t
	}
	runes := []rune(s)
	ell := "…"
	lo, hi := 1, len(runes)
	best := ell
	for lo <= hi {
		mid := (lo + hi) / 2
		t.Text = string(runes[:mid]) + ell
		if t.MinSize().Width <= maxW {
			best = t.Text
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	t.Text = best
	return t
}

// benchCard is a rounded surface panel with an optional colored outline.
func benchCard(body fyne.CanvasObject, outline color.Color) fyne.CanvasObject {
	return benchCardPad(body, outline, 14, 14, 12, 12)
}

func benchCardPad(body fyne.CanvasObject, outline color.Color, left, right, top, bottom float32) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorSurface)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = outline
	border.StrokeWidth = 1
	return container.NewStack(bg, view.NewInsetExact(body, left, right, top, bottom), border)
}

// benchTightVBox stacks children with a fixed gap and no Fyne theme padding.
type benchTightVBox struct{ gap float32 }

func (l *benchTightVBox) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var h float32
	n := 0
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		h += benchLaidHeight(o, 0)
		n++
	}
	if n > 1 {
		h += l.gap * float32(n-1)
	}
	return fyne.NewSize(0, h)
}

func (l *benchTightVBox) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		h := benchLaidHeight(o, size.Width)
		o.Resize(fyne.NewSize(size.Width, h))
		o.Move(fyne.NewPos(0, y))
		y += h + l.gap
	}
}

func benchLaidHeight(o fyne.CanvasObject, width float32) float32 {
	if c, ok := o.(*fyne.Container); ok {
		if l, ok := c.Layout.(*benchChartFill); ok {
			w := width
			if w < 1 {
				w = l.guessW
			}
			if w < 1 {
				w = 280
			}
			return w * l.aspect
		}
	}
	return o.MinSize().Height
}

// benchChartFill sizes a chart image to the full allocated width, keeping
// the PNG's aspect so stacked plots stay readable.
type benchChartFill struct {
	aspect float32
	guessW float32
}

func (l *benchChartFill) MinSize([]fyne.CanvasObject) fyne.Size {
	w := l.guessW
	if w < 1 {
		w = 280
	}
	return fyne.NewSize(0, w*l.aspect)
}

func (l *benchChartFill) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	h := size.Width * l.aspect
	if h < 1 {
		h = 1
	}
	for _, o := range objects {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(fyne.NewSize(size.Width, h))
	}
}

// benchShrinkCols lays out n columns whose MinSize width is one cell, so a
// phone card can shrink instead of GridWithColumns forcing n× widest tile.
type benchShrinkCols struct {
	cols int
	gap  float32
}

func (l *benchShrinkCols) MinSize(objects []fyne.CanvasObject) fyne.Size {
	cols := l.cols
	if cols < 1 {
		cols = 1
	}
	var maxH float32
	n := 0
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		n++
		m := o.MinSize()
		if m.Height > maxH {
			maxH = m.Height
		}
	}
	rows := (n + cols - 1) / cols
	if rows < 1 {
		rows = 1
	}
	h := maxH * float32(rows)
	if rows > 1 {
		h += l.gap * float32(rows-1)
	}
	return fyne.NewSize(0, h)
}

func (l *benchShrinkCols) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	cols := l.cols
	if cols < 1 {
		cols = 1
	}
	vis := make([]fyne.CanvasObject, 0, len(objects))
	for _, o := range objects {
		if o != nil && o.Visible() {
			vis = append(vis, o)
		}
	}
	n := len(vis)
	if n == 0 {
		return
	}
	rows := (n + cols - 1) / cols
	rowH := make([]float32, rows)
	for i, o := range vis {
		r := i / cols
		if h := o.MinSize().Height; h > rowH[r] {
			rowH[r] = h
		}
	}
	colW := (size.Width - l.gap*float32(cols-1)) / float32(cols)
	if colW < 1 {
		colW = 1
	}
	y := float32(0)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= n {
				break
			}
			vis[i].Resize(fyne.NewSize(colW, rowH[r]))
			vis[i].Move(fyne.NewPos(float32(c)*(colW+l.gap), y))
		}
		y += rowH[r] + l.gap
	}
}

// benchSummaryTile is one metric in a streamer's summary card: a large
// value over a small caption, the value green when it's the best of all
// streamers.
type benchSummaryTile struct {
	caption string
	value   func(m service.BenchMetrics) (float64, bool)
	format  string
	lower   bool
}

var benchSummaryTiles = []benchSummaryTile{
	{"avg fps", func(m service.BenchMetrics) (float64, bool) { return m.AvgFPS, true }, "%.1f", false},
	{"1% low fps", func(m service.BenchMetrics) (float64, bool) { return m.Low1FPS, true }, "%.1f", false},
	{"stalls", func(m service.BenchMetrics) (float64, bool) { return float64(m.StallCount), true }, "%.0f", true},
	{"encode avg", func(m service.BenchMetrics) (float64, bool) { return m.HostLatencyAvg, true }, "%.1f ms", true},
	{"bitrate", func(m service.BenchMetrics) (float64, bool) { return m.BitrateMbps, true }, "%.1f Mbps", false},
	{"startup", func(m service.BenchMetrics) (float64, bool) { return (m.SwitchMs + m.StartupMs) / 1000, true }, "%.1f s", true},
}

// benchSummaryCards builds one card per streamer with its headline numbers
// side by side, so the comparison reads at a glance before the full table.
func benchSummaryCards(metrics []service.BenchMetrics, compact bool) fyne.CanvasObject {
	best := make([][]bool, len(benchSummaryTiles))
	for t, tile := range benchSummaryTiles {
		vals := make([]float64, len(metrics))
		oks := make([]bool, len(metrics))
		for i, m := range metrics {
			vals[i], oks[i] = tile.value(m)
			oks[i] = oks[i] && m.Error == ""
		}
		best[t] = benchmarkBestMask(vals, oks, tile.lower, tile.format)
	}
	titleSize, valueSize, capSize := float32(17), float32(22), float32(11)
	tileCols := 3
	if compact {
		titleSize, valueSize, capSize = 13, 15, 9
		tileCols = 2
	}
	var cards []fyne.CanvasObject
	for i, m := range metrics {
		accent := service.BenchBackendColor(m.Backend)
		title := benchText(service.BenchBackendLabel(m.Backend), accent, titleSize, true)
		sub := ""
		if m.Codec != "" {
			sub = strings.ToUpper(m.Codec)
		}
		var head fyne.CanvasObject
		if compact {
			parts := []fyne.CanvasObject{title}
			if sub != "" {
				parts = append(parts, benchText(sub, design.ColorTextMuted, 9, false))
			}
			head = container.New(&benchTightVBox{gap: 1}, parts...)
		} else {
			head = container.NewBorder(nil, nil, title, benchText(sub, design.ColorTextMuted, 12, false))
		}
		cardPadL, cardPadR, cardPadT, cardPadB := float32(14), float32(14), float32(12), float32(12)
		if compact {
			cardPadL, cardPadR, cardPadT, cardPadB = 8, 8, 6, 6
		}
		if m.Error != "" {
			msg := widget.NewLabel(m.Error)
			msg.Wrapping = fyne.TextWrapWord
			cards = append(cards, benchCardPad(container.New(&benchTightVBox{gap: 4}, head, benchText("failed", design.ColorDanger, valueSize, true), msg), accent, cardPadL, cardPadR, cardPadT, cardPadB))
			continue
		}
		var tiles []fyne.CanvasObject
		for t, tile := range benchSummaryTiles {
			v, ok := tile.value(m)
			s := "n/a"
			if ok {
				s = fmt.Sprintf(tile.format, v)
			}
			c := color.Color(design.ColorTextLight)
			if best[t] != nil && best[t][i] {
				c = benchBestColor
			}
			val := benchText(s, c, valueSize, true)
			cap := benchText(tile.caption, design.ColorTextMuted, capSize, false)
			if compact {
				tiles = append(tiles, container.New(&benchTightVBox{gap: 0}, val, cap))
			} else {
				tiles = append(tiles, container.NewVBox(val, cap))
			}
		}
		var grid fyne.CanvasObject
		if compact {
			grid = container.New(&benchShrinkCols{cols: tileCols, gap: 4}, tiles...)
		} else {
			grid = container.NewGridWithColumns(tileCols, tiles...)
		}
		innerGap := float32(8)
		if compact {
			innerGap = 4
		}
		cards = append(cards, benchCardPad(container.New(&benchTightVBox{gap: innerGap}, head, grid), accent, cardPadL, cardPadR, cardPadT, cardPadB))
	}
	if compact || len(cards) <= 1 {
		gap := float32(theme.Padding())
		if compact {
			gap = 6
		}
		return container.New(&benchTightVBox{gap: gap}, cards...)
	}
	return container.NewGridWithColumns(len(cards), cards...)
}

// benchResultsTable is the full per-metric comparison: section header
// rows, then striped metric rows with the best value in green.
func benchResultsTable(metrics []service.BenchMetrics, compact bool, wrap float32) fyne.CanvasObject {
	if compact {
		return benchResultsTableCompact(metrics, wrap)
	}
	cols := 1 + len(metrics)
	row := func(cells []fyne.CanvasObject, fill color.Color) fyne.CanvasObject {
		grid := container.NewGridWithColumns(cols, cells...)
		if fill == nil {
			return view.NewInset(grid, 8, 8, 3, 3)
		}
		bg := canvas.NewRectangle(fill)
		bg.CornerRadius = 4
		return container.NewStack(bg, view.NewInset(grid, 8, 8, 3, 3))
	}

	head := []fyne.CanvasObject{benchText("", design.ColorTextLight, 13, true)}
	for _, m := range metrics {
		head = append(head, benchText(service.BenchBackendLabel(m.Backend), service.BenchBackendColor(m.Backend), 13, true))
	}
	rows := []fyne.CanvasObject{row(head, nil)}
	stripe := 0
	for _, r := range benchmarkRows() {
		if r.section != "" {
			rows = append(rows, view.NewInset(benchText(r.section, design.ColorAccent, 13, true), 8, 8, 12, 2), widget.NewSeparator())
			stripe = 0
		}
		cells := []fyne.CanvasObject{benchText(r.label, design.ColorTextMuted, 13, false)}
		vals, oks, bestMask := benchRowValues(metrics, r)
		for i, m := range metrics {
			s := benchRowCell(r, m, vals[i], oks[i])
			c := color.Color(design.ColorTextLight)
			win := bestMask[i]
			if win {
				c = benchBestColor
			}
			cells = append(cells, benchText(s, c, 13, win))
		}
		var fill color.Color
		if stripe%2 == 1 {
			fill = design.ColorAlphaWhite07
		}
		stripe++
		rows = append(rows, row(cells, fill))
	}
	return container.NewVBox(rows...)
}

func benchResultsTableCompact(metrics []service.BenchMetrics, wrap float32) fyne.CanvasObject {
	n := len(metrics)
	if n < 1 {
		n = 1
	}
	headCells := make([]fyne.CanvasObject, 0, n)
	colW := wrap / float32(n)
	if colW < 48 {
		colW = 48
	}
	for _, m := range metrics {
		headCells = append(headCells, benchEllipsize(service.BenchBackendLabel(m.Backend), 10, colW-4, service.BenchBackendColor(m.Backend)))
	}
	rows := []fyne.CanvasObject{
		view.NewInsetExact(container.New(&benchShrinkCols{cols: n, gap: 4}, headCells...), 4, 4, 2, 2),
	}
	stripe := 0
	for _, r := range benchmarkRows() {
		if r.section != "" {
			rows = append(rows, view.NewInsetExact(benchText(r.section, design.ColorAccent, 11, true), 4, 4, 8, 1), widget.NewSeparator())
			stripe = 0
		}
		vals, oks, bestMask := benchRowValues(metrics, r)
		lbl := benchEllipsize(r.label, 10, wrap, design.ColorTextMuted)
		valsRow := make([]fyne.CanvasObject, 0, len(metrics))
		for i, m := range metrics {
			s := benchRowCell(r, m, vals[i], oks[i])
			c := color.Color(design.ColorTextLight)
			win := bestMask[i]
			if win {
				c = benchBestColor
			}
			cell := benchEllipsize(s, 12, colW-4, c)
			cell.TextStyle.Bold = win
			valsRow = append(valsRow, cell)
		}
		body := container.New(&benchTightVBox{gap: 1}, lbl, container.New(&benchShrinkCols{cols: n, gap: 4}, valsRow...))
		var fill color.Color
		if stripe%2 == 1 {
			fill = design.ColorAlphaWhite07
		}
		stripe++
		inner := view.NewInsetExact(body, 4, 4, 2, 2)
		if fill == nil {
			rows = append(rows, inner)
			continue
		}
		bg := canvas.NewRectangle(fill)
		bg.CornerRadius = 4
		rows = append(rows, container.NewStack(bg, inner))
	}
	return container.New(&benchTightVBox{gap: 0}, rows...)
}

func benchRowValues(metrics []service.BenchMetrics, r benchmarkRow) ([]float64, []bool, []bool) {
	vals := make([]float64, len(metrics))
	oks := make([]bool, len(metrics))
	for i, m := range metrics {
		vals[i], oks[i] = r.value(m)
		if m.Error != "" {
			oks[i] = false
		}
	}
	return vals, oks, benchmarkBestMask(vals, oks, r.lower, r.format)
}

func benchRowCell(r benchmarkRow, m service.BenchMetrics, v float64, ok bool) string {
	switch {
	case m.Error != "":
		return "failed"
	case !ok:
		return "n/a"
	case r.label == "Codec":
		return strings.ToUpper(m.Codec)
	case r.label == "Frame time p50 / p95":
		return fmt.Sprintf("%.1f / %.1f ms", m.IntervalP50, m.IntervalP95)
	case r.label == "Recovered by IDR / RFI":
		return fmt.Sprintf("%d / %d", m.RecoveredByIDR, m.RecoveredByRFI)
	default:
		return fmt.Sprintf(r.format, v)
	}
}

func benchStallList(metrics []service.BenchMetrics) fyne.CanvasObject {
	list := container.NewVBox()
	for _, m := range metrics {
		list.Add(benchText(service.BenchBackendLabel(m.Backend), service.BenchBackendColor(m.Backend), 13, true))
		if len(m.Stalls) == 0 {
			list.Add(benchText("   "+i18n.Current.BenchNoStalls, design.ColorTextMuted, 13, false))
		}
		for _, st := range m.Stalls {
			line := fmt.Sprintf("   %7.2fs  %5.0f ms  %s", st.AtMs/1000, st.DurationMs, st.Cause)
			if st.LostFrames > 0 {
				line += fmt.Sprintf(" (%d frames lost, recovered by %s)", st.LostFrames, strings.ToUpper(st.Recovery))
			}
			if st.Cause == service.BenchCauseHost {
				line += fmt.Sprintf(" (host captured nothing for %.0f ms)", st.HostGapMs)
			}
			list.Add(benchText(line, service.BenchCauseColor(st.Cause), 13, false))
		}
	}
	return list
}

func benchCompactWrap(w fyne.Window, extra float32) float32 {
	cw := float32(280)
	if w != nil && w.Canvas() != nil {
		cw = w.Canvas().Size().Width - extra
	}
	if cw < 180 {
		return 180
	}
	return cw
}

func (mw *MainWindow) showBenchmarkResults(res *benchmarkResult, dir string) {
	metrics := res.Metrics
	compact := benchResultsCompact(mw.window)
	wrap := benchCompactWrap(mw.window, 88)

	var bits []fyne.CanvasObject
	if len(res.Runs) > 0 && res.Runs[0].Content != "" {
		r := res.Runs[0]
		line := fmt.Sprintf("%s · %dx%d @ %d fps · %.0f s per streamer", r.Content, r.Width, r.Height, r.ExpectedFPS, res.Duration)
		if compact {
			bits = append(bits, benchEllipsize(line, 10, wrap, design.ColorTextMuted))
		} else {
			meta := widget.NewLabel(line)
			meta.Wrapping = fyne.TextWrapWord
			meta.Importance = widget.LowImportance
			bits = append(bits, meta)
		}
	}
	bits = append(bits, benchSummaryCards(metrics, compact))
	tableCard := benchResultsTable(metrics, compact, wrap)
	if compact {
		bits = append(bits, benchCardPad(tableCard, design.ColorBorder, 6, 6, 4, 4))
	} else {
		bits = append(bits, benchCard(tableCard, design.ColorBorder))
	}

	img := service.RenderBenchChart(res.Runs, metrics)
	chart := canvas.NewImageFromImage(img)
	chart.FillMode = canvas.ImageFillContain
	if compact {
		aspect := float32(img.Bounds().Dy()) / float32(img.Bounds().Dx())
		if aspect < 0.2 {
			aspect = 0.2
		}
		bits = append(bits, container.New(&benchChartFill{aspect: aspect, guessW: benchCompactWrap(mw.window, 24)}, chart))
	} else {
		w := float32(900)
		chart.SetMinSize(fyne.NewSize(w, w*float32(img.Bounds().Dy())/float32(img.Bounds().Dx())))
		bits = append(bits, benchCard(chart, design.ColorBorder))
	}

	stalls := widget.NewAccordion(widget.NewAccordionItem(i18n.Current.BenchStalls, benchStallList(metrics)))
	bits = append(bits, stalls)

	secGap := float32(theme.Padding())
	pad := float32(18)
	if compact {
		secGap = 8
		pad = 4
	}
	sections := container.New(&benchTightVBox{gap: secGap}, bits...)
	scroll := container.NewVScroll(view.NewInsetExact(sections, pad, pad, 4, 4))

	var popup *widget.PopUp
	download := view.NewDialogApplyButton(i18n.Current.BenchSaveResults, func() {
		mw.downloadBenchmarkResults(res)
	})
	closeBtn := view.NewDialogCancelButton(i18n.Current.Close, func() {
		if popup != nil {
			popup.Hide()
		}
	})
	canShare := fileExportAvailable()
	var shareBtn fyne.CanvasObject
	if canShare {
		if compact {
			shareBtn = view.NewDialogIconButton(assets.ShareIconDark, func() { mw.shareBenchmarkResults(res) })
		} else {
			shareBtn = view.NewDialogApplyButton(i18n.Current.BenchShareResults, func() { mw.shareBenchmarkResults(res) })
		}
	}
	savedPath := ""
	if dir != "" {
		savedPath = fmt.Sprintf(i18n.Current.BenchSavedTo, dir)
	}
	var footer fyne.CanvasObject
	if compact {
		dl := view.NewDialogIconButton(assets.DownloadIconDark, func() {
			mw.downloadBenchmarkResults(res)
		})
		path := benchEllipsize(savedPath, 8, wrap-36, color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff})
		var right fyne.CanvasObject = dl
		if shareBtn != nil {
			right = container.NewHBox(shareBtn, dl)
		}
		footer = container.NewBorder(nil, nil, nil, right, path)
	} else {
		saved := benchText(savedPath, color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff}, 9, false)
		controls := []fyne.CanvasObject{closeBtn, download}
		if shareBtn != nil {
			controls = []fyne.CanvasObject{closeBtn, shareBtn, download}
		}
		footer = container.NewBorder(nil, nil, saved, container.New(&view.DeviceRowControlsLayout{Gap: 12}, controls...))
	}

	popup = showBenchStyledDialogEx(mw.window, i18n.Current.BenchResultsTitle, scroll, footer, benchResultsSizeFn, nil, compact)
}

// downloadBenchmarkResults saves the results as one zip (results.json +
// chart.png) wherever the operator picks -- the auto-save location is a
// user-config directory most people never look inside.
func (mw *MainWindow) downloadBenchmarkResults(res *benchmarkResult) {
	name := "usbridge-benchmark-" + res.CreatedAt.Format("20060102_150405") + ".zip"
	title := i18n.Current.BenchSaveResults
	if fileExportAvailable() {
		mw.exportBenchmarkZip(res, name, false)
		return
	}
	if nativeSaveAvailable() {
		go func() {
			path, err := nativeSaveFile(title, name)
			if err != nil {
				fyne.Do(func() { mw.fyneSaveBenchmarkZip(res, name) })
				return
			}
			if path == "" {
				return
			}
			mw.saveBenchmarkZipFile(res, withZipExt(path))
		}()
		return
	}
	mw.fyneSaveBenchmarkZip(res, name)
}

// shareBenchmarkResults hands the zip to the system share sheet (messengers, mail).
func (mw *MainWindow) shareBenchmarkResults(res *benchmarkResult) {
	name := "usbridge-benchmark-" + res.CreatedAt.Format("20060102_150405") + ".zip"
	mw.exportBenchmarkZip(res, name, true)
}

// exportBenchmarkZip builds the zip in the app's temp (cache) dir, then either
// opens the share sheet or copies it into Downloads. Android's Fyne save dialog
// left a 0-byte file, so the bytes are written here and only copied by the OS.
func (mw *MainWindow) exportBenchmarkZip(res *benchmarkResult, name string, share bool) {
	go func() {
		fail := func(err error) {
			fyne.Do(func() {
				view.ShowErrorDialog(fmt.Errorf(i18n.Current.BenchSaveResultsFailed, err), mw.window)
			})
		}
		var buf bytes.Buffer
		if err := writeBenchmarkZip(&buf, res); err != nil {
			fail(err)
			return
		}
		tmp := filepath.Join(os.TempDir(), name)
		if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
			fail(err)
			return
		}
		if share {
			if err := shareFile(tmp, "application/zip", i18n.Current.BenchShareResults); err != nil {
				fail(err)
			}
			return
		}
		where, err := saveFileToDownloads(tmp, name, "application/zip")
		if err != nil {
			fail(err)
			return
		}
		fyne.Do(func() {
			view.ShowInfoDialog(i18n.Current.BenchResultsTitle, fmt.Sprintf(i18n.Current.BenchSaveResultsDone, where), mw.window)
		})
	}()
}

func (mw *MainWindow) fyneSaveBenchmarkZip(res *benchmarkResult, name string) {
	fd := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil || wc == nil {
			return
		}
		werr := writeBenchmarkZip(wc, res)
		if cerr := wc.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			view.ShowErrorDialog(fmt.Errorf(i18n.Current.BenchSaveResultsFailed, werr), mw.window)
			return
		}
		view.ShowInfoDialog(i18n.Current.BenchResultsTitle, fmt.Sprintf(i18n.Current.BenchSaveResultsDone, wc.URI().Name()), mw.window)
	}, mw.window)
	fd.SetFileName(name)
	fd.Show()
}

func (mw *MainWindow) saveBenchmarkZipFile(res *benchmarkResult, path string) {
	f, err := os.Create(path)
	if err != nil {
		fyne.Do(func() {
			view.ShowErrorDialog(fmt.Errorf(i18n.Current.BenchSaveResultsFailed, err), mw.window)
		})
		return
	}
	werr := writeBenchmarkZip(f, res)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	fyne.Do(func() {
		if werr != nil {
			view.ShowErrorDialog(fmt.Errorf(i18n.Current.BenchSaveResultsFailed, werr), mw.window)
			return
		}
		view.ShowInfoDialog(i18n.Current.BenchResultsTitle, fmt.Sprintf(i18n.Current.BenchSaveResultsDone, filepath.Base(path)), mw.window)
	})
}

// writeBenchmarkZip writes results.json and chart.png, the same pair
// saveBenchmarkResult stores, into one zip archive.
func writeBenchmarkZip(w io.Writer, res *benchmarkResult) error {
	zw := zip.NewWriter(w)
	data, err := json.MarshalIndent(res, "", " ")
	if err != nil {
		return err
	}
	f, err := zw.Create("results.json")
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if f, err = zw.Create("chart.png"); err != nil {
		return err
	}
	if err := png.Encode(f, service.RenderBenchChart(res.Runs, res.Metrics)); err != nil {
		return err
	}
	return zw.Close()
}

// benchmarkBestMask marks every streamer that shares the best value as
// shown on screen (rounded to the row's format). Ties — 60.04 vs 59.96
// both printing as 60.0 fps — highlight both, instead of greening only
// the first raw float.
func benchmarkBestMask(vals []float64, oks []bool, lower bool, format string) []bool {
	out := make([]bool, len(vals))
	best, n := -1, 0
	rounded := make([]float64, len(vals))
	prec := benchFormatPrecision(format)
	for i, v := range vals {
		if !oks[i] {
			continue
		}
		n++
		rounded[i] = benchRound(v, prec)
		if best < 0 || (lower && rounded[i] < rounded[best]) || (!lower && rounded[i] > rounded[best]) {
			best = i
		}
	}
	if n < 2 || best < 0 {
		return out
	}
	for i := range vals {
		if oks[i] && rounded[i] == rounded[best] {
			out[i] = true
		}
	}
	return out
}

func benchFormatPrecision(format string) int {
	i := strings.Index(format, "%.")
	if i < 0 || i+2 >= len(format) {
		return 6
	}
	d := format[i+2]
	if d >= '0' && d <= '9' {
		return int(d - '0')
	}
	return 6
}

func benchRound(v float64, prec int) float64 {
	if prec < 0 {
		prec = 0
	}
	p := math.Pow(10, float64(prec))
	return math.Round(v*p) / p
}

// benchmarkMenuAction is the gear menu's "Run benchmark" handler, nil
// where the build can't record frames (web).
func (mw *MainWindow) benchmarkMenuAction() func() {
	if !service.BenchSupported() {
		return nil
	}
	return mw.showBenchmarkDialog
}
