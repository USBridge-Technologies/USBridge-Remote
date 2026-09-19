//go:build linux

package remotelock

/*
#cgo LDFLAGS: -lX11
#include <X11/Xlib.h>

static int usbridgeIgnoreXError(Display *d, XErrorEvent *e) {
	(void)d;
	(void)e;
	return 0;
}

static void usbridgeXInit(void) {
	XInitThreads();
	XSetErrorHandler(usbridgeIgnoreXError);
}

static int usbridgePointerOverWindow(Display *dpy, unsigned long xid) {
	Window win = (Window)xid;
	Window root = 0, parent = 0, *children = NULL;
	unsigned int n = 0;
	Window walk = win;
	for (;;) {
		if (XQueryTree(dpy, walk, &root, &parent, &children, &n) == 0) {
			return 0;
		}
		if (children != NULL) {
			XFree(children);
			children = NULL;
		}
		if (parent == 0 || parent == root) {
			break;
		}
		walk = parent;
	}
	XWindowAttributes attr;
	if (XGetWindowAttributes(dpy, walk, &attr) == 0 || attr.map_state != IsViewable) {
		return 0;
	}
	int x = 0, y = 0;
	Window child = 0;
	if (XTranslateCoordinates(dpy, walk, root, 0, 0, &x, &y, &child) == 0) {
		return 0;
	}
	Window rroot = 0, rchild = 0;
	int rx = 0, ry = 0, wx = 0, wy = 0;
	unsigned int mask = 0;
	if (XQueryPointer(dpy, root, &rroot, &rchild, &rx, &ry, &wx, &wy, &mask) == 0) {
		return 0;
	}
	return rx >= x && ry >= y && rx < x+attr.width && ry < y+attr.height;
}
*/
import "C"

import (
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// EVIOCGRAB = _IOW('E', 0x90, int) on linux/amd64.
const eviocgrab = 0x40044590

var x11Window atomic.Uintptr

func setX11Window(xid uintptr) {
	x11Window.Store(xid)
}

type grabbedDev struct {
	path    string
	fd      int
	grabbed bool
}

var (
	linuxMu     sync.Mutex
	linuxStop   chan struct{}
	linuxActive bool
)

func setHookEnabled(on bool) {
	linuxMu.Lock()
	defer linuxMu.Unlock()
	if on {
		startLinuxLocked()
		return
	}
	stopLinuxLocked()
}

func startLinuxLocked() {
	if linuxActive {
		return
	}
	linuxStop = make(chan struct{})
	linuxActive = true
	stop := linuxStop
	go linuxLoop(stop)
	log.Printf("[remotelock] grabbing virtual evdev devices while the pointer is over this window")
}

func stopLinuxLocked() {
	if !linuxActive {
		return
	}
	close(linuxStop)
	linuxStop = nil
	linuxActive = false
}

func linuxLoop(stop <-chan struct{}) {
	C.usbridgeXInit()
	dpy := C.XOpenDisplay(nil)
	if dpy == nil {
		log.Printf("[remotelock] XOpenDisplay failed — Linux window lock needs DISPLAY")
		<-stop
		return
	}
	defer C.XCloseDisplay(dpy)

	var devs []*grabbedDev
	defer func() { closeDevs(devs) }()
	lastScan := time.Time{}
	loggedOpenFail := map[string]bool{}

	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			ungrabAll(devs)
			return
		case <-ticker.C:
		}
		if !isArmed() {
			ungrabAll(devs)
			continue
		}
		if time.Since(lastScan) > 2*time.Second {
			devs = rescanDevs(devs, loggedOpenFail)
			lastScan = time.Now()
		}
		xid := x11Window.Load()
		over := xid != 0 && C.usbridgePointerOverWindow(dpy, C.ulong(xid)) != 0
		setGrab(devs, over)
	}
}

func rescanDevs(old []*grabbedDev, loggedFail map[string]bool) []*grabbedDev {
	want := map[string]bool{}
	for _, path := range virtualEventPaths() {
		want[path] = true
	}
	kept := make([]*grabbedDev, 0, len(want))
	have := map[string]*grabbedDev{}
	for _, d := range old {
		if want[d.path] {
			kept = append(kept, d)
			have[d.path] = d
			continue
		}
		ungrab(d)
		_ = unix.Close(d.fd)
	}
	for path := range want {
		if have[path] != nil {
			continue
		}
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			if !loggedFail[path] {
				log.Printf("[remotelock] open %s: %v (need access to the streamer's /dev/input/event* node)", path, err)
				loggedFail[path] = true
			}
			continue
		}
		kept = append(kept, &grabbedDev{path: path, fd: fd})
	}
	return kept
}

func closeDevs(devs []*grabbedDev) {
	for _, d := range devs {
		ungrab(d)
		_ = unix.Close(d.fd)
	}
}

func ungrabAll(devs []*grabbedDev) {
	for _, d := range devs {
		ungrab(d)
	}
}

func setGrab(devs []*grabbedDev, on bool) {
	for _, d := range devs {
		if on {
			grab(d)
		} else {
			ungrab(d)
		}
	}
}

func grab(d *grabbedDev) {
	if d.grabbed {
		return
	}
	if err := unix.IoctlSetInt(d.fd, eviocgrab, 1); err != nil {
		return
	}
	d.grabbed = true
}

func ungrab(d *grabbedDev) {
	if !d.grabbed {
		return
	}
	_ = unix.IoctlSetInt(d.fd, eviocgrab, 0)
	d.grabbed = false
}

func virtualEventPaths() []string {
	data, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return nil
	}
	return eventPathsFromDevices(string(data))
}
