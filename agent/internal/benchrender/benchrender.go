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
// OpenGL clears.
package benchrender

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"strconv"
	"strings"
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
