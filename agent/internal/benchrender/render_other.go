//go:build !windows

package benchrender

import (
	"fmt"
	"runtime"
	"time"

	"github.com/go-gl/gl/v2.1/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
)

// GLFW wants its calls on the main thread (macOS insists).
func init() { runtime.LockOSThread() }

func glColor(rgb uint32) (r, g, b float32) {
	return float32(rgb>>16&0xff) / 255, float32(rgb>>8&0xff) / 255, float32(rgb&0xff) / 255
}

// pickMonitor returns the monitor at r's origin, the primary one if none is
// there (or r is nil).
func pickMonitor(r *Rect) *glfw.Monitor {
	if r != nil {
		for _, m := range glfw.GetMonitors() {
			if x, y := m.GetPos(); x == r.X && y == r.Y {
				return m
			}
		}
		for _, m := range glfw.GetMonitors() {
			x, y := m.GetPos()
			mode := m.GetVideoMode()
			if mode != nil && r.X >= x && r.X < x+mode.Width && r.Y >= y && r.Y < y+mode.Height {
				return m
			}
		}
	}
	return glfw.GetPrimaryMonitor()
}

func run(o options) error {
	if err := glfw.Init(); err != nil {
		return fmt.Errorf("glfw: %w", err)
	}
	defer glfw.Terminate()

	mon := pickMonitor(o.rect)
	if mon == nil {
		return fmt.Errorf("no monitor")
	}
	mode := mon.GetVideoMode()
	// Matching the monitor's current mode makes this a borderless
	// fullscreen window rather than a mode switch.
	glfw.WindowHint(glfw.RedBits, mode.RedBits)
	glfw.WindowHint(glfw.GreenBits, mode.GreenBits)
	glfw.WindowHint(glfw.BlueBits, mode.BlueBits)
	glfw.WindowHint(glfw.RefreshRate, mode.RefreshRate)
	glfw.WindowHint(glfw.AutoIconify, glfw.False)
	glfw.WindowHint(glfw.Floating, glfw.True)
	win, err := glfw.CreateWindow(mode.Width, mode.Height, WindowTitle, mon, nil)
	if err != nil {
		return fmt.Errorf("window: %w", err)
	}
	defer win.Destroy()
	win.MakeContextCurrent()
	if err := gl.Init(); err != nil {
		return fmt.Errorf("opengl: %w", err)
	}
	// Paced below, not by the display: the stream may run faster than the
	// monitor this window is on.
	glfw.SwapInterval(0)
	win.SetInputMode(glfw.CursorMode, glfw.CursorHidden)
	win.SetKeyCallback(func(w *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if key == glfw.KeyEscape && action == glfw.Press {
			w.SetShouldClose(true)
		}
	})

	bgR, bgG, bgB := glColor(colorBackground)
	aR, aG, aB := glColor(colorBoxA)
	bR, bG, bB := glColor(colorBoxB)
	period := time.Second / time.Duration(o.fps)
	start := time.Now()
	next := start
	for !win.ShouldClose() {
		fw, fh := win.GetFramebufferSize()
		gl.Viewport(0, 0, int32(fw), int32(fh))
		gl.Disable(gl.SCISSOR_TEST)
		gl.ClearColor(bgR, bgG, bgB, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		gl.Enable(gl.SCISSOR_TEST)
		bx := boxes(time.Since(start).Seconds(), fw, fh)
		for i, c := range [2][3]float32{{aR, aG, aB}, {bR, bG, bB}} {
			b := bx[i]
			gl.Scissor(int32(b.x), int32(fh-b.y-b.h), int32(b.w), int32(b.h)) // GL's origin is bottom-left
			gl.ClearColor(c[0], c[1], c[2], 1)
			gl.Clear(gl.COLOR_BUFFER_BIT)
		}
		win.SwapBuffers()
		glfw.PollEvents()

		next = next.Add(period)
		if d := time.Until(next); d > 0 {
			time.Sleep(d)
		} else if d < -4*period {
			next = time.Now()
		}
	}
	return nil
}
