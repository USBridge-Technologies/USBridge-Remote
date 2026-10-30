//go:build darwin

package displaypower

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework CoreGraphics
#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>
#include <dlfcn.h>
#include <stdlib.h>

// CGVirtualDisplay: CoreGraphics' private virtual monitor (macOS 11+), the one
// DeskPad and BetterDisplay use. It lives as long as the object does, in the
// process that made it.
@interface CGVirtualDisplayDescriptor : NSObject
@property(retain, nonatomic) dispatch_queue_t queue;
@property(retain, nonatomic) NSString *name;
@property(nonatomic) unsigned int maxPixelsHigh;
@property(nonatomic) unsigned int maxPixelsWide;
@property(nonatomic) CGSize sizeInMillimeters;
@property(nonatomic) unsigned int serialNum;
@property(nonatomic) unsigned int productID;
@property(nonatomic) unsigned int vendorID;
@property(copy, nonatomic) void (^terminationHandler)(id, id);
@end
@interface CGVirtualDisplayMode : NSObject
- (instancetype)initWithWidth:(unsigned int)w height:(unsigned int)h refreshRate:(double)r;
@end
@interface CGVirtualDisplaySettings : NSObject
@property(retain, nonatomic) NSArray *modes;
@property(nonatomic) unsigned int hiDPI;
@end
@interface CGVirtualDisplay : NSObject
- (instancetype)initWithDescriptor:(CGVirtualDisplayDescriptor *)d;
- (BOOL)applySettings:(CGVirtualDisplaySettings *)s;
@property(readonly, nonatomic) CGDirectDisplayID displayID;
@end

static id usbVirtual;

// usbCreateVirtual brings the virtual monitor up; 0 when CoreGraphics won't.
// The autorelease pools matter: a Go thread has none, and the display outlived
// its release, autoreleased into nothing.
static CGDirectDisplayID usbCreateVirtual(unsigned w, unsigned h, double hz, const char *name,
		unsigned vendor, unsigned product, unsigned serial) {
	@autoreleasepool {
		Class descClass = NSClassFromString(@"CGVirtualDisplayDescriptor");
		Class displayClass = NSClassFromString(@"CGVirtualDisplay");
		Class settingsClass = NSClassFromString(@"CGVirtualDisplaySettings");
		Class modeClass = NSClassFromString(@"CGVirtualDisplayMode");
		if (!descClass || !displayClass || !settingsClass || !modeClass) return 0;
		CGVirtualDisplayDescriptor *d = [[descClass alloc] init];
		d.queue = dispatch_get_global_queue(QOS_CLASS_DEFAULT, 0);
		d.name = [NSString stringWithUTF8String:name];
		d.maxPixelsWide = w;
		d.maxPixelsHigh = h;
		d.sizeInMillimeters = CGSizeMake(w * 25.4 / 109, h * 25.4 / 109); // 109 ppi
		d.vendorID = vendor;
		d.productID = product;
		d.serialNum = serial;
		d.terminationHandler = ^(id a, id b) {};
		CGVirtualDisplay *v = [[displayClass alloc] initWithDescriptor:d];
		if (!v) return 0;
		CGVirtualDisplaySettings *s = [[settingsClass alloc] init];
		s.hiDPI = 0;
		s.modes = @[[[modeClass alloc] initWithWidth:w height:h refreshRate:hz]];
		if (![v applySettings:s]) return 0;
		usbVirtual = v;
		return v.displayID;
	}
}

static CGDirectDisplayID usbVirtualID(void) {
	@autoreleasepool {
		return usbVirtual ? ((CGVirtualDisplay *)usbVirtual).displayID : 0;
	}
}

static void usbDestroyVirtual(void) {
	@autoreleasepool {
		usbVirtual = nil;
	}
}

// CGSConfigureDisplayEnabled: private (SkyLight), what System Settings itself
// has no switch for -- a monitor taken off the desktop, as if unplugged, until
// switched on again or the login session ends. -1 when the symbol is gone.
typedef CGError (*usbEnableFn)(CGDisplayConfigRef, CGDirectDisplayID, bool);

static int usbCanEnable(void) {
	return dlsym(RTLD_DEFAULT, "CGSConfigureDisplayEnabled") != NULL;
}

static CGError usbConfigureEnabled(CGDisplayConfigRef cfg, CGDirectDisplayID id, bool on) {
	usbEnableFn f = (usbEnableFn)dlsym(RTLD_DEFAULT, "CGSConfigureDisplayEnabled");
	if (!f) return -1;
	return f(cfg, id, on);
}

// usbDisplayName is the monitor's own (EDID) name, from CoreDisplay's info
// dictionary; "" when there is none.
typedef CFDictionaryRef (*usbInfoFn)(CGDirectDisplayID);

static void usbDisplayName(CGDirectDisplayID id, char *out, int n) {
	out[0] = 0;
	@autoreleasepool {
		usbInfoFn f = (usbInfoFn)dlsym(RTLD_DEFAULT, "CoreDisplay_DisplayCreateInfoDictionary");
		if (!f) return;
		CFDictionaryRef info = f(id);
		if (!info) return;
		NSDictionary *dict = CFBridgingRelease(info);
		NSDictionary *names = dict[@"DisplayProductName"];
		NSString *name = nil;
		if ([names isKindOfClass:[NSDictionary class]]) {
			name = names[@"en_US"];
			if (!name) name = names.allValues.firstObject;
		}
		if ([name isKindOfClass:[NSString class]]) {
			strlcpy(out, name.UTF8String, n);
		}
	}
}
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// On macOS the virtual monitor is a CGVirtualDisplay this process holds: one
// lives only as long as its owner, so a short-lived helper process can't make
// it, and the agent -- not the stream host -- owns the virtual monitor for
// every stream host. The helper commands therefore run in the agent process
// (streamhost's runDisplayHelper), all but `watch`, which outlives it to put
// the real monitors back. When the agent dies the monitor dies with it.
//
// The rest goes through CoreGraphics' display configuration: which monitor is
// primary is which one sits at the desktop origin, and a monitor is switched
// off with the private CGSConfigureDisplayEnabled. Both for the login session
// only (kCGConfigureForSession), so a logout or reboot gives everything back.
// A switched-off monitor drops out of CoreGraphics' lists, so the ones the
// agent switched off are kept in a file to be listed (and switched on) still.

// AgentVirtual: the agent brings the virtual monitor up and down for sessions
// itself (Windows, Linux, macOS).
const AgentVirtual = true

// The virtual monitor's EDID identity: what the stream host finds it by (its
// display ID is new every time it comes up), and how it is told from others.
const (
	virtualName    = "USBridge Virtual Display"
	virtualVendor  = 0x5553 // "US"
	virtualProduct = 0x4256 // "BV"
	virtualSerial  = 0x55534256
)

// VirtualDeviceName is the virtual monitor's name for the stream host's
// monitor_device: "VENDOR:PRODUCT:SERIAL", hex.
var VirtualDeviceName = fmt.Sprintf("%04x:%04x:%08x", virtualVendor, virtualProduct, virtualSerial)

// mu serializes the display changes: the session watchdog and the API both
// make them, in this one process.
var mu sync.Mutex

type cgDisplay struct {
	id      C.CGDirectDisplayID
	main    bool
	builtin bool
	virtual bool
	mirrors C.CGDirectDisplayID
	x, y    int
	w, h    int
}

func onlineDisplays() []cgDisplay {
	var ids [32]C.CGDirectDisplayID
	var n C.uint32_t
	if C.CGGetOnlineDisplayList(C.uint32_t(len(ids)), &ids[0], &n) != C.kCGErrorSuccess {
		return nil
	}
	out := make([]cgDisplay, 0, n)
	for _, id := range ids[:n] {
		b := C.CGDisplayBounds(id)
		out = append(out, cgDisplay{
			id:      id,
			main:    C.CGDisplayIsMain(id) != 0,
			builtin: C.CGDisplayIsBuiltin(id) != 0,
			virtual: isVirtual(id),
			mirrors: C.CGDisplayMirrorsDisplay(id),
			x:       int(b.origin.x), y: int(b.origin.y),
			w: int(b.size.width), h: int(b.size.height),
		})
	}
	return out
}

func isVirtual(id C.CGDirectDisplayID) bool {
	return C.CGDisplayVendorNumber(id) == virtualVendor && C.CGDisplayModelNumber(id) == virtualProduct &&
		C.CGDisplaySerialNumber(id) == virtualSerial
}

func displayName(d cgDisplay) string {
	if d.virtual {
		return virtualName
	}
	buf := make([]C.char, 256)
	C.usbDisplayName(d.id, &buf[0], C.int(len(buf)))
	if name := C.GoString(&buf[0]); name != "" {
		return name
	}
	if d.builtin {
		return "Built-in Display"
	}
	return fmt.Sprintf("Display %d", d.id)
}

func displayID(id string) (C.CGDirectDisplayID, error) {
	n, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return 0, ErrNotFound
	}
	return C.CGDirectDisplayID(n), nil
}

func findDisplay(ds []cgDisplay, id C.CGDirectDisplayID) (cgDisplay, bool) {
	for _, d := range ds {
		if d.id == id {
			return d, true
		}
	}
	return cgDisplay{}, false
}

// offListPath keeps the monitors the agent switched off: CoreGraphics no
// longer lists them.
func offListPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "usbridge-agent", "displays_off.json")
}

func loadOffList() []Monitor {
	data, err := os.ReadFile(offListPath())
	if err != nil {
		return nil
	}
	var list []Monitor
	_ = json.Unmarshal(data, &list)
	return list
}

func saveOffList(list []Monitor) {
	path := offListPath()
	if len(list) == 0 {
		_ = os.Remove(path)
		return
	}
	data, _ := json.Marshal(list)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("[vdisplay] keeping the switched-off monitors: %v", err)
	}
}

// listLocked reports the online monitors, then the ones the agent switched off
// that are still off. ID is the CGDirectDisplayID (stable for a real monitor).
func listLocked() []Monitor {
	online := onlineDisplays()
	var out []Monitor
	for _, d := range online {
		id := strconv.FormatUint(uint64(d.id), 10)
		out = append(out, Monitor{
			ID: id, Name: displayName(d), Active: true, GDIName: id,
			Primary: d.main, Virtual: d.virtual, Width: d.w, Height: d.h,
		})
	}
	var off []Monitor
	for _, m := range loadOffList() {
		id, err := displayID(m.ID)
		if err != nil {
			continue
		}
		if _, ok := findDisplay(online, id); ok {
			continue // on again (a logout, or switched on elsewhere)
		}
		off = append(off, m)
		out = append(out, Monitor{ID: m.ID, Name: m.Name, Width: m.Width, Height: m.Height})
	}
	if len(off) != len(loadOffList()) {
		saveOffList(off)
	}
	return out
}

// List reports every monitor, the switched-off ones the agent knows of included.
func List() ([]Monitor, error) {
	mu.Lock()
	defer mu.Unlock()
	return listLocked(), nil
}

// configure applies one display configuration change for the login session.
func configure(what string, apply func(cfg C.CGDisplayConfigRef) C.CGError) error {
	var cfg C.CGDisplayConfigRef
	if e := C.CGBeginDisplayConfiguration(&cfg); e != C.kCGErrorSuccess {
		return fmt.Errorf("%s: CGBeginDisplayConfiguration: %d", what, int(e))
	}
	if e := apply(cfg); e != C.kCGErrorSuccess {
		C.CGCancelDisplayConfiguration(cfg)
		return fmt.Errorf("%s: %d", what, int(e))
	}
	if e := C.CGCompleteDisplayConfiguration(cfg, C.kCGConfigureForSession); e != C.kCGErrorSuccess {
		return fmt.Errorf("%s: CGCompleteDisplayConfiguration: %d", what, int(e))
	}
	return nil
}

// setEnabledLocked switches monitors on or off in one go, keeping the off list.
func setEnabledLocked(ids []C.CGDirectDisplayID, on bool) error {
	if len(ids) == 0 {
		return nil
	}
	if C.usbCanEnable() == 0 {
		return ErrUnsupported
	}
	if !on {
		online := onlineDisplays()
		list := loadOffList()
		for _, id := range ids {
			d, ok := findDisplay(online, id)
			if !ok {
				continue
			}
			sid := strconv.FormatUint(uint64(id), 10)
			list = append(removeMonitor(list, sid), Monitor{ID: sid, Name: displayName(d), Width: d.w, Height: d.h})
		}
		saveOffList(list)
	}
	err := configure("switching monitors", func(cfg C.CGDisplayConfigRef) C.CGError {
		for _, id := range ids {
			if e := C.usbConfigureEnabled(cfg, id, C.bool(on)); e != C.kCGErrorSuccess {
				return e
			}
		}
		return C.kCGErrorSuccess
	})
	if err != nil {
		return err
	}
	if on {
		list := loadOffList()
		for _, id := range ids {
			list = removeMonitor(list, strconv.FormatUint(uint64(id), 10))
		}
		saveOffList(list)
		waitOnline(ids, 3*time.Second)
	}
	return nil
}

func removeMonitor(list []Monitor, id string) []Monitor {
	out := list[:0:0]
	for _, m := range list {
		if m.ID != id {
			out = append(out, m)
		}
	}
	return out
}

// waitOnline waits for monitors switched on to be part of the desktop again.
func waitOnline(ids []C.CGDirectDisplayID, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		online := onlineDisplays()
		all := true
		for _, id := range ids {
			if _, ok := findDisplay(online, id); !ok {
				all = false
			}
		}
		if all {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// SetEnabled switches one monitor on or off. Switching off the only active one
// is refused.
func SetEnabled(id string, on bool) error {
	mu.Lock()
	defer mu.Unlock()
	cid, err := displayID(id)
	if err != nil {
		return err
	}
	online := onlineDisplays()
	_, isOn := findDisplay(online, cid)
	if !on {
		if !isOn {
			return nil
		}
		drawn := 0
		for _, d := range online {
			if d.mirrors == 0 {
				drawn++
			}
		}
		if drawn <= 1 {
			return ErrLastDisplay
		}
		return setEnabledLocked([]C.CGDirectDisplayID{cid}, false)
	}
	if isOn {
		return nil
	}
	known := false
	for _, m := range loadOffList() {
		known = known || m.ID == id
	}
	if !known {
		return ErrNotFound
	}
	if err := setEnabledLocked([]C.CGDirectDisplayID{cid}, true); err != nil {
		return err
	}
	if _, ok := findDisplay(onlineDisplays(), cid); !ok {
		return ErrNotFound // unplugged while it was off
	}
	return nil
}

// makePrimaryLocked moves the monitor to the desktop origin, the others with
// it: the primary display is the one at (0,0), and moving them all alike
// keeps the arrangement as it was.
func makePrimaryLocked(id C.CGDirectDisplayID) error {
	online := onlineDisplays()
	target, ok := findDisplay(online, id)
	if !ok {
		return ErrNotFound
	}
	if target.main {
		return nil
	}
	dx, dy := target.x, target.y
	return configure("making the monitor primary", func(cfg C.CGDisplayConfigRef) C.CGError {
		for _, d := range online {
			if d.mirrors != 0 {
				continue // follows the one it mirrors
			}
			if e := C.CGConfigureDisplayOrigin(cfg, d.id, C.int32_t(d.x-dx), C.int32_t(d.y-dy)); e != C.kCGErrorSuccess {
				return e
			}
		}
		return C.kCGErrorSuccess
	})
}

// MakePrimary makes a monitor the primary display, switching it on first when
// the agent had switched it off.
func MakePrimary(id string) error {
	mu.Lock()
	defer mu.Unlock()
	cid, err := displayID(id)
	if err != nil {
		return err
	}
	if _, ok := findDisplay(onlineDisplays(), cid); !ok {
		for _, m := range loadOffList() {
			if m.ID == id {
				if err := setEnabledLocked([]C.CGDirectDisplayID{cid}, true); err != nil {
					return err
				}
			}
		}
	}
	return makePrimaryLocked(cid)
}

// placeVirtualLocked takes the virtual monitor out of any mirror set (macOS
// mirrors a new monitor onto the built-in one) and puts it to the right of the
// desktop, an extended screen.
func placeVirtualLocked(vid C.CGDirectDisplayID) error {
	online := onlineDisplays()
	right, top := 0, 0
	for _, d := range online {
		if d.id == vid || d.mirrors != 0 {
			continue
		}
		if d.x+d.w > right {
			right = d.x + d.w
		}
		if d.main {
			top = d.y
		}
	}
	return configure("placing the virtual monitor", func(cfg C.CGDisplayConfigRef) C.CGError {
		for _, d := range online {
			if d.id == vid || d.mirrors == vid {
				if e := C.CGConfigureDisplayMirrorOfDisplay(cfg, d.id, C.kCGNullDirectDisplay); e != C.kCGErrorSuccess {
					return e
				}
			}
		}
		return C.CGConfigureDisplayOrigin(cfg, vid, C.int32_t(right), C.int32_t(top))
	})
}

// AttachVirtual brings the virtual monitor up in mode m, extended to the right,
// makes it primary (remembering the primary before) and with exclusive
// switches the others off.
func AttachVirtual(m Mode, primary, exclusive bool) (*VirtualState, error) {
	mu.Lock()
	defer mu.Unlock()
	if C.usbVirtualID() != 0 {
		C.usbDestroyVirtual()
	}
	name := C.CString(virtualName)
	defer C.free(unsafe.Pointer(name))
	vid := C.usbCreateVirtual(C.uint(m.Width), C.uint(m.Height), C.double(m.Hz), name, virtualVendor, virtualProduct, virtualSerial)
	if vid == 0 {
		return nil, errors.New("CoreGraphics did not create the virtual monitor")
	}
	st := &VirtualState{GDIName: VirtualDeviceName, FriendlyName: virtualName, DevicePath: strconv.FormatUint(uint64(vid), 10), Mode: m}
	if !waitOnline([]C.CGDirectDisplayID{vid}, 5*time.Second) {
		return st, fmt.Errorf("the virtual monitor %d did not show up on the desktop", vid)
	}
	if err := placeVirtualLocked(vid); err != nil {
		log.Printf("[vdisplay] %v", err)
	}
	if primary {
		if err := st.setPrimaryLocked(true); err != nil {
			return st, err
		}
	}
	if exclusive {
		return othersOffLocked(st)
	}
	return st, nil
}

func virtualOnline() (C.CGDirectDisplayID, bool) {
	vid := C.usbVirtualID()
	if vid == 0 {
		return 0, false
	}
	_, ok := findDisplay(onlineDisplays(), vid)
	return vid, ok
}

// SetPrimary makes the attached virtual monitor the primary display (the one
// before is kept in Layout), or gives the role back.
func (st *VirtualState) SetPrimary(on bool) error {
	mu.Lock()
	defer mu.Unlock()
	return st.setPrimaryLocked(on)
}

func (st *VirtualState) setPrimaryLocked(on bool) error {
	if on {
		vid, ok := virtualOnline()
		if !ok {
			return errors.New("the virtual monitor is not up")
		}
		if len(st.Layout) == 0 {
			for _, d := range onlineDisplays() {
				if d.main && !d.virtual {
					st.Layout = []Placed{{Name: strconv.FormatUint(uint64(d.id), 10)}}
				}
			}
		}
		return makePrimaryLocked(vid)
	}
	err := restorePrimaryLocked(st.Layout)
	st.Layout = nil
	return err
}

// restorePrimaryLocked makes the monitor recorded in layout the primary again,
// or, when it is gone, the first real monitor -- never the virtual one.
func restorePrimaryLocked(layout []Placed) error {
	online := onlineDisplays()
	if len(layout) > 0 {
		if id, err := displayID(layout[0].Name); err == nil {
			if _, ok := findDisplay(online, id); ok {
				return makePrimaryLocked(id)
			}
		}
	}
	real := []cgDisplay{}
	for _, d := range online {
		if d.virtual || d.mirrors != 0 {
			continue
		}
		if d.main {
			return nil
		}
		real = append(real, d)
	}
	if len(real) == 0 {
		return nil
	}
	// The built-in display first, as macOS itself picks.
	sort.SliceStable(real, func(i, j int) bool { return real[i].builtin && !real[j].builtin })
	return makePrimaryLocked(real[0].id)
}

// OthersOff switches every monitor but the virtual one off; their IDs are kept
// in Topology. A no-op until the virtual monitor is up.
func OthersOff(st *VirtualState) (*VirtualState, error) {
	mu.Lock()
	defer mu.Unlock()
	return othersOffLocked(st)
}

func othersOffLocked(st *VirtualState) (*VirtualState, error) {
	if _, ok := virtualOnline(); !ok {
		return st, nil
	}
	if st == nil {
		st = &VirtualState{}
	}
	off := splitNames(st.Topology)
	var ids []C.CGDirectDisplayID
	for _, d := range onlineDisplays() {
		if d.virtual {
			continue
		}
		ids = append(ids, d.id)
		if id := strconv.FormatUint(uint64(d.id), 10); !contains(off, id) {
			off = append(off, id)
		}
	}
	st.Topology = strings.Join(off, ",")
	return st, setEnabledLocked(ids, false)
}

// OthersOn switches the monitors OthersOff switched off back on.
func OthersOn(st *VirtualState) error {
	mu.Lock()
	defer mu.Unlock()
	return othersOnLocked(st)
}

func othersOnLocked(st *VirtualState) error {
	if st == nil || st.Topology == "" {
		return nil
	}
	online := onlineDisplays()
	var ids []C.CGDirectDisplayID
	for _, name := range splitNames(st.Topology) {
		if id, err := displayID(name); err == nil {
			if _, ok := findDisplay(online, id); !ok {
				ids = append(ids, id)
			}
		}
	}
	return setEnabledLocked(ids, true)
}

// RestoreVirtual undoes AttachVirtual: the others back on, the primary role
// back on a real monitor, the virtual monitor gone. With no state it only
// takes the monitor away.
func RestoreVirtual(st *VirtualState) error {
	mu.Lock()
	defer mu.Unlock()
	var errs []error
	if st != nil {
		if err := othersOnLocked(st); err != nil {
			errs = append(errs, err)
		}
	}
	var layout []Placed
	if st != nil {
		layout = st.Layout
	}
	if err := restorePrimaryLocked(layout); err != nil {
		errs = append(errs, err)
	}
	if vid := C.usbVirtualID(); vid != 0 {
		C.usbDestroyVirtual()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if _, ok := findDisplay(onlineDisplays(), vid); !ok {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	return errors.Join(errs...)
}

// ParkVirtual: no driver install attaches a monitor on macOS.
func ParkVirtual(time.Duration) (bool, error) { return false, nil }

// PlannedVirtual names the virtual monitor before it is up: its EDID identity
// (VirtualDeviceName), which the stream host captures it by, and its name.
func PlannedVirtual() (gdi, friendly string, err error) {
	return VirtualDeviceName, virtualName, nil
}

func prepareHelperThread() {}

// RunInProcess runs one helper command (see RunHelper) in this process: the
// virtual monitor it brings up lives as long as the process does.
func RunInProcess(args []string) HelperResult { return runHelper(args) }

// WatchAgent waits for the agent (pid) to exit and puts the displays back as
// the state file records: the virtual monitor went with the agent, so the
// monitors a session switched off stay dark, and the primary role wherever
// macOS put it, until something switches them back -- nothing waits for a new
// agent (unlike Linux), there is no monitor left for it to take over.
func WatchAgent(state string, pid int, _ string) error {
	for processAlive(pid) {
		time.Sleep(500 * time.Millisecond)
	}
	st := LoadState(state)
	if st == nil {
		return nil
	}
	log.Printf("[vdisplay] the agent is gone -- putting the displays back")
	err := RestoreVirtual(st)
	if serr := saveState(state, nil); serr != nil && err == nil {
		err = serr
	}
	return err
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func splitNames(s string) []string {
	var out []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
