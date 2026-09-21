//go:build windows

package usbpass

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// idSets makes winHIDCollection a hidGenPort.
func (c *winHIDCollection) idSets() [ppReportTypes]map[uint8]bool { return c.ids }

// hidBridgeEnabled reports whether the HID bridge may take HID devices. It is
// opt-in: it cannot forward the vendor-specific feature reports some drivers
// send at USB level (Wacom's router does; hid.dll refuses report IDs the
// descriptor does not declare), so by default every device keeps using the
// full-fidelity libusb path. Set USBRIDGE_HID_BRIDGE=1 to try the bridge.
func hidBridgeEnabled() bool { return os.Getenv("USBRIDGE_HID_BRIDGE") == "1" }

// colNumber extracts nn from a "...&COLnn\..." HID instance ID (0 if none) so
// a device's collections are put back in report-descriptor order.
func colNumber(instanceID string) int {
	u := strings.ToUpper(instanceID)
	i := strings.LastIndex(u, "&COL")
	if i < 0 {
		return 0
	}
	rest := u[i+4:]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(rest[:end])
	return n
}

// miNumber extracts the USB interface number from an interface node ID such as
// "USB\VID_046D&PID_C548&MI_01\...", or -1.
func miNumber(ifaceID string) int {
	u := strings.ToUpper(ifaceID)
	i := strings.Index(u, "&MI_")
	if i < 0 || i+6 > len(u) {
		return -1
	}
	n, err := strconv.ParseUint(u[i+4:i+6], 16, 8)
	if err != nil {
		return -1
	}
	return int(n)
}

// liveInput says how a collection's input reports reach the bridge.
type liveInput int

const (
	liveRead liveInput = iota // hid.dll ReadFile on a handle with read access
	liveRaw                   // Raw Input (WM_INPUT): the OS holds the collection exclusively
	liveNone                  // mouse/keyboard held exclusively: Raw Input only gives parsed data
)

func (c *winHIDCollection) liveKind() liveInput {
	if !c.queryOnly {
		return liveRead
	}
	// Raw Input hands mice and keyboards over as parsed RAWMOUSE/RAWKEYBOARD
	// records, not as HID reports, so there is nothing to forward for them.
	if c.pp.UsagePage == 0x01 && (c.pp.Usage == 0x02 || c.pp.Usage == 0x06) {
		return liveNone
	}
	return liveRaw
}

// normalizeHIDID makes HID instance IDs (HID\VID_056A&PID_0374&COL03\7&F7...)
// and Raw Input device paths (\\?\HID#VID_056A&PID_0374&Col03#7&f7...#{guid})
// comparable.
func normalizeHIDID(s string) string {
	s = strings.TrimPrefix(s, `\\?\`)
	if i := strings.Index(s, "#{"); i > 0 {
		s = s[:i]
	}
	return strings.ToUpper(strings.ReplaceAll(s, "#", `\`))
}

// tryClaimHID exports dev through the HID bridge when Windows exposes it as a
// HID device. handled=false with a nil error means "not a HID device (or not
// bridgeable): use the libusb path", handled=true means the outcome (nil or
// error) is final.
func tryClaimHID(dev *ExportedDevice) (handled bool, err error) {
	if !hidBridgeEnabled() || dev.InstanceID == "" || !strings.HasPrefix(strings.ToUpper(dev.InstanceID), "USB\\") {
		return false, nil
	}
	nodes, err := hidNodesOfUSBDevice(dev.InstanceID)
	if err != nil {
		logrus.Debugf("usbpass: hidbridge: %v", err)
		return false, nil
	}
	if len(nodes) == 0 {
		return false, nil
	}

	// Group the collection nodes by USB interface, keeping descriptor order.
	byIface := map[string][]hidNode{}
	var order []string
	for _, n := range nodes {
		if _, seen := byIface[n.Interface]; !seen {
			order = append(order, n.Interface)
		}
		byIface[n.Interface] = append(byIface[n.Interface], n)
	}
	sort.SliceStable(order, func(i, j int) bool { return miNumber(order[i]) < miNumber(order[j]) })

	type ifaceCols struct {
		idx  int // index into ifaces
		cols []*winHIDCollection
	}
	var (
		ifaces  []*hidGenIface
		groups  []ifaceCols
		opened  []*winHIDCollection
		hasAny  *winHIDCollection
		skipped []string
		silent  []string
	)
	closeAll := func() {
		for _, c := range opened {
			c.close()
		}
	}
	for idx, key := range order {
		group := byIface[key]
		sort.SliceStable(group, func(i, j int) bool { return colNumber(group[i].InstanceID) < colNumber(group[j].InstanceID) })

		f := &hidGenIface{Number: uint8(idx)}
		if mi := miNumber(key); mi >= 0 {
			f.Number = uint8(mi)
		}
		var cols []*winHIDCollection
		var problem error
		live := 0
		var quiet []string
		for _, n := range group {
			c, err := openHIDCollection(n.InstanceID)
			if err != nil {
				problem = err
				break
			}
			opened = append(opened, c)
			cols = append(cols, c)
			desc, err := reconstructReportDescriptor(c.pp)
			if err != nil {
				problem = fmt.Errorf("%s: %w", n.InstanceID, err)
				break
			}
			f.ReportDesc = append(f.ReportDesc, desc...)
			for _, m := range c.ids {
				for id := range m {
					if id != 0 {
						f.HasIDs = true
					}
				}
			}
			if c.inLen > f.MaxInput {
				f.MaxInput = c.inLen
			}
			if c.liveKind() == liveNone {
				quiet = append(quiet, n.InstanceID)
			} else {
				live++
			}
		}
		if problem == nil && live == 0 {
			problem = fmt.Errorf("no collection can deliver input reports (mouse/keyboard are held exclusively by Windows)")
		}
		if problem != nil {
			skipped = append(skipped, fmt.Sprintf("interface %q: %v", key, problem))
			continue
		}
		silent = append(silent, quiet...)
		if !f.HasIDs && f.MaxInput > 0 {
			f.MaxInput-- // no report ID byte on the wire
		}
		for _, c := range cols {
			f.Ports = append(f.Ports, c)
		}
		if hasAny == nil {
			hasAny = cols[0]
		}
		groups = append(groups, ifaceCols{idx: len(ifaces), cols: cols})
		ifaces = append(ifaces, f)
	}
	if len(ifaces) == 0 {
		closeAll()
		logrus.Infof("usbpass: hidbridge: %s not bridged (%s); falling back to libusb", dev.InstanceID, strings.Join(skipped, "; "))
		return false, nil
	}
	if len(skipped) > 0 {
		logrus.Warnf("usbpass: hidbridge: some interfaces are left out: %s", strings.Join(skipped, "; "))
	}
	if len(silent) > 0 {
		logrus.Warnf("usbpass: hidbridge: these collections are exported but stay silent (Windows gives no raw reports for mice/keyboards): %s", strings.Join(silent, "; "))
	}

	vid, pid, ver, ok := hasAny.attributes()
	if !ok {
		vid, pid, ver = dev.VID, dev.PID, dev.BCDDevice
	}
	info := hidGenInfo{
		VID: vid, PID: pid, BCD: ver,
		Manufacturer: hasAny.hidString(procHidDGetMfrString),
		Product:      hasAny.hidString(procHidDGetProductString),
		Serial:       hasAny.hidString(procHidDGetSerialString),
		Interfaces:   ifaces,
	}
	backend := newHIDGenBackend(info)

	// Input: collections we may read use hid.dll; the OS-held ones are fed by
	// one Raw Input window, routed by device path.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	type rawTarget struct {
		iface  int
		hasIDs bool
		col    *winHIDCollection
	}
	rawRoutes := map[string]rawTarget{}
	var rawUsages [][2]uint16
	seenUsage := map[[2]uint16]bool{}
	for _, g := range groups {
		f := ifaces[g.idx]
		for _, c := range g.cols {
			switch c.liveKind() {
			case liveRead:
				wg.Add(1)
				go func(i int, hasIDs bool, c *winHIDCollection) {
					defer wg.Done()
					c.readLoop(stop, hasIDs, func(rep []byte) { backend.push(i, rep) })
				}(g.idx, f.HasIDs, c)
			case liveRaw:
				rawRoutes[normalizeHIDID(c.instance)] = rawTarget{iface: g.idx, hasIDs: f.HasIDs, col: c}
				u := [2]uint16{c.pp.UsagePage, c.pp.Usage}
				if !seenUsage[u] {
					seenUsage[u] = true
					rawUsages = append(rawUsages, u)
				}
			}
		}
	}
	var raw *rawInputSource
	if len(rawRoutes) > 0 {
		raw, err = startRawInput(nil, rawUsages, func(ev rawInputEvent) {
			if ev.Type != rimTypeHID {
				return
			}
			t, ok := rawRoutes[normalizeHIDID(ev.Path)]
			if !ok {
				return
			}
			for _, rep := range ev.Reports {
				// Raw Input reports carry the report ID slot even for devices
				// without report IDs; the wire format only has it with IDs.
				if !t.hasIDs && len(rep) == t.col.inLen && len(rep) > 0 {
					rep = rep[1:]
				}
				backend.push(t.iface, rep)
			}
		})
		if err != nil {
			close(stop)
			wg.Wait()
			closeAll()
			logrus.Warnf("usbpass: hidbridge: Raw Input unavailable for %s (%v); falling back to libusb", dev.InstanceID, err)
			return false, nil
		}
	}
	backend.onClose = func() {
		if raw != nil {
			raw.Stop()
		}
		close(stop)
		wg.Wait()
		closeAll()
	}

	dev.Backend = backend
	dev.DeviceDesc = backend.deviceDesc
	dev.ConfigDesc = backend.configDesc
	dev.Class, dev.SubClass, dev.Protocol = 0, 0, 0
	dev.ConfigVal = 1
	dev.NumConfigs = 1
	dev.BCDDevice = ver
	dev.VID, dev.PID = vid, pid
	dev.Interfaces = nil
	for range ifaces {
		dev.Interfaces = append(dev.Interfaces, [3]uint8{0x03, 0x00, 0x00})
	}
	dev.Speed = 2 // full speed, like the real tablets this was built for

	descLen := 0
	for _, f := range ifaces {
		descLen += len(f.ReportDesc)
	}
	logrus.Infof("usbpass: hidbridge claimed %04x:%04x %q via HID (%d interface(s), %d report descriptor bytes, %d collection(s) via Raw Input, no WinUSB needed)",
		vid, pid, info.Product, len(ifaces), descLen, len(rawRoutes))
	return true, nil
}
