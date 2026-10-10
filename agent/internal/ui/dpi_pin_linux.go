//go:build linux && !android && !wayland

package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/sirupsen/logrus"
)

// pinLinuxDPIScale stops Fyne re-detecting the DPI scale on every window
// move (see pinDetectedDPIScale). The primary monitor's factor is used
// because that is Fyne's own fallback and, under KDE + XWayland, the output
// whose scale X11 clients are rendered at.
func (w *Window) pinLinuxDPIScale() {
	a := w.app
	// Let the initial show / placement moves settle first.
	time.AfterFunc(500*time.Millisecond, func() {
		fyne.Do(func() {
			mon := glfw.GetPrimaryMonitor()
			if mon == nil {
				return
			}
			base := glfwMonitorDetectedScale(mon)
			if !pinDetectedDPIScale(a, base) {
				return
			}
			logrus.Infof("[Scale] pinned X11 DPI scale %.2f (primary %q, %d monitors)",
				base, mon.GetName(), len(glfw.GetMonitors()))
			reloadFyneCanvasScale(a)
		})
	})
}

// glfwMonitorDetectedScale mirrors Fyne v2.7 glfw getMonitorScale /
// calculateDetectedScale (baseline 120 DPI, unrounded).
func glfwMonitorDetectedScale(mon *glfw.Monitor) float32 {
	const baselineDPI = 120.0
	widthMm, heightMm := mon.GetPhysicalSize()
	if widthMm == 60 && heightMm == 60 { // Steam Deck reports 6cm square
		return 1
	}
	mode := mon.GetVideoMode()
	if mode == nil || widthMm <= 0 {
		return 1
	}
	dpi := float32(mode.Width) / (float32(widthMm) / 25.4)
	if dpi > 1000 || dpi < 10 {
		dpi = baselineDPI
	}
	if s := dpi / baselineDPI; s > 1 {
		return s
	}
	return 1
}
