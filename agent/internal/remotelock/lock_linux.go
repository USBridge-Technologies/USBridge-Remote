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
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// EVIOCGRAB = _IOW('E', 0x90, int) on linux/amd64.
const eviocgrab = 0x40044590

// input_event / uinput bits matching agent/internal/input on linux/{amd64,arm64}.
const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03

	relWheel       = 0x08
	relHWheel      = 0x06
	relWheelHiRes  = 0x0b
	relHWheelHiRes = 0x0c

	absX = 0x00
	absY = 0x01

	uiDevCreate = 0x5501
	uiSetEvbit  = 0x40045564
	uiSetRelbit = 0x40045566
	uiSetAbsbit = 0x40045567
	uiDevSetup  = 0x405c5503
	uiAbsSetup  = 0x401c5504

	busUsb = 0x03

	// Must not match isVirtualInput name patterns or we grab our own relay.
	relayDeviceName = "usbridge-remotelock-relay"
)

var x11Window atomic.Uintptr

func setX11Window(xid uintptr) {
	x11Window.Store(xid)
}

type grabbedDev struct {
	path    string
	fd      int
	grabbed bool
}

// inputEvent matches struct input_event on 64-bit Linux (24 bytes).
type inputEvent struct {
	Sec   uint64
	Usec  uint64
	Type  uint16
	Code  uint16
	Value int32
}

type inputID struct {
	Bustype uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

type uinputSetup struct {
	ID           inputID
	Name         [80]byte
	FfEffectsMax uint32
}

type inputAbsinfo struct {
	Value      int32
	Minimum    int32
	Maximum    int32
	Fuzz       int32
	Flat       int32
	Resolution int32
}

type uinputAbsSetup struct {
	Code    uint16
	_       [2]byte
	Absinfo inputAbsinfo
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

func hookInstalled() bool {
	linuxMu.Lock()
	defer linuxMu.Unlock()
	return linuxActive
}

func startLinuxLocked() {
	if linuxActive {
		return
	}
	linuxStop = make(chan struct{})
	linuxActive = true
	stop := linuxStop
	go linuxLoop(stop)
	log.Printf("[remotelock] filtering virtual evdev buttons/keys over this window (motion still passes)")
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

	relay, err := openMotionRelay()
	if err != nil {
		// Grabbing without a relay freezes the remote pointer (EVIOCGRAB
		// swallows REL/ABS). Fail open: leave remote input alone.
		log.Printf("[remotelock] motion relay uinput unavailable (%v) — lock disabled this session", err)
		<-stop
		return
	}
	defer relay.Close()

	var devs []*grabbedDev
	defer func() { closeDevs(devs) }()
	lastScan := time.Time{}
	loggedOpenFail := map[string]bool{}

	for {
		select {
		case <-stop:
			ungrabAll(devs)
			return
		default:
		}
		if !isArmed() {
			ungrabAll(devs)
			if !sleepOrStop(stop, 40*time.Millisecond) {
				ungrabAll(devs)
				return
			}
			continue
		}
		if time.Since(lastScan) > 2*time.Second {
			devs = rescanDevs(devs, loggedOpenFail)
			lastScan = time.Now()
		}
		xid := x11Window.Load()
		over := xid != 0 && C.usbridgePointerOverWindow(dpy, C.ulong(xid)) != 0
		setGrab(devs, over)
		if !over {
			if !sleepOrStop(stop, 40*time.Millisecond) {
				ungrabAll(devs)
				return
			}
			continue
		}
		// Cursor is over the agent window: grab is exclusive, so drain often
		// and re-inject motion or the remote pointer cannot leave the window.
		until := time.Now().Add(40 * time.Millisecond)
		for time.Now().Before(until) {
			select {
			case <-stop:
				ungrabAll(devs)
				return
			default:
			}
			drainAndFilter(devs, relay)
			time.Sleep(time.Millisecond)
		}
	}
}

func sleepOrStop(stop <-chan struct{}, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-stop:
		return false
	case <-t.C:
		return true
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

// EVIOCGKEY bitmap: 768 bits covers KEY_*/BTN_* 0..0x2ff.
const evdevKeyBytes = 96

// eviocgkey = EVIOCGKEY(evdevKeyBytes) = _IOC(_IOC_READ, 'E', 0x18, 96).
const eviocgkey = 0x80000000 | evdevKeyBytes<<16 | 'E'<<8 | 0x18

var (
	grabIoctl = func(fd int, on bool) error {
		v := 0
		if on {
			v = 1
		}
		return unix.IoctlSetInt(fd, eviocgrab, v)
	}
	keysDownFn = keysDown
)

func keysDown(fd int) bool {
	var buf [evdevKeyBytes]byte
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(eviocgkey), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return true
	}
	return anyBitSet(buf[:])
}

func anyBitSet(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}

// grab takes EVIOCGRAB on d, but never while a key or button is held.
//
// Why: the compositor has already seen the press. Once we grab, the
// matching release goes only to us (we never re-inject KEY), so the
// compositor keeps the button "down" forever. Deferring is safe: setGrab
// retries every tick.
func grab(d *grabbedDev) {
	if d.grabbed {
		return
	}
	if keysDownFn(d.fd) {
		return
	}
	if err := grabIoctl(d.fd, true); err != nil {
		return
	}
	d.grabbed = true
}

func ungrab(d *grabbedDev) {
	if !d.grabbed {
		return
	}
	_ = grabIoctl(d.fd, false)
	d.grabbed = false
}

func virtualEventPaths() []string {
	data, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return nil
	}
	return eventPathsFromDevices(string(data))
}

// dropFilteredEvent is true for the same class Windows/macOS drop: buttons,
// keys, and wheel — not pointer motion. EVIOCGRAB alone would swallow REL/ABS
// too and pin the remote cursor on the agent window.
func dropFilteredEvent(evType, code uint16) bool {
	switch evType {
	case evKey:
		return true
	case evRel:
		switch code {
		case relWheel, relHWheel, relWheelHiRes, relHWheelHiRes:
			return true
		}
	}
	return false
}

func drainAndFilter(devs []*grabbedDev, relay *os.File) {
	var buf [unsafe.Sizeof(inputEvent{})]byte
	for _, d := range devs {
		if !d.grabbed {
			continue
		}
		for {
			n, err := unix.Read(d.fd, buf[:])
			if n == 0 && err == nil {
				break
			}
			if err != nil {
				if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
					break
				}
				break
			}
			if n != len(buf) {
				continue
			}
			ev := *(*inputEvent)(unsafe.Pointer(&buf[0]))
			if dropFilteredEvent(ev.Type, ev.Code) {
				continue
			}
			if _, err := relay.Write(buf[:]); err != nil {
				log.Printf("[remotelock] motion relay write failed: %v — releasing grabs", err)
				ungrabAll(devs)
				return
			}
		}
	}
}

func openMotionRelay() (*os.File, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	fd := f.Fd()
	for _, bit := range []int{evRel, evAbs} {
		if err := ioctlInt(fd, uiSetEvbit, bit); err != nil {
			f.Close()
			return nil, err
		}
	}
	for code := 0; code < 16; code++ {
		_ = ioctlInt(fd, uiSetRelbit, code)
	}
	for _, abs := range []int{absX, absY} {
		if err := ioctlInt(fd, uiSetAbsbit, abs); err != nil {
			f.Close()
			return nil, err
		}
	}
	setup := uinputSetup{
		// Distinct from Sunshine's 0xbeef/0xdead so isVirtualInput skips us.
		ID: inputID{Bustype: busUsb, Vendor: 0x1234, Product: 0x10c4, Version: 1},
	}
	copy(setup.Name[:], relayDeviceName)
	if err := ioctlPtr(fd, uiDevSetup, unsafe.Pointer(&setup)); err != nil {
		f.Close()
		return nil, err
	}
	for _, axis := range []uinputAbsSetup{
		{Code: absX, Absinfo: inputAbsinfo{Maximum: 65535}},
		{Code: absY, Absinfo: inputAbsinfo{Maximum: 65535}},
	} {
		if err := ioctlPtr(fd, uiAbsSetup, unsafe.Pointer(&axis)); err != nil {
			f.Close()
			return nil, err
		}
	}
	if err := ioctlInt(fd, uiDevCreate, 0); err != nil {
		f.Close()
		return nil, err
	}
	time.Sleep(50 * time.Millisecond)
	return f, nil
}

func ioctlInt(fd uintptr, req uintptr, val int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(val))
	if errno != 0 {
		return errno
	}
	return nil
}

func ioctlPtr(fd uintptr, req uintptr, ptr unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(ptr))
	if errno != 0 {
		return errno
	}
	return nil
}
