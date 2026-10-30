//go:build windows

package displaypower

import (
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The same SetDisplayConfig moves rust-shine's virtual-display crate uses for MttVDD
// (display_config.rs attach_target / detach_target): the legacy ChangeDisplaySettingsExW
// attach fails on a hybrid-GPU laptop, SetDisplayConfig does not.

var (
	user32                          = windows.NewLazySystemDLL("user32.dll")
	procGetDisplayConfigBufferSizes = user32.NewProc("GetDisplayConfigBufferSizes")
	procQueryDisplayConfig          = user32.NewProc("QueryDisplayConfig")
	procSetDisplayConfig            = user32.NewProc("SetDisplayConfig")
	procDisplayConfigGetDeviceInfo  = user32.NewProc("DisplayConfigGetDeviceInfo")
)

const (
	qdcAllPaths        = 0x1
	qdcOnlyActivePaths = 0x2

	sdcTopologySupplied         = 0x10
	sdcUseSuppliedDisplayConfig = 0x20
	sdcApply                    = 0x80
	sdcSaveToDatabase           = 0x200
	sdcAllowChanges             = 0x400
	sdcAllowPathOrderChanges    = 0x2000

	pathActive            = 0x1
	modeIdxInvalid        = 0xffffffff
	modeTypeSource        = 1
	getSourceName         = 1
	getTargetName         = 2
	errInsufficientBuffer = 122

	// MttVDD's monitor in its device path (EDID manufacturer MTT, product 1337).
	mttvddTarget = "MTT1337"
)

type luid struct {
	Low  uint32
	High int32
}

type pathSourceInfo struct {
	AdapterID   luid
	ID          uint32
	ModeInfoIdx uint32
	StatusFlags uint32
}

type pathTargetInfo struct {
	AdapterID        luid
	ID               uint32
	ModeInfoIdx      uint32
	OutputTechnology uint32
	Rotation         uint32
	Scaling          uint32
	RefreshNum       uint32
	RefreshDen       uint32
	ScanLineOrdering uint32
	TargetAvailable  int32
	StatusFlags      uint32
}

// DISPLAYCONFIG_PATH_INFO, 72 bytes.
type pathInfo struct {
	Source pathSourceInfo
	Target pathTargetInfo
	Flags  uint32
}

// DISPLAYCONFIG_MODE_INFO, 64 bytes. For a source mode the union starts with
// width, height, pixelFormat, then the position (x, y).
type modeInfo struct {
	InfoType  uint32
	ID        uint32
	AdapterID luid
	Union     [48]byte
}

func (m *modeInfo) position() (int32, int32) {
	return *(*int32)(unsafe.Pointer(&m.Union[12])), *(*int32)(unsafe.Pointer(&m.Union[16]))
}

// size is a source mode's width and height (DISPLAYCONFIG_SOURCE_MODE).
func (m *modeInfo) size() (int, int) {
	return int(*(*uint32)(unsafe.Pointer(&m.Union[0]))), int(*(*uint32)(unsafe.Pointer(&m.Union[4])))
}

func (m *modeInfo) setPosition(x, y int32) {
	*(*int32)(unsafe.Pointer(&m.Union[12])) = x
	*(*int32)(unsafe.Pointer(&m.Union[16])) = y
}

type deviceInfoHeader struct {
	Type      uint32
	Size      uint32
	AdapterID luid
	ID        uint32
}

type targetDeviceName struct {
	Header            deviceInfoHeader
	Flags             uint32
	OutputTechnology  uint32
	EdidManufactureID uint16
	EdidProductCodeID uint16
	ConnectorInstance uint32
	FriendlyName      [64]uint16
	DevicePath        [128]uint16
}

type sourceDeviceName struct {
	Header  deviceInfoHeader
	GDIName [32]uint16
}

func query(flags uint32) ([]pathInfo, []modeInfo, error) {
	for i := 0; i < 3; i++ {
		var pc, mc uint32
		if r, _, _ := procGetDisplayConfigBufferSizes.Call(uintptr(flags), uintptr(unsafe.Pointer(&pc)), uintptr(unsafe.Pointer(&mc))); r != 0 {
			return nil, nil, fmt.Errorf("GetDisplayConfigBufferSizes: %d", r)
		}
		paths := make([]pathInfo, pc)
		modes := make([]modeInfo, mc)
		var pp, mp uintptr
		if pc > 0 {
			pp = uintptr(unsafe.Pointer(&paths[0]))
		}
		if mc > 0 {
			mp = uintptr(unsafe.Pointer(&modes[0]))
		}
		r, _, _ := procQueryDisplayConfig.Call(uintptr(flags), uintptr(unsafe.Pointer(&pc)), pp, uintptr(unsafe.Pointer(&mc)), mp, 0)
		if r == 0 {
			return paths[:pc], modes[:mc], nil
		}
		if r != errInsufficientBuffer {
			return nil, nil, fmt.Errorf("QueryDisplayConfig: %d", r)
		}
	}
	return nil, nil, fmt.Errorf("QueryDisplayConfig: the path list kept growing")
}

func set(what string, paths []pathInfo, modes []modeInfo, flags uint32) error {
	var pp, mp uintptr
	if len(paths) > 0 {
		pp = uintptr(unsafe.Pointer(&paths[0]))
	}
	if len(modes) > 0 {
		mp = uintptr(unsafe.Pointer(&modes[0]))
	}
	r, _, _ := procSetDisplayConfig.Call(uintptr(len(paths)), pp, uintptr(len(modes)), mp, uintptr(flags))
	if r != 0 {
		return fmt.Errorf("SetDisplayConfig(%s): %d", what, r)
	}
	return nil
}

func targetName(p *pathInfo) (friendly, devicePath string, ok bool) {
	var n targetDeviceName
	n.Header = deviceInfoHeader{Type: getTargetName, Size: uint32(unsafe.Sizeof(n)), AdapterID: p.Target.AdapterID, ID: p.Target.ID}
	if r, _, _ := procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&n))); r != 0 {
		return "", "", false
	}
	return windows.UTF16ToString(n.FriendlyName[:]), windows.UTF16ToString(n.DevicePath[:]), true
}

func sourceGDIName(p *pathInfo) string {
	var n sourceDeviceName
	n.Header = deviceInfoHeader{Type: getSourceName, Size: uint32(unsafe.Sizeof(n)), AdapterID: p.Source.AdapterID, ID: p.Source.ID}
	if r, _, _ := procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&n))); r != 0 {
		return ""
	}
	return windows.UTF16ToString(n.GDIName[:])
}

func sameSource(a, b *pathInfo) bool {
	return a.Source.ID == b.Source.ID && a.Source.AdapterID == b.Source.AdapterID
}

func sameTarget(a, b *pathInfo) bool {
	return a.Target.ID == b.Target.ID && a.Target.AdapterID == b.Target.AdapterID
}

// List reports every monitor Windows has a display path for, active or not.
func List() ([]Monitor, error) {
	all, _, err := query(qdcAllPaths)
	if err != nil {
		return nil, err
	}
	active, modes, err := query(qdcOnlyActivePaths)
	if err != nil {
		return nil, err
	}
	var out []Monitor
	seen := map[string]bool{}
	for i := range all {
		p := &all[i]
		if p.Target.TargetAvailable == 0 {
			continue
		}
		friendly, path, ok := targetName(p)
		if !ok || path == "" || seen[strings.ToLower(path)] {
			continue
		}
		seen[strings.ToLower(path)] = true
		m := Monitor{ID: path, Name: friendly, Virtual: strings.Contains(path, mttvddTarget)}
		for j := range active {
			a := &active[j]
			if !sameTarget(a, p) {
				continue
			}
			m.Active = true
			m.GDIName = sourceGDIName(a)
			if idx := a.Source.ModeInfoIdx; int(idx) < len(modes) && modes[idx].InfoType == modeTypeSource {
				x, y := modes[idx].position()
				m.Primary = x == 0 && y == 0
				m.Width, m.Height = modes[idx].size()
			}
			break
		}
		if m.Name == "" {
			m.Name = m.GDIName
		}
		out = append(out, m)
	}
	return out, nil
}

func isTarget(p *pathInfo, id string) bool {
	_, path, ok := targetName(p)
	return ok && strings.EqualFold(path, id)
}

// SetEnabled switches the monitor with this ID on (attached to the desktop, extending
// it) or off (detached). Switching off the only active monitor is refused.
func SetEnabled(id string, on bool) error {
	if on {
		return attach(id)
	}
	return detach(id)
}

func attach(id string) error {
	all, _, err := query(qdcAllPaths)
	if err != nil {
		return err
	}
	var sel []pathInfo
	for i := range all {
		if all[i].Flags&pathActive != 0 && !isTarget(&all[i], id) {
			sel = append(sel, all[i])
		}
	}
	for i := range all {
		if all[i].Flags&pathActive != 0 && isTarget(&all[i], id) {
			return nil // already on
		}
	}
	var free *pathInfo
	for i := range all {
		p := &all[i]
		if !isTarget(p, id) || p.Target.TargetAvailable == 0 {
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
			free = p
			break
		}
	}
	if free == nil {
		if !anyTarget(all, id) {
			return ErrNotFound
		}
		return fmt.Errorf("no free display output for this monitor")
	}
	v := *free
	v.Flags |= pathActive
	sel = append(sel, v)
	for i := range sel {
		sel[i].Source.ModeInfoIdx = modeIdxInvalid
		sel[i].Target.ModeInfoIdx = modeIdxInvalid
	}
	return set("attach", sel, nil, sdcApply|sdcTopologySupplied|sdcAllowPathOrderChanges)
}

func countActive(all []pathInfo) int {
	n := 0
	for i := range all {
		if all[i].Flags&pathActive != 0 {
			n++
		}
	}
	return n
}

func anyTarget(all []pathInfo, id string) bool {
	for i := range all {
		if isTarget(&all[i], id) {
			return true
		}
	}
	return false
}

func detach(id string) error {
	paths, modes, err := query(qdcOnlyActivePaths)
	if err != nil {
		return err
	}
	var keep []pathInfo
	wasPrimary := false
	for i := range paths {
		p := &paths[i]
		if isTarget(p, id) {
			if idx := p.Source.ModeInfoIdx; int(idx) < len(modes) && modes[idx].InfoType == modeTypeSource {
				x, y := modes[idx].position()
				wasPrimary = wasPrimary || (x == 0 && y == 0)
			}
			continue
		}
		keep = append(keep, *p)
	}
	if len(keep) == len(paths) {
		all, _, err := query(qdcAllPaths)
		if err == nil && !anyTarget(all, id) {
			return ErrNotFound
		}
		return nil // already off
	}
	if len(keep) == 0 {
		return ErrLastDisplay
	}
	if wasPrimary {
		// Some screen must sit at the origin: shift the rest so the first remaining
		// physical one (else any) becomes primary, keeping the arrangement's shape.
		pick := -1
		for i := range keep {
			_, path, _ := targetName(&keep[i])
			if !strings.Contains(path, mttvddTarget) {
				pick = i
				break
			}
		}
		if pick < 0 {
			pick = 0
		}
		if idx := keep[pick].Source.ModeInfoIdx; int(idx) < len(modes) && modes[idx].InfoType == modeTypeSource {
			dx, dy := modes[idx].position()
			done := map[uint32]bool{}
			for i := range keep {
				si := keep[i].Source.ModeInfoIdx
				if int(si) >= len(modes) || modes[si].InfoType != modeTypeSource || done[si] {
					continue
				}
				done[si] = true
				x, y := modes[si].position()
				modes[si].setPosition(x-dx, y-dy)
			}
		}
	}
	return set("detach", keep, modes, sdcApply|sdcUseSuppliedDisplayConfig|sdcAllowChanges|sdcSaveToDatabase)
}

var (
	procOpenInputDesktop = user32.NewProc("OpenInputDesktop")
	procSetThreadDesktop = user32.NewProc("SetThreadDesktop")
)

// prepareHelperThread puts the helper's thread on the desktop that receives input.
// The service starts the helper as LocalSystem in the user's session, where its
// thread starts on a desktop SetDisplayConfig refuses (ERROR_ACCESS_DENIED, 5) --
// seen live switching the other monitors off. The streamer does the same before its
// own display calls (capture-dxgi attach_input_desktop). Best-effort: a plain user
// process already is on that desktop.
func prepareHelperThread() {
	runtime.LockOSThread()
	const genericAll = 0x10000000
	h, _, _ := procOpenInputDesktop.Call(0, 0, genericAll)
	if h == 0 {
		return
	}
	// Not closed: the thread stays on it until the helper exits.
	procSetThreadDesktop.Call(h)
}

// AgentVirtual: the agent brings the virtual monitor up and down for sessions
// itself (Windows, Linux); elsewhere the stream host does.
const AgentVirtual = true
