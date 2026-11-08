// Package benchrender is the streamer benchmark's built-in test content: a
// borderless, topmost window covering one monitor in which two boxes cross
// a flat background, drawn by the agent executable itself (started as
// "usbridge_agent bench-render ..."). A clean host needs no video player
// for the benchmark any more (ffplay, mpv and VLC were the only options,
// and a fresh Windows has none of them).
//
// It is built to cost the host next to nothing, so the benchmark measures
// the streamer rather than its test content: no decode, no filter chain,
// and every frame is still a new picture (both boxes move every frame), so
// a capture path that only wakes on real damage (DXGI Desktop Duplication)
// gets a full frame rate. On Windows each frame only repaints the boxes'
// old and new rectangles with GDI; elsewhere it is a handful of scissored
// OpenGL clears. A row of small rectangles carries the time each frame was drawn.
package benchrender

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
)

// HelperArg is the first command-line argument that makes the agent
// executable run the renderer instead of the agent.
const HelperArg = "bench-render"

// DefaultFPS is the rate the animation runs at unless a higher one is asked
// for: above most streams' rate, so the content never caps them.
const DefaultFPS = 120

// WindowTitle is the renderer's window title.
const WindowTitle = "USBridge Benchmark"

// Rect is a monitor's area in the desktop's (physical) coordinates.
type Rect struct{ X, Y, W, H int }

// Args builds the command line (after the executable) that starts the
// renderer at fps on r (nil: the primary monitor).
func Args(fps int, r *Rect) []string {
	args := []string{HelperArg, "-fps", strconv.Itoa(fps)}
	if r != nil {
		args = append(args, "-rect", fmt.Sprintf("%d,%d,%d,%d", r.X, r.Y, r.W, r.H))
	}
	return args
}

type options struct {
	fps  int
	rect *Rect
}

func parse(args []string) (options, error) {
	fs := flag.NewFlagSet(HelperArg, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fps := fs.Int("fps", DefaultFPS, "frames per second")
	rect := fs.String("rect", "", "x,y,w,h of the monitor to cover (default: the primary one)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	o := options{fps: *fps}
	if o.fps <= 0 || o.fps > 1000 {
		return options{}, fmt.Errorf("fps %d out of range", o.fps)
	}
	if *rect != "" {
		parts := strings.Split(*rect, ",")
		if len(parts) != 4 {
			return options{}, fmt.Errorf("rect %q: want x,y,w,h", *rect)
		}
		var v [4]int
		for i, s := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				return options{}, fmt.Errorf("rect %q: %w", *rect, err)
			}
			v[i] = n
		}
		if v[2] <= 0 || v[3] <= 0 {
			return options{}, fmt.Errorf("rect %q: empty", *rect)
		}
		o.rect = &Rect{v[0], v[1], v[2], v[3]}
	}
	return o, nil
}

// Run runs the renderer (args after HelperArg) until its window is closed
// or the process is killed, and returns the exit code.
func Run(args []string) int {
	o, err := parse(args)
	if err != nil {
		log.Printf("[bench-render] %v", err)
		return 2
	}
	if err := run(o); err != nil {
		log.Printf("[bench-render] %v", err)
		return 1
	}
	return 0
}

// Scene colors (0xRRGGBB).
const (
	colorBackground = 0x202830
	colorBoxA       = 0xffffff
	colorBoxB       = 0x61f0d3
)

// box is one box's rectangle in window pixels (origin top-left).
type box struct{ x, y, w, h int }

// The time barcode: a row of black and white cells between the boxes' rows, with
// the time this frame was drawn, so the client can tell how old the picture it
// decoded is (service.AnalyzeBenchRun). Guard cells 1010, 32 bits of the wall clock
// in 100 us units (most significant first), guard cell 1: the client finds the row by
// its first and last white pixel, so it reads through any scaling or letterboxing.
const (
	barcodeCells     = 37
	barcodeCellWidth = 40.0 // on the 1920x1080 canvas
	barcodeY         = 500.0
	barcodeHeight    = 80.0
)

// barcodeBits gives each cell of the barcode for the clock reading t, white = true.
func barcodeBits(t time.Time) (bits [barcodeCells]bool) {
	v := uint32(t.UnixMicro() / 100)
	bits[0], bits[2], bits[barcodeCells-1] = true, true, true
	for i := 0; i < 32; i++ {
		bits[4+i] = v>>(31-i)&1 == 1
	}
	return bits
}

// barcodeBox is cell i's rectangle in a w x h window. Cell edges are computed from
// the canvas, not from a rounded width, so the row stays exactly as wide as it should.
func barcodeBox(i, w, h int) box {
	sx, sy := float64(w)/1920, float64(h)/1080
	left := (1920 - barcodeCells*barcodeCellWidth) / 2
	x0 := int(math.Round((left + float64(i)*barcodeCellWidth) * sx))
	x1 := int(math.Round((left + float64(i+1)*barcodeCellWidth) * sx))
	return box{x0, int(math.Round(barcodeY * sy)), x1 - x0, int(math.Round(barcodeHeight * sy))}
}

// boxes places both boxes t seconds in, in a w x h window. Laid out on a
// 1920x1080 canvas and scaled: 120px boxes crossing at 1500 and 1000 px/s,
// in opposite directions on separate rows, so each moves every frame even
// at several hundred fps and they never overlap.
func boxes(t float64, w, h int) [2]box {
	sx, sy := float64(w)/1920, float64(h)/1080
	size := func(v, s float64) int { return int(math.Round(v * s)) }
	travel := 1920.0 - 120
	ax := math.Mod(t*1500, travel)
	bx := travel - math.Mod(t*1000, travel)
	return [2]box{
		{size(ax, sx), size(300, sy), size(120, sx), size(120, sy)},
		{size(bx, sx), size(660, sy), size(120, sx), size(120, sy)},
	}
}
