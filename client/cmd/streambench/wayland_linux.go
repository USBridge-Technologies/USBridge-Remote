//go:build linux && !android

package main

import (
	"fmt"
	"os"
	"runtime"
	"time"
	"unsafe"

	"github.com/go-gl/glfw/v3.4/glfw"

	"usbridge-client/internal/service"
)

func init() { runtime.LockOSThread() }

func setPhase(reporting bool, arm int) { service.PhaseProbeSet(reporting, arm) }
func dumpPhase(path string) int        { return service.PhaseProbeDump(path) }

// runOnWayland shows the stream as the GUI client does on native Wayland, a
// subsurface overlay on a toplevel, and runs body while GLFW's events are served
// on the main thread.
func runOnWayland(kind string, w, h int, vsync bool, body func(func() string)) {
	glfw.InitHint(glfw.PlatformHint, int(glfw.PlatformWayland))
	if err := glfw.Init(); err != nil || glfw.GetPlatform() != glfw.PlatformWayland {
		fmt.Fprintln(os.Stderr, "no native Wayland:", err)
		os.Exit(1)
	}
	defer glfw.Terminate()
	win, err := glfw.CreateWindow(w, h, "streambench", nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "window:", err)
		os.Exit(1)
	}
	win.MakeContextCurrent()
	win.SwapBuffers() // the first buffer maps the toplevel
	for i := 0; i < 20; i++ {
		glfw.PollEvents()
		time.Sleep(10 * time.Millisecond)
	}
	win.SwapBuffers()
	display := uintptr(unsafe.Pointer(glfw.GetWaylandDisplay()))
	surface := uintptr(unsafe.Pointer(win.GetWaylandWindow()))
	scale, _ := win.GetContentScale()
	pw, ph := win.GetFramebufferSize()
	var ok bool
	var stats func() string
	switch kind {
	case "gl-wl":
		ok = service.GLVideoCreateWayland(display, surface, 0, 0, pw, ph, scale, vsync)
		stats = func() string { return fmt.Sprintf("%+v", service.GLVideoGetStats()) }
	case "vk-wl":
		ok = service.VKVideoCreateWayland(display, surface, 0, 0, pw, ph, scale)
		stats = func() string { return fmt.Sprintf("%+v", service.VKVideoGetStats()) }
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "overlay failed:", kind)
		os.Exit(1)
	}
	done := make(chan struct{})
	go func() {
		body(stats)
		close(done)
	}()
	for {
		select {
		case <-done:
			if kind == "gl-wl" {
				service.GLVideoDestroy()
			} else {
				service.VKVideoDestroy()
			}
			return
		default:
			glfw.WaitEventsTimeout(0.002)
		}
	}
}
