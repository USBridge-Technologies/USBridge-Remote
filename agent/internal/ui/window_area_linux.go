//go:build linux && !android

package ui

import (
	"fyne.io/fyne/v2"
	"github.com/go-gl/glfw/v3.4/glfw"
)

// windowDecorationAllowance leaves room for the title bar a server-side
// decorated window gets outside its canvas.
const windowDecorationAllowance = 48

// availableWindowArea is the primary monitor's work area in Fyne units, or
// a zero size when unknown. A size saved by another session can be far
// larger than this one's screen: under XWayland "apply scaling themselves"
// a 4K output is 3840 wide at Fyne scale ~1.3 (~2900 units), while native
// Wayland at 275% gives only ~1396 logical units -- restoring it unclamped
// opened the window well past both screen edges.
func availableWindowArea(c fyne.Canvas) fyne.Size {
	mon := glfw.GetPrimaryMonitor()
	if mon == nil {
		return fyne.Size{}
	}
	_, _, w, h := mon.GetWorkarea()
	if w <= 0 || h <= 0 {
		return fyne.Size{}
	}
	scale := float32(1)
	if c != nil && c.Scale() > 0 {
		scale = c.Scale()
	}
	if glfw.GetPlatform() != glfw.PlatformWayland {
		// X11: screen pixels; Fyne units are those over the canvas scale.
		return fyne.NewSize(float32(w)/scale, float32(h-windowDecorationAllowance)/scale)
	}
	// Wayland: GLFW's work area is the output mode in device pixels. Device
	// pixels per Fyne unit is the canvas scale times the (fractional) output
	// scale, which only the canvas knows once the window is on an output;
	// before that the output's integer scale (rounded up) stands in.
	px := scale
	if c != nil {
		const probe = 1000
		if x, _ := c.PixelCoordinateForPosition(fyne.NewPos(probe, 0)); x > 0 {
			px = float32(x) / probe
		}
	}
	if px <= scale*1.01 {
		if ms, _ := mon.GetContentScale(); ms > 1 {
			px = scale * ms
		}
	}
	return fyne.NewSize(float32(w)/px, float32(h)/px-windowDecorationAllowance/scale)
}
