package service

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Streamer benchmark charts, drawn in pure Go like the Net Graph HUD so
// they look the same on every platform and can be saved as PNG next to the
// raw JSON.

var (
	benchChartBg     = color.RGBA{0x14, 0x17, 0x1a, 0xff}
	benchChartPanel  = color.RGBA{0x1c, 0x20, 0x24, 0xff}
	benchChartGrid   = color.RGBA{0x2c, 0x32, 0x38, 0xff}
	benchChartText   = color.RGBA{0xdc, 0xe2, 0xe6, 0xff}
	benchChartDim    = color.RGBA{0x86, 0x90, 0x98, 0xff}
	benchChartOK     = color.RGBA{0x4a, 0xd6, 0x6d, 0xff}
	benchChartHitch  = color.RGBA{0xff, 0xd8, 0x2a, 0xff}
	benchCauseColors = map[string]color.RGBA{
		BenchCauseLoss:    {0xff, 0x3a, 0x2a, 0xff},
		BenchCauseHost:    {0xff, 0x8c, 0x1a, 0xff},
		BenchCauseNetwork: {0x4a, 0x9d, 0xff, 0xff},
	}
)

// BenchBackendColor is the color a backend is drawn in on every chart.
func BenchBackendColor(backend string) color.RGBA {
	switch backend {
	case "sunshine":
		return color.RGBA{0xff, 0xb3, 0x47, 0xff}
	case "rustshine":
		return color.RGBA{0x3d, 0xd6, 0xc6, 0xff}
	case "punktfunk":
		return color.RGBA{0xc8, 0x5a, 0xff, 0xff}
	case "vibepollo":
		return color.RGBA{0x5a, 0x8c, 0xff, 0xff}
	}
	return color.RGBA{0xa0, 0xa0, 0xa0, 0xff}
}

// BenchBackendLabel is the display name of a backend kind.
func BenchBackendLabel(backend string) string {
	switch backend {
	case "sunshine":
		return "Sunshine"
	case "rustshine":
		return "USBridge Streamer"
	case "punktfunk":
		return "Punktfunk"
	case "vibepollo":
		return "Vibepollo"
	}
	return backend
}

// BenchCauseColor is the marker color of a stall cause.
func BenchCauseColor(cause string) color.RGBA { return benchCauseColors[cause] }

var (
	benchFaceOnce sync.Once
	benchFace     font.Face
	benchFaceBig  font.Face
)

func benchFaces() (small, big font.Face) {
	benchFaceOnce.Do(func() {
		f, err := opentype.Parse(gomedium.TTF)
		if err != nil {
			return
		}
		benchFace, _ = opentype.NewFace(f, &opentype.FaceOptions{Size: 15, DPI: 72, Hinting: font.HintingFull})
		benchFaceBig, _ = opentype.NewFace(f, &opentype.FaceOptions{Size: 20, DPI: 72, Hinting: font.HintingFull})
	})
	return benchFace, benchFaceBig
}

var benchDrawMu sync.Mutex // opentype faces are not safe for concurrent use

func benchText(img *image.RGBA, face font.Face, x, y int, s string, c color.Color) int {
	if face == nil {
		return x
	}
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
	return d.Dot.X.Round()
}

func benchFill(img *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(img, r.Intersect(img.Bounds()), image.NewUniform(c), image.Point{}, draw.Over)
}

func benchVLine(img *image.RGBA, x, y0, y1 int, c color.Color) {
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	benchFill(img, image.Rect(x, y0, x+1, y1+1), c)
}

func benchHLineDashed(img *image.RGBA, x0, x1, y int, c color.Color) {
	for x := x0; x < x1; x += 8 {
		benchFill(img, image.Rect(x, y, min(x+4, x1), y+1), c)
	}
}

// benchLine draws a 2px polyline segment.
func benchLine(img *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	dx, dy := x1-x0, y1-y0
	steps := max(benchAbs(dx), benchAbs(dy))
	if steps == 0 {
		benchFill(img, image.Rect(x0, y0, x0+2, y0+2), c)
		return
	}
	for i := 0; i <= steps; i++ {
		x := x0 + dx*i/steps
		y := y0 + dy*i/steps
		benchFill(img, image.Rect(x, y, x+2, y+2), c)
	}
}

func benchAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

const (
	benchChartW      = 1400
	benchChartMargin = 16
	benchPlotLeft    = 70 // y-axis labels
)

type benchPanel struct {
	img    *image.RGBA
	rect   image.Rectangle // plot area
	maxX   float64         // seconds
	maxY   float64
	unit   string
	yTicks []float64
}

func newBenchPanel(img *image.RGBA, y, h int, title string, maxX, maxY float64, unit string) *benchPanel {
	small, big := benchFaces()
	outer := image.Rect(benchChartMargin, y, benchChartW-benchChartMargin, y+h)
	benchFill(img, outer, benchChartPanel)
	benchText(img, big, outer.Min.X+12, outer.Min.Y+24, title, benchChartText)
	p := &benchPanel{
		img:  img,
		rect: image.Rect(outer.Min.X+benchPlotLeft, outer.Min.Y+38, outer.Max.X-16, outer.Max.Y-28),
		maxX: math.Max(maxX, 1), maxY: math.Max(maxY, 1e-6), unit: unit,
	}
	// Horizontal grid with 4 labelled lines.
	for i := 0; i <= 4; i++ {
		v := p.maxY * float64(i) / 4
		yy := p.y(v)
		benchFill(img, image.Rect(p.rect.Min.X, yy, p.rect.Max.X, yy+1), benchChartGrid)
		benchText(img, small, outer.Min.X+8, yy+5, benchFmtAxis(v)+unit, benchChartDim)
	}
	// Time axis every 5 or 10 seconds.
	step := 5.0
	if p.maxX > 90 {
		step = 10
	}
	for s := 0.0; s <= p.maxX+1e-9; s += step {
		xx := p.x(s)
		benchFill(img, image.Rect(xx, p.rect.Min.Y, xx+1, p.rect.Max.Y), benchChartGrid)
		benchText(img, small, xx-8, p.rect.Max.Y+18, fmt.Sprintf("%.0fs", s), benchChartDim)
	}
	return p
}

func (p *benchPanel) x(sec float64) int {
	return p.rect.Min.X + int(sec/p.maxX*float64(p.rect.Dx()-1))
}

func (p *benchPanel) y(v float64) int {
	if v > p.maxY {
		v = p.maxY
	}
	if v < 0 {
		v = 0
	}
	return p.rect.Max.Y - 1 - int(v/p.maxY*float64(p.rect.Dy()-1))
}

func benchFmtAxis(v float64) string {
	if v >= 10 || v == 0 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

// RenderBenchChart draws the comparison of all runs: per-streamer frame
// timelines with every stall marked by cause, then overlaid fps, host
// encode time, network jitter/RTT, and startup times.
func RenderBenchChart(runs []*BenchRun, metrics []BenchMetrics) *image.RGBA {
	benchDrawMu.Lock()
	defer benchDrawMu.Unlock()
	small, big := benchFaces()

	maxSec := 1.0
	for _, r := range runs {
		if r != nil && r.EndUs > r.StartUs {
			maxSec = math.Max(maxSec, float64(r.EndUs-r.StartUs)/1e6)
		}
	}
	const timelineH, panelH, gap = 230, 240, 14
	loadPanels := 0
	for _, r := range runs {
		if r != nil && len(r.HostLoad) > 0 {
			loadPanels = 2
		}
	}
	h := benchChartMargin + 44 + len(runs)*(timelineH+gap) + (4+loadPanels)*(panelH+gap) + 190
	img := image.NewRGBA(image.Rect(0, 0, benchChartW, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(benchChartBg), image.Point{}, draw.Src)

	// Legend.
	y := benchChartMargin + 22
	x := benchText(img, big, benchChartMargin, y, "Streamer benchmark", benchChartText) + 30
	for _, r := range runs {
		c := BenchBackendColor(r.Backend)
		benchFill(img, image.Rect(x, y-12, x+14, y+2), c)
		x = benchText(img, small, x+20, y, BenchBackendLabel(r.Backend), benchChartText) + 24
	}
	x += 20
	for _, it := range []struct{ label, cause string }{{"lost frames", BenchCauseLoss}, {"host stall", BenchCauseHost}, {"network stall", BenchCauseNetwork}} {
		benchFill(img, image.Rect(x, y-12, x+14, y+2), benchCauseColors[it.cause])
		x = benchText(img, small, x+20, y, it.label, benchChartDim) + 22
	}
	y += 22

	// 1. Frame timelines, one per streamer, same scale.
	for i, r := range runs {
		m := metrics[i]
		title := fmt.Sprintf("%s -- frame time (every frame; stalls >= %.0fms marked)", BenchBackendLabel(r.Backend), m.StallThreshMs)
		p := newBenchPanel(img, y, timelineH, title, maxSec, 120, "ms")
		ivs := BenchIntervals(r)
		expected := m.IntervalP50
		for _, iv := range ivs {
			c := benchChartOK
			switch {
			case iv.IntervalMs >= m.StallThreshMs:
				c = benchCauseColors[BenchCauseNetwork]
			case expected > 0 && iv.IntervalMs > 2*expected:
				c = benchChartHitch
			}
			benchVLine(img, p.x(iv.AtMs/1000), p.y(0), p.y(iv.IntervalMs), c)
		}
		for _, st := range m.Stalls {
			c := benchCauseColors[st.Cause]
			x0, x1 := p.x(st.AtMs/1000), p.x((st.AtMs+st.DurationMs)/1000)
			benchFill(img, image.Rect(x0, p.rect.Min.Y, max(x1, x0+3), p.rect.Min.Y+8), c)
			benchVLine(img, x0, p.y(0), p.y(st.DurationMs), c)
			benchVLine(img, x0+1, p.y(0), p.y(st.DurationMs), c)
		}
		benchHLineDashed(img, p.rect.Min.X, p.rect.Max.X, p.y(m.StallThreshMs), benchCauseColors[BenchCauseLoss])
		summary := fmt.Sprintf("%.1f fps avg, %.1f 1%%-low, %d stalls (%d loss / %d host / %d network), %d hitches",
			m.AvgFPS, m.Low1FPS, m.StallCount, m.StallLoss, m.StallHost, m.StallNetwork, m.Hitches)
		benchText(img, small, p.rect.Max.X-560, p.rect.Min.Y-14, summary, BenchBackendColor(r.Backend))
		y += timelineH + gap
	}

	// 2. FPS per second.
	p := newBenchPanel(img, y, panelH, "Delivered fps (per second)", maxSec, benchMaxFPS(runs), "")
	for _, r := range runs {
		fps := BenchFPSPerSecond(r)
		c := BenchBackendColor(r.Backend)
		for i := 1; i < len(fps); i++ {
			benchLine(img, p.x(float64(i-1)+0.5), p.y(fps[i-1]), p.x(float64(i)+0.5), p.y(fps[i]), c)
		}
	}
	y += panelH + gap

	// 3. Host encode time (per-frame header), 250ms averages.
	p = newBenchPanel(img, y, panelH, "Host capture+encode time per frame (250ms average)", maxSec, benchMaxHost(metrics), "ms")
	for _, r := range runs {
		pts := benchBucketAvg(r, 0.25, func(f BenchFrame) (float64, bool) { return f.HostLatencyMs, true })
		benchPlotSeries(p, pts, BenchBackendColor(r.Backend))
	}
	y += panelH + gap

	// 4. Network-added jitter per frame (arrival spacing vs capture spacing).
	p = newBenchPanel(img, y, panelH, "Network jitter per frame |d(arrival) - d(capture)| (250ms average)", maxSec, benchMaxJitter(runs), "ms")
	for _, r := range runs {
		ivs := BenchIntervals(r)
		pts := benchBucketIv(ivs, 0.25, func(iv BenchFrameInterval) (float64, bool) { return iv.NetJitter, !math.IsNaN(iv.NetJitter) })
		benchPlotSeries(p, pts, BenchBackendColor(r.Backend))
	}
	y += panelH + gap

	// 5. RTT.
	p = newBenchPanel(img, y, panelH, "Round-trip time", maxSec, benchMaxRTT(runs), "ms")
	for _, r := range runs {
		var pts [][2]float64
		for _, t := range r.Ticks {
			if t.RTTValid && t.AtUs >= r.StartUs {
				pts = append(pts, [2]float64{float64(t.AtUs-r.StartUs) / 1e6, t.RTTMs})
			}
		}
		benchPlotSeries(p, pts, BenchBackendColor(r.Backend))
	}
	y += panelH + gap

	// 6. Host load from the agent's counters, when it sampled them.
	if loadPanels > 0 {
		p = newBenchPanel(img, y, panelH, "Streamer GPU load, % (bright: 3D, dim: video encode, dark: video decode)", maxSec, 100, "%")
		for _, r := range runs {
			c := BenchBackendColor(r.Backend)
			benchPlotSeries(p, benchLoadSeries(r, func(l BenchLoad) float64 { return l.StreamerGPU["3d"] }), c)
			benchPlotSeries(p, benchLoadSeries(r, func(l BenchLoad) float64 { return BenchGPUEncode(l.StreamerGPU) }), benchDim(c, 2))
			benchPlotSeries(p, benchLoadSeries(r, func(l BenchLoad) float64 { return BenchGPUDecode(l.StreamerGPU) }), benchDim(c, 4))
		}
		y += panelH + gap
		p = newBenchPanel(img, y, panelH, "CPU load, % of all cores (bright: whole host, dim: streamer)", maxSec, 100, "%")
		for _, r := range runs {
			c := BenchBackendColor(r.Backend)
			benchPlotSeries(p, benchLoadSeries(r, func(l BenchLoad) float64 { return l.CPU }), c)
			benchPlotSeries(p, benchLoadSeries(r, func(l BenchLoad) float64 { return l.StreamerCPU }), benchDim(c, 2))
		}
		y += panelH + gap
	}

	// 7. Startup, measured separately from everything above.
	outer := image.Rect(benchChartMargin, y, benchChartW-benchChartMargin, y+176)
	benchFill(img, outer, benchChartPanel)
	benchText(img, big, outer.Min.X+12, outer.Min.Y+24, "Startup (not part of the measurement window)", benchChartText)
	maxStart := 1.0
	for _, m := range metrics {
		maxStart = math.Max(maxStart, m.SwitchMs+m.StartupMs)
	}
	barW := float64(outer.Dx() - 130 - 440)
	by := outer.Min.Y + 44
	for _, m := range metrics {
		c := BenchBackendColor(m.Backend)
		benchText(img, small, outer.Min.X+12, by+17, BenchBackendLabel(m.Backend), c)
		x0 := outer.Min.X + 130
		sw := int(m.SwitchMs / maxStart * barW)
		st := int(m.StartupMs / maxStart * barW)
		benchFill(img, image.Rect(x0, by, x0+sw, by+24), color.RGBA{c.R / 2, c.G / 2, c.B / 2, 0xff})
		benchFill(img, image.Rect(x0+sw, by, x0+sw+st, by+24), c)
		benchText(img, small, x0+sw+st+10, by+17,
			fmt.Sprintf("switch %.1fs + first frame %.1fs = %.1fs", m.SwitchMs/1000, m.StartupMs/1000, (m.SwitchMs+m.StartupMs)/1000), benchChartText)
		by += 40
	}
	_ = small
	return img
}

func benchPlotSeries(p *benchPanel, pts [][2]float64, c color.Color) {
	for i := 1; i < len(pts); i++ {
		benchLine(p.img, p.x(pts[i-1][0]), p.y(pts[i-1][1]), p.x(pts[i][0]), p.y(pts[i][1]), c)
	}
	if len(pts) == 1 {
		benchFill(p.img, image.Rect(p.x(pts[0][0]), p.y(pts[0][1]), p.x(pts[0][0])+3, p.y(pts[0][1])+3), c)
	}
}

func benchBucketAvg(r *BenchRun, bucketSec float64, val func(BenchFrame) (float64, bool)) [][2]float64 {
	sums := map[int][2]float64{}
	maxB := 0
	for _, f := range r.Frames {
		v, ok := val(f)
		if !ok {
			continue
		}
		b := int(float64(f.SubmitUs-r.StartUs) / 1e6 / bucketSec)
		s := sums[b]
		sums[b] = [2]float64{s[0] + v, s[1] + 1}
		maxB = max(maxB, b)
	}
	return benchBucketsToPoints(sums, maxB, bucketSec)
}

func benchBucketIv(ivs []BenchFrameInterval, bucketSec float64, val func(BenchFrameInterval) (float64, bool)) [][2]float64 {
	sums := map[int][2]float64{}
	maxB := 0
	for _, iv := range ivs {
		v, ok := val(iv)
		if !ok {
			continue
		}
		b := int(iv.AtMs / 1000 / bucketSec)
		s := sums[b]
		sums[b] = [2]float64{s[0] + v, s[1] + 1}
		maxB = max(maxB, b)
	}
	return benchBucketsToPoints(sums, maxB, bucketSec)
}

func benchBucketsToPoints(sums map[int][2]float64, maxB int, bucketSec float64) [][2]float64 {
	var pts [][2]float64
	for b := 0; b <= maxB; b++ {
		if s, ok := sums[b]; ok && s[1] > 0 {
			pts = append(pts, [2]float64{(float64(b) + 0.5) * bucketSec, s[0] / s[1]})
		}
	}
	return pts
}

func benchNiceCeil(v, floor float64) float64 {
	v = math.Max(v, floor)
	for _, step := range []float64{1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000} {
		if v <= step {
			return step
		}
	}
	return math.Ceil(v/1000) * 1000
}

func benchMaxFPS(runs []*BenchRun) float64 {
	m := 0.0
	for _, r := range runs {
		for _, v := range BenchFPSPerSecond(r) {
			m = math.Max(m, v)
		}
	}
	// Multiples of 20 so the four grid lines land on round fps values.
	return math.Ceil((math.Max(m, 30)+1)/20) * 20
}

func benchMaxHost(metrics []BenchMetrics) float64 {
	m := 0.0
	for _, x := range metrics {
		m = math.Max(m, x.HostLatencyP95*1.5)
	}
	return benchNiceCeil(m, 5)
}

func benchMaxJitter(runs []*BenchRun) float64 {
	m := 0.0
	for _, r := range runs {
		var v []float64
		for _, iv := range BenchIntervals(r) {
			if !math.IsNaN(iv.NetJitter) {
				v = append(v, iv.NetJitter)
			}
		}
		if len(v) > 0 {
			sort.Float64s(v)
			m = math.Max(m, benchPercentile(v, 99))
		}
	}
	return benchNiceCeil(m, 5)
}

func benchMaxRTT(runs []*BenchRun) float64 {
	m := 0.0
	for _, r := range runs {
		for _, t := range r.Ticks {
			if t.RTTValid {
				m = math.Max(m, t.RTTMs)
			}
		}
	}
	return benchNiceCeil(m*1.2, 5)
}

// benchLoadSeries is one host-load value over the run, in seconds since
// the agent started sampling (right after the recording started).
func benchLoadSeries(r *BenchRun, val func(BenchLoad) float64) [][2]float64 {
	pts := make([][2]float64, 0, len(r.HostLoad))
	for _, l := range r.HostLoad {
		pts = append(pts, [2]float64{float64(l.AtMs) / 1000, val(l)})
	}
	return pts
}

// benchDim darkens c by div, for a series secondary to c's own.
func benchDim(c color.RGBA, div uint8) color.RGBA {
	return color.RGBA{c.R / div, c.G / div, c.B / div, 0xff}
}
