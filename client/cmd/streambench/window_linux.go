//go:build linux && !android

package main

/*
#cgo LDFLAGS: -lX11
#include <X11/Xlib.h>
#include <stdlib.h>
static unsigned long sb_window(int w, int h) {
    XInitThreads();
    Display *d = XOpenDisplay(NULL);
    if (!d) return 0;
    Window win = XCreateSimpleWindow(d, DefaultRootWindow(d), 0, 0, w, h, 0, 0, 0);
    XStoreName(d, win, "streambench");
    XMapRaised(d, win);
    XSync(d, False);
    return (unsigned long)win;
}
*/
import "C"

import (
	"fmt"
	"time"

	"usbridge-client/internal/service"
)

// startRenderer gives the run the same on-screen path as the GUI client (X11
// overlay on a plain window), so decode takes the renderer's NV12 route and
// not the windowless RGBA one. Returns a stop function and a stats reader.
func startRenderer(kind string, w, h int, vsync bool) (func(), func() string, error) {
	xwin := uintptr(C.sb_window(C.int(w), C.int(h)))
	if xwin == 0 {
		return nil, nil, fmt.Errorf("no X11 display")
	}
	time.Sleep(200 * time.Millisecond)
	switch kind {
	case "gl":
		if !service.GLVideoCreate(xwin, 0, 0, w, h, vsync) {
			return nil, nil, fmt.Errorf("GL overlay failed")
		}
		return service.GLVideoDestroy, func() string {
			s := service.GLVideoGetStats()
			return fmt.Sprintf("%+v", s)
		}, nil
	case "vk":
		if !service.VKVideoCreate(xwin, 0, 0, w, h) {
			return nil, nil, fmt.Errorf("Vulkan overlay failed")
		}
		return service.VKVideoDestroy, func() string {
			s := service.VKVideoGetStats()
			return fmt.Sprintf("%+v", s)
		}, nil
	}
	return nil, nil, fmt.Errorf("renderer %q: want gl or vk", kind)
}
