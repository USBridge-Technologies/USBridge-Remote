//go:build windows

package displaypower

import (
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The MttVDD virtual monitor for a stream host that has no virtual display of its own
// (Sunshine, Vibepollo): a Go port of rust-shine's virtual-display crate (mttvdd.rs,
// primary.rs, exclusive.rs, display_config.rs), so every streamer gets the same monitor
// with the same behavior. RustShine keeps its own copy in-process; here the state lives in
// VirtualState, so attaching and restoring can run in separate helper processes (see
// RunHelper).

var (
	procEnumDisplaySettingsW     = user32.NewProc("EnumDisplaySettingsW")
	procChangeDisplaySettingsExW = user32.NewProc("ChangeDisplaySettingsExW")
)

const (
	sdcTopologyExtend = 0x4

	dmPelsWidth        = 0x80000
	dmPelsHeight       = 0x100000
	dmDisplayFrequency = 0x400000
)

// DEVMODEW, display variant, 220 bytes.
type devMode struct {
	DeviceName       [32]uint16
	SpecVersion      uint16
	DriverVersion    uint16
	Size             uint16
	DriverExtra      uint16
	Fields           uint32
	Position         [16]byte
	Color            int16
	Duplex           int16
	YResolution      int16
	TTOption         int16
	Collate          int16
	FormName         [32]uint16
	LogPixels        uint16
	BitsPerPel       uint32
	PelsWidth        uint32
	PelsHeight       uint32
	DisplayFlags     uint32
	DisplayFrequency uint32
	ICM              [8]uint32
}

func isVirtual(p *pathInfo) bool {
	_, path, ok := targetName(p)
	return ok && strings.Contains(path, mttvddTarget)
}

func sourcePosition(p *pathInfo, modes []modeInfo) (x, y int32, ok bool) {
	idx := p.Source.ModeInfoIdx
	if int(idx) >= len(modes) || modes[idx].InfoType != modeTypeSource {
		return 0, 0, false
	}
	x, y = modes[idx].position()
	return x, y, true
}

// layout reports every active source's GDI name and position.
func layout() ([]Placed, error) {
	paths, modes, err := query(qdcOnlyActivePaths)
	if err != nil {
		return nil, err
	}
	var out []Placed
	for i := range paths {
		x, y, ok := sourcePosition(&paths[i], modes)
		name := sourceGDIName(&paths[i])
		if !ok || name == "" {
			continue
		}
		dup := false
		for _, p := range out {
			if strings.EqualFold(p.Name, name) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, Placed{Name: name, X: x, Y: y})
		}
	}
	return out, nil
}

// applyLayout moves active sources to the given positions (by GDI name; others keep
// theirs) in one SetDisplayConfig. The source at (0,0) is the primary display.
func applyLayout(l []Placed) error {
	paths, modes, err := query(qdcOnlyActivePaths)
	if err != nil {
		return err
	}
	for i := range paths {
		name := sourceGDIName(&paths[i])
		idx := paths[i].Source.ModeInfoIdx
		if name == "" || int(idx) >= len(modes) || modes[idx].InfoType != modeTypeSource {
			continue
		}
		for _, p := range l {
			if strings.EqualFold(p.Name, name) {
				modes[idx].setPosition(p.X, p.Y)
				break
			}
		}
	}
	// Not saved to Windows' display database, like the attach and the mode: a
	// session that never got to put them back (connection lost, agent killed,
	// power cut) left the virtual monitor primary through a reboot too, the
	// login screen on a monitor nobody can see (issue #23). Only the detach,
	// the end state, is saved.
	return set("positions", paths, modes, sdcApply|sdcUseSuppliedDisplayConfig|sdcAllowChanges)
}

// shiftedTo is l with target moved to the origin and everything else by the same
// offset, so the arrangement keeps its shape.
func shiftedTo(l []Placed, target string) ([]Placed, bool) {
	for _, t := range l {
		if !strings.EqualFold(t.Name, target) {
			continue
		}
		out := make([]Placed, len(l))
		for i, p := range l {
			out[i] = Placed{Name: p.Name, X: p.X - t.X, Y: p.Y - t.Y}
		}
		return out, true
	}
	return nil, false
}

func samePlaces(a, b []Placed) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// primaryDevicePath is the monitor device path of the primary display, "" if none.
func primaryDevicePath() string {
	ms, err := List()
	if err != nil {
		return ""
	}
	for _, m := range ms {
		if m.Primary {
			return m.ID
		}
	}
	return ""
}

// activeGDIName is the GDI name of the active monitor with this device path, "" if none.
func activeGDIName(devicePath string) string {
	if devicePath == "" {
		return ""
	}
	ms, err := List()
	if err != nil {
		return ""
	}
	for _, m := range ms {
		if m.Active && strings.EqualFold(m.ID, devicePath) {
			return m.GDIName
		}
	}
	return ""
}

func moveOriginTo(name string) error {
	l, err := layout()
	if err != nil {
		return err
	}
	next, ok := shiftedTo(l, name)
	if !ok {
		return fmt.Errorf("%s is not an active display", name)
	}
	if samePlaces(next, l) {
		return nil
	}
	return applyLayout(next)
}

// pickMode is the exact mode if offered, else the closest size, then refresh rate.
func pickMode(available []Mode, want Mode) (Mode, bool) {
	best, found := Mode{}, false
	var bestSize, bestHz int
	for _, m := range available {
		size := absInt(m.Width-want.Width) + absInt(m.Height-want.Height)
		hz := absInt(m.Hz - want.Hz)
		if !found || size < bestSize || (size == bestSize && hz < bestHz) {
			best, bestSize, bestHz, found = m, size, hz, true
		}
	}
	return best, found
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func newDevMode() devMode {
	var dm devMode
	dm.Size = uint16(unsafe.Sizeof(dm))
	return dm
}

func displayModes(gdi string) []Mode {
	name, err := windows.UTF16PtrFromString(gdi)
	if err != nil {
		return nil
	}
	var out []Mode
	for i := uint32(0); ; i++ {
		dm := newDevMode()
		if r, _, _ := procEnumDisplaySettingsW.Call(uintptr(unsafe.Pointer(name)), uintptr(i), uintptr(unsafe.Pointer(&dm))); r == 0 {
			break
		}
		m := Mode{Width: int(dm.PelsWidth), Height: int(dm.PelsHeight), Hz: int(dm.DisplayFrequency)}
		dup := false
		for _, o := range out {
			if o == m {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, m)
		}
	}
	return out
}

// setMode changes an attached source's size and refresh rate, leaving its position.
// Dynamically, not in the registry (see the positions above).
func setMode(gdi string, m Mode) error {
	name, err := windows.UTF16PtrFromString(gdi)
	if err != nil {
		return err
	}
	dm := newDevMode()
	dm.Fields = dmPelsWidth | dmPelsHeight | dmDisplayFrequency
	dm.PelsWidth, dm.PelsHeight, dm.DisplayFrequency = uint32(m.Width), uint32(m.Height), uint32(m.Hz)
	r, _, _ := procChangeDisplaySettingsExW.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&dm)), 0, 0, 0)
	if int32(r) != 0 {
		return fmt.Errorf("ChangeDisplaySettingsExW(%s, %s): DISP_CHANGE %d", gdi, m, int32(r))
	}
	return nil
}

// activeVirtual is the active path to the virtual monitor, if any.
func activeVirtual() (gdi, friendly, devicePath string, ok bool) {
	paths, _, err := query(qdcOnlyActivePaths)
	if err != nil {
		return "", "", "", false
	}
	for i := range paths {
		f, path, tok := targetName(&paths[i])
		if tok && strings.Contains(path, mttvddTarget) {
			return sourceGDIName(&paths[i]), f, path, true
		}
	}
	return "", "", "", false
}

// freeVirtualPath splits all paths into the active non-virtual ones and the path the
// virtual monitor gets attached on: its first one whose source no active display uses.
func freeVirtualPath(all []pathInfo) (sel []pathInfo, free *pathInfo) {
	for i := range all {
		if all[i].Flags&pathActive != 0 && !isVirtual(&all[i]) {
			sel = append(sel, all[i])
		}
	}
	for i := range all {
		p := &all[i]
		if !isVirtual(p) || p.Target.TargetAvailable == 0 {
			continue
		}
		used := false
		for j := range sel {
			if sameSource(&sel[j], p) {
				used = true
				break
			}
		}
		if !used {
			return sel, p
		}
	}
	return sel, nil
}

// PlannedVirtual is the GDI and friendly name the virtual monitor has, or gets once
// attached (attachVirtualTarget's choice of path), without attaching it: what a
// stream host is pointed at before a session brings the monitor up.
func PlannedVirtual() (gdi, friendly string, err error) {
	if g, f, _, ok := activeVirtual(); ok {
		return g, f, nil
	}
	all, _, err := query(qdcAllPaths)
	if err != nil {
		return "", "", err
	}
	_, free := freeVirtualPath(all)
	if free == nil {
		return "", "", ErrNoVirtualDriver
	}
	f, _, _ := targetName(free)
	return sourceGDIName(free), f, nil
}

// attachVirtualTarget attaches the virtual monitor as a display of its own, extending
// the desktop. A path mirroring it onto another source (Windows puts a newly seen
// virtual monitor into "duplicate" with the main screen) is dropped. Windows picks the
// mode and position; the caller sets both afterwards.
func attachVirtualTarget() error {
	all, _, err := query(qdcAllPaths)
	if err != nil {
		return err
	}
	sel, free := freeVirtualPath(all)
	if free == nil {
		return ErrNoVirtualDriver
	}
	v := *free
	v.Flags |= pathActive
	v.Source.ModeInfoIdx = modeIdxInvalid
	v.Target.ModeInfoIdx = modeIdxInvalid
	topo := append(append([]pathInfo(nil), sel...), v)
	for i := range topo {
		topo[i].Source.ModeInfoIdx = modeIdxInvalid
		topo[i].Target.ModeInfoIdx = modeIdxInvalid
	}
	err = set("attach", topo, nil, sdcApply|sdcTopologySupplied|sdcAllowPathOrderChanges)
	if err == nil {
		return nil
	}
	// Seen live with an HDMI capture card as a second physical monitor on an Intel
	// iGPU: the topology-only attach fails with ERROR_GEN_FAILURE (31). The active
	// paths with their own modes plus the new path without one goes through.
	act, modes, qerr := query(qdcOnlyActivePaths)
	if qerr != nil {
		return err
	}
	var keep []pathInfo
	for i := range act {
		if !isVirtual(&act[i]) {
			keep = append(keep, act[i])
		}
	}
	if serr := set("attach (supplied)", append(keep, v), modes, sdcApply|sdcUseSuppliedDisplayConfig|sdcAllowChanges); serr != nil {
		return fmt.Errorf("%v; %v", err, serr)
	}
	log.Printf("[vdisplay] attached with the supplied display config (the topology-only attach failed: %v)", err)
	return nil
}

func extendAll() error {
	r, _, _ := procSetDisplayConfig.Call(0, 0, 0, 0, sdcApply|sdcTopologyExtend)
	if r != 0 {
		return fmt.Errorf("SetDisplayConfig(extend): %d", r)
	}
	return nil
}

// detachVirtualTarget drops every active path to the virtual monitor, keeping the rest
// of the desktop as it is. A no-op when it is not attached.
func detachVirtualTarget() error {
	paths, modes, err := query(qdcOnlyActivePaths)
	if err != nil {
		return err
	}
	allVirtual := len(paths) > 0
	for i := range paths {
		if !isVirtual(&paths[i]) {
			allVirtual = false
			break
		}
	}
	if allVirtual {
		// The other screens were switched off by a run that never put them back: bring
		// everything connected back first, so detaching leaves a desktop.
		log.Printf("[vdisplay] the virtual display is the only active one -- switching the other displays back on first")
		if err := extendAll(); err != nil {
			return err
		}
		if paths, modes, err = query(qdcOnlyActivePaths); err != nil {
			return err
		}
	}
	var keep []pathInfo
	for i := range paths {
		if !isVirtual(&paths[i]) {
			keep = append(keep, paths[i])
		}
	}
	if len(keep) == len(paths) || len(keep) == 0 {
		return nil
	}
	// Windows refuses a desktop with nothing at the origin: shift what stays so its
	// first display sits there.
	atOrigin := false
	for i := range keep {
		if x, y, ok := sourcePosition(&keep[i], modes); ok && x == 0 && y == 0 {
			atOrigin = true
			break
		}
	}
	if !atOrigin {
		if dx, dy, ok := sourcePosition(&keep[0], modes); ok {
			done := map[uint32]bool{}
			for i := range keep {
				idx := keep[i].Source.ModeInfoIdx
				if done[idx] {
					continue
				}
				done[idx] = true
				if x, y, ok := sourcePosition(&keep[i], modes); ok {
					modes[idx].setPosition(x-dx, y-dy)
				}
			}
		}
	}
	return set("detach", keep, modes, sdcApply|sdcUseSuppliedDisplayConfig|sdcAllowChanges|sdcSaveToDatabase)
}

func encodeTopology(paths []pathInfo, modes []modeInfo) string {
	pb := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(paths))), len(paths)*int(unsafe.Sizeof(pathInfo{})))
	mb := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(modes))), len(modes)*int(unsafe.Sizeof(modeInfo{})))
	return fmt.Sprintf("%d:%s:%s", len(paths), base64.StdEncoding.EncodeToString(pb), base64.StdEncoding.EncodeToString(mb))
}

func decodeTopology(s string) ([]pathInfo, []modeInfo, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return nil, nil, fmt.Errorf("bad saved topology")
	}
	pb, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, nil, err
	}
	mb, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, nil, err
	}
	ps, ms := int(unsafe.Sizeof(pathInfo{})), int(unsafe.Sizeof(modeInfo{}))
	if len(pb)%ps != 0 || len(mb)%ms != 0 {
		return nil, nil, fmt.Errorf("bad saved topology size")
	}
	paths := make([]pathInfo, len(pb)/ps)
	modes := make([]modeInfo, len(mb)/ms)
	if len(paths) > 0 {
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&paths[0])), len(pb)), pb)
	}
	if len(modes) > 0 {
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&modes[0])), len(mb)), mb)
	}
	return paths, modes, nil
}

// AttachVirtual attaches the MttVDD monitor in want (or the driver's closest mode),
// optionally makes it the primary display and optionally switches every other monitor
// off. The returned state undoes it (RestoreVirtual). Being made primary or exclusive
// is best-effort: a failure there is logged and the monitor stays attached.
func AttachVirtual(want Mode, primary, exclusive bool) (*VirtualState, error) {
	st := &VirtualState{}
	if _, _, _, ok := activeVirtual(); !ok {
		// By device path: rebuilding the topology can renumber the GDI names (seen live:
		// the physical primary went from DISPLAY2 to DISPLAY1).
		before := primaryDevicePath()
		if err := attachVirtualTarget(); err != nil {
			return nil, err
		}
		if gdi := activeGDIName(before); gdi != "" {
			if err := moveOriginTo(gdi); err != nil {
				log.Printf("[vdisplay] couldn't keep %s primary after attaching the virtual display: %v", gdi, err)
			}
		}
	}
	gdi, friendly, path, ok := activeVirtual()
	if !ok {
		return nil, fmt.Errorf("the virtual display did not attach")
	}
	st.GDIName, st.FriendlyName, st.DevicePath = gdi, friendly, path
	m, ok := pickMode(displayModes(gdi), want)
	if !ok {
		return st, fmt.Errorf("MttVDD offers no display modes (more than ~100 in vdd_settings.xml?)")
	}
	if m != want {
		log.Printf("[vdisplay] MttVDD has no %s -- using its closest mode %s", want, m)
	}
	if err := setMode(gdi, m); err != nil {
		return st, err
	}
	st.Mode = m
	if primary {
		if err := st.SetPrimary(true); err != nil {
			log.Printf("[vdisplay] couldn't make %s the primary display: %v", gdi, err)
		}
	}
	if exclusive {
		if err := st.makeExclusive(); err != nil {
			log.Printf("[vdisplay] couldn't switch the other displays off: %v", err)
		}
	}
	// SetDisplayConfig returns before DXGI's outputs catch up; a capture opened right
	// away could miss the monitor.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if g, _, _, ok := activeVirtual(); ok && strings.EqualFold(g, gdi) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return st, nil
}

// SetPrimary makes the attached virtual monitor the primary display (remembering the
// desktop from before) or puts that desktop back.
func (st *VirtualState) SetPrimary(on bool) error {
	if !on {
		return st.restorePrimary()
	}
	l, err := layout()
	if err != nil {
		return err
	}
	next, ok := shiftedTo(l, st.GDIName)
	if !ok {
		return fmt.Errorf("%s is not an active display", st.GDIName)
	}
	if samePlaces(next, l) {
		return nil
	}
	if st.Layout == nil {
		st.Layout = l
	}
	if err := applyLayout(next); err != nil {
		return err
	}
	log.Printf("[vdisplay] %s is now the primary display", st.GDIName)
	return nil
}

func (st *VirtualState) restorePrimary() error {
	saved := st.Layout
	st.Layout = nil
	if saved == nil {
		return nil
	}
	now, err := layout()
	if err != nil {
		return err
	}
	var back []Placed
	oldPrimary := ""
	for _, p := range saved {
		if p.X == 0 && p.Y == 0 {
			oldPrimary = p.Name
		}
		for _, n := range now {
			if strings.EqualFold(n.Name, p.Name) {
				back = append(back, p)
				break
			}
		}
	}
	present := false
	for _, p := range back {
		if strings.EqualFold(p.Name, oldPrimary) {
			present = true
		}
	}
	if !present {
		log.Printf("[vdisplay] the previous primary display %q is gone -- leaving the primary to Windows", oldPrimary)
		return nil
	}
	if err := applyLayout(back); err != nil {
		return err
	}
	log.Printf("[vdisplay] primary display %s restored", oldPrimary)
	return nil
}

// makeExclusive drops every display path but the virtual monitor's, which goes to the
// origin. Not saved to Windows' display database, so a reboot never leaves the machine
// with its screens off.
func (st *VirtualState) makeExclusive() error {
	paths, modes, err := query(qdcOnlyActivePaths)
	if err != nil {
		return err
	}
	var keep []pathInfo
	for i := range paths {
		if strings.EqualFold(sourceGDIName(&paths[i]), st.GDIName) {
			keep = append(keep, paths[i])
		}
	}
	if len(keep) == 0 {
		return fmt.Errorf("%s is not an active display", st.GDIName)
	}
	if len(keep) == len(paths) {
		return nil
	}
	next := append([]modeInfo(nil), modes...)
	for i := range keep {
		if idx := keep[i].Source.ModeInfoIdx; int(idx) < len(next) && next[idx].InfoType == modeTypeSource {
			next[idx].setPosition(0, 0)
		}
	}
	if err := set("exclusive", keep, next, sdcApply|sdcUseSuppliedDisplayConfig|sdcAllowChanges); err != nil {
		return err
	}
	if st.Topology == "" {
		st.Topology = encodeTopology(paths, modes)
	}
	log.Printf("[vdisplay] %d other display(s) switched off -- %s is the only one", len(paths)-len(keep), st.GDIName)
	return nil
}

func (st *VirtualState) restoreExclusive() error {
	saved := st.Topology
	st.Topology = ""
	if saved == "" {
		return nil
	}
	paths, modes, err := decodeTopology(saved)
	if err == nil {
		err = set("restore", paths, modes, sdcApply|sdcUseSuppliedDisplayConfig|sdcAllowChanges)
	}
	if err != nil {
		log.Printf("[vdisplay] couldn't put the previous display topology back (%v) -- extending over every connected display", err)
		return extendAll()
	}
	log.Printf("[vdisplay] other displays switched back on")
	return nil
}

// RestoreVirtual switches the other monitors back on, hands the primary role back and
// detaches the virtual monitor. st may be nil (nothing remembered, e.g. after a crash):
// then it only detaches it. A no-op when it is not attached.
// watchAgentGrace is how long WatchAgent leaves the agent to come back (the
// service's recovery restarts it after 5 s) and undo the session itself.
const watchAgentGrace = 20 * time.Second

// WatchAgent is the agent's stand-in while a session has the virtual monitor up: it
// waits for the agent process pid to exit and, when the session's state file is still
// there watchAgentGrace later and the agent's service isn't running again, puts the
// displays back itself (RestoreVirtual) and removes the file. Killed or crashed
// mid-session, with nothing restarting it, the agent left the virtual monitor the
// only, primary screen and the real ones dark until a reboot. A clean session end
// removes the state file, so the watcher then has nothing to do.
func WatchAgent(path string, pid int, service string) error {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("watch agent %d: %w", pid, err)
	}
	_, err = windows.WaitForSingleObject(h, windows.INFINITE)
	windows.CloseHandle(h)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(watchAgentGrace)
	for time.Now().Before(deadline) {
		if LoadState(path) == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	if LoadState(path) == nil || serviceRunning(service) {
		// Back up: the agent undoes or keeps the session itself.
		return nil
	}
	log.Printf("[vdisplay] the agent (pid %d) exited mid-session and didn't come back -- putting the displays back", pid)
	if err := RestoreVirtual(LoadState(path)); err != nil {
		return err
	}
	return saveState(path, nil)
}

func serviceRunning(name string) bool {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(scm)
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	svc, err := windows.OpenService(scm, n, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(svc)
	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(svc, &st); err != nil {
		return false
	}
	return st.CurrentState == windows.SERVICE_RUNNING || st.CurrentState == windows.SERVICE_START_PENDING
}

// ParkVirtual detaches the virtual monitor a fresh driver install brings up: Windows
// attaches a newly arrived monitor extended, though nothing streams it. Waits up to
// timeout for it to show up (the driver starts it a moment after its install) and
// reports whether it detached one; a virtual monitor alone is left be (StrayVirtual).
// Detached, Windows' display database keeps it off until a session attaches it.
func ParkVirtual(timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		monitors, err := List()
		if err == nil && StrayVirtual(monitors) {
			return true, detachVirtualTarget()
		}
		if time.Now().After(deadline) {
			return false, err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func RestoreVirtual(st *VirtualState) error {
	if st != nil {
		// While the virtual monitor is still there to move away from; Windows would
		// otherwise pick a new primary on its own when it disappears.
		if err := st.restoreExclusive(); err != nil {
			log.Printf("[vdisplay] restore other displays: %v", err)
		}
		if err := st.restorePrimary(); err != nil {
			log.Printf("[vdisplay] restore primary: %v", err)
		}
	}
	return detachVirtualTarget()
}

// OthersOff switches every monitor but the attached virtual one off, for a virtual
// monitor someone else attached (RustShine attaches its own), remembering in st the
// topology from before so OthersOn can put it back. st may be nil (nothing switched
// off yet). A no-op returning st when the virtual monitor already is the only one, or
// is not attached (nothing to keep on).
func OthersOff(st *VirtualState) (*VirtualState, error) {
	gdi, friendly, path, ok := activeVirtual()
	if !ok {
		return st, nil
	}
	if st == nil {
		st = &VirtualState{}
	}
	st.GDIName, st.FriendlyName, st.DevicePath = gdi, friendly, path
	return st, st.makeExclusive()
}

// OthersOn puts back the topology OthersOff switched off. A no-op without one.
func OthersOn(st *VirtualState) error {
	if st == nil {
		return nil
	}
	return st.restoreExclusive()
}
