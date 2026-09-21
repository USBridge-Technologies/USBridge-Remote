//go:build windows

package usbpass

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var procAuditBeep = syscall.NewLazyDLL("kernel32.dll").NewProc("Beep")

var auditColRe = regexp.MustCompile(`(?i)&col(\d+)`)

// TestLiveWacomAudit records, per phase, which raw HID reports Windows delivers
// for a pen tablet and which bytes of each change. The phases are announced by
// beeps; do what the phase says with the pen / tablet for its whole length.
//
//	USBRIDGE_WACOM_AUDIT=056a:0374 go test -v -run TestLiveWacomAudit ./internal/usbpass/
func TestLiveWacomAudit(t *testing.T) {
	spec := os.Getenv("USBRIDGE_WACOM_AUDIT")
	if spec == "" {
		t.Skip("USBRIDGE_WACOM_AUDIT not set")
	}
	var vid, pid uint16
	if _, err := fmt.Sscanf(spec, "%x:%x", &vid, &pid); err != nil {
		t.Fatalf("bad USBRIDGE_WACOM_AUDIT %q: %v", spec, err)
	}

	phases := []string{
		"IDLE (hands off)",
		"HOVER (pen ~1 cm above the tablet, no buttons)",
		"SIDE-BUTTON-LOWER (pen ~1 cm above the tablet, HOLD the lower side button the whole time)",
		"SIDE-BUTTON-UPPER (pen ~1 cm above the tablet, HOLD the upper side button the whole time)",
		"ERASER (flip the pen, eraser end ~1 cm above the tablet, then touch down)",
		"EXPRESS-KEY-1 (the first key on the tablet: press and release it a few times, pen away)",
		"EXPRESS-KEY-2 (the second key: press and release a few times)",
		"EXPRESS-KEY-3 (the third key: press and release a few times)",
		"EXPRESS-KEY-4 (the fourth key: press and release a few times)",
	}
	phaseLen := 6 * time.Second

	type key struct {
		col int
		id  byte
		n   int
	}
	type agg struct {
		count    int
		min, max []byte
		first    []byte
		inner    map[byte]int // byte 1: the report id inside the 0xDC wrapper
		flags    map[byte]int // byte 2: pen flags of a 0x10 report
	}
	var mu sync.Mutex
	cur := 0
	stats := make([]map[key]*agg, len(phases))
	for i := range stats {
		stats[i] = map[key]*agg{}
	}
	mouseEvents := make([]int, len(phases))

	src, err := startRawInput(
		[]uint16{0x0D, 0xFF00},
		[][2]uint16{{0x01, 0x02}},
		func(ev rawInputEvent) {
			if !rawInputMatchesUSB(ev.Path, vid, pid) {
				return
			}
			col := 0
			if m := auditColRe.FindStringSubmatch(ev.Path); m != nil {
				fmt.Sscanf(m[1], "%d", &col)
			}
			mu.Lock()
			defer mu.Unlock()
			if ev.Type != rimTypeHID {
				mouseEvents[cur]++
				return
			}
			for _, r := range ev.Reports {
				if len(r) == 0 {
					continue
				}
				k := key{col, r[0], len(r)}
				a := stats[cur][k]
				if a == nil {
					a = &agg{min: append([]byte(nil), r...), max: append([]byte(nil), r...), first: append([]byte(nil), r...), inner: map[byte]int{}, flags: map[byte]int{}}
					stats[cur][k] = a
				}
				a.count++
				if len(r) > 2 {
					a.inner[r[1]]++
					if r[1] == 0x10 {
						a.flags[r[2]]++
					}
				}
				for i := range r {
					if r[i] < a.min[i] {
						a.min[i] = r[i]
					}
					if r[i] > a.max[i] {
						a.max[i] = r[i]
					}
				}
			}
		})
	if err != nil {
		t.Fatalf("startRawInput: %v", err)
	}
	defer src.Stop()

	for i := 0; i < 3; i++ {
		procAuditBeep.Call(600, 300)
		time.Sleep(700 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
	for i, name := range phases {
		mu.Lock()
		cur = i
		mu.Unlock()
		t.Logf(">>> PHASE %d: %s", i+1, name)
		procAuditBeep.Call(1000, 150)
		time.Sleep(phaseLen)
	}
	procAuditBeep.Call(500, 400)

	mu.Lock()
	defer mu.Unlock()
	for i, name := range phases {
		t.Logf("=== PHASE %d: %s  (mouse-type raw events: %d)", i+1, name, mouseEvents[i])
		var keys []key
		for k := range stats[i] {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(a, b int) bool {
			if keys[a].col != keys[b].col {
				return keys[a].col < keys[b].col
			}
			return keys[a].id < keys[b].id
		})
		if len(keys) == 0 {
			t.Logf("    (no HID reports)")
		}
		for _, k := range keys {
			a := stats[i][k]
			var varying []string
			for j := range a.min {
				if a.min[j] != a.max[j] {
					varying = append(varying, fmt.Sprintf("[%d]=%02x..%02x", j, a.min[j], a.max[j]))
				}
			}
			if len(varying) > 14 {
				varying = append(varying[:14], fmt.Sprintf("...+%d more", len(varying)-14))
			}
			t.Logf("    Col%02d id=%#02x len=%d  x%d  varying: %v", k.col, k.id, k.n, a.count, varying)
			t.Logf("        inner report ids: %v   pen flags (byte 2) seen: %v", a.inner, a.flags)
			if i > 0 && a.count > 0 {
				t.Logf("        sample: %s", hex.EncodeToString(a.first[:min(len(a.first), 24)]))
			}
		}
	}
}

// TestLiveWacomAux prints every raw report that is not a plain pen report (inner
// id 0x10): the ExpressKeys report and anything else the tablet sends.
//
//	USBRIDGE_WACOM_AUX=056a:0374 go test -v -run TestLiveWacomAux ./internal/usbpass/
func TestLiveWacomAux(t *testing.T) {
	spec := os.Getenv("USBRIDGE_WACOM_AUX")
	if spec == "" {
		t.Skip("USBRIDGE_WACOM_AUX not set")
	}
	var vid, pid uint16
	if _, err := fmt.Sscanf(spec, "%x:%x", &vid, &pid); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	var mu sync.Mutex
	src, err := startRawInput([]uint16{0x0D, 0xFF00}, [][2]uint16{{0x01, 0x02}}, func(ev rawInputEvent) {
		if !rawInputMatchesUSB(ev.Path, vid, pid) || ev.Type != rimTypeHID {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, r := range ev.Reports {
			if len(r) > 2 && r[0] == 0xDC && r[1] == 0x10 {
				continue
			}
			end := min(len(r), 28)
			t.Logf("%5.1fs len=%d %s", time.Since(start).Seconds(), len(r), hex.EncodeToString(r[:end]))
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Stop()
	procAuditBeep.Call(600, 300)
	time.Sleep(25 * time.Second)
}

// TestLiveWacomDescriptor prints the report descriptor Windows lets us reconstruct
// for every HID collection of the tablet, with an item-by-item decode.
//
//	USBRIDGE_HID_LIVE_USB='USB\VID_056A&PID_0374\<instance>' go test -v -run TestLiveWacomDescriptor ./internal/usbpass/
func TestLiveWacomDescriptor(t *testing.T) {
	usb := os.Getenv("USBRIDGE_HID_LIVE_USB")
	if usb == "" {
		t.Skip("USBRIDGE_HID_LIVE_USB not set")
	}
	nodes, err := hidNodesOfUSBDevice(usb)
	if err != nil || len(nodes) == 0 {
		t.Fatalf("nodes: %v %v", nodes, err)
	}
	for _, n := range nodes {
		c, err := openHIDCollection(n.InstanceID)
		if err != nil {
			t.Logf("%s: %v", n.InstanceID, err)
			continue
		}
		desc, err := reconstructReportDescriptor(c.pp)
		c.close()
		if err != nil {
			t.Logf("%s: reconstruct: %v", n.InstanceID, err)
			continue
		}
		t.Logf("=== %s: %d bytes", n.InstanceID, len(desc))
		t.Logf("%s", hex.EncodeToString(desc))
		for _, line := range decodeHIDDescriptor(desc) {
			t.Logf("  %s", line)
		}
	}
}

// decodeHIDDescriptor renders a report descriptor one item per line (short items only).
func decodeHIDDescriptor(d []byte) []string {
	var out []string
	depth := 0
	for i := 0; i < len(d); {
		b := d[i]
		size := int(b & 3)
		if size == 3 {
			size = 4
		}
		typ := (b >> 2) & 3
		tag := b >> 4
		if i+1+size > len(d) {
			break
		}
		var v uint32
		for j := 0; j < size; j++ {
			v |= uint32(d[i+1+j]) << (8 * uint(j))
		}
		name := fmt.Sprintf("item type=%d tag=%#x", typ, tag)
		switch typ {
		case 0:
			name = map[byte]string{8: "Input", 9: "Output", 11: "Feature", 10: "Collection", 12: "End Collection"}[tag]
			if tag == 12 {
				depth--
			}
		case 1:
			name = map[byte]string{0: "Usage Page", 1: "Logical Min", 2: "Logical Max", 3: "Physical Min", 4: "Physical Max", 5: "Unit Exp", 6: "Unit", 7: "Report Size", 8: "Report ID", 9: "Report Count"}[tag]
		case 2:
			name = map[byte]string{0: "Usage", 1: "Usage Min", 2: "Usage Max"}[tag]
		}
		pad := ""
		for k := 0; k < depth; k++ {
			pad += "  "
		}
		out = append(out, fmt.Sprintf("%s%s %#x", pad, name, v))
		if typ == 0 && tag == 10 {
			depth++
		}
		i += 1 + size
	}
	return out
}

var (
	procGetCursorPos = syscall.NewLazyDLL("user32.dll").NewProc("GetCursorPos")
)

// TestLiveWacomSynthFeed proves whether the importer's native Wacom driver really
// consumes the bridged device: it exports the tablet through the HID bridge, then
// (once a real importer has attached it: `usbip attach -r 127.0.0.1 -b 9-9`) feeds
// SYNTHETIC vendor pen reports (a hovering pen circling the tablet) and watches
// the Windows cursor. If the cursor follows the circle, the driver processes it.
//
//	USBRIDGE_WACOM_SYNTH=1 USBRIDGE_HID_IMPORT_USB='USB\VID_056A&PID_0374\<inst>' go test -v -run TestLiveWacomSynthFeed ./internal/usbpass/
func TestLiveWacomSynthFeed(t *testing.T) {
	usb := os.Getenv("USBRIDGE_HID_IMPORT_USB")
	if usb == "" || os.Getenv("USBRIDGE_WACOM_SYNTH") == "" {
		t.Skip("set USBRIDGE_WACOM_SYNTH=1 and USBRIDGE_HID_IMPORT_USB")
	}
	t.Setenv("USBRIDGE_HID_BRIDGE", "1")
	dev := &ExportedDevice{InstanceID: usb, BusID: "9-9", Path: "/sys/devices/usbridge/9-9", Busnum: 9, Devnum: 9}
	if handled, err := tryClaimHID(dev); err != nil || !handled {
		t.Fatalf("tryClaimHID handled=%v err=%v", handled, err)
	}
	b, ok := dev.Backend.(*hidGenBackend)
	if !ok || len(b.ifaces) == 0 {
		t.Fatalf("unexpected backend %T", dev.Backend)
	}
	srv, err := StartExport("127.0.0.1:3240", []*ExportedDevice{dev})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	t.Logf("EXPORT-READY: attach now, the circle starts in 12 s")
	time.Sleep(12 * time.Second)

	type pt struct{ x, y int32 }
	cursor := func() pt {
		var p pt
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
		return p
	}
	minP, maxP := cursor(), cursor()
	sent := 0
	procAuditBeep.Call(1000, 200)
	start := time.Now()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for time.Since(start) < 25*time.Second {
		<-tick.C
		ang := float64(time.Since(start)) / float64(3*time.Second) * 2 * math.Pi
		x := 7600 + int(3500*math.Cos(ang))
		y := 4750 + int(2500*math.Sin(ang))
		r := make([]byte, 193)
		r[0], r[1], r[2] = 0xDC, 0x10, 0x60 // in proximity, not touching
		r[3], r[4], r[5] = byte(x), byte(x>>8), byte(x>>16)
		r[6], r[7], r[8] = byte(y), byte(y>>8), byte(y>>16)
		r[17] = 0x10 // hover distance
		for _, f := range b.ifaces {
			select {
			case f.pending <- r:
				sent++
			default:
			}
		}
		if c := cursor(); true {
			if c.x < minP.x {
				minP.x = c.x
			}
			if c.y < minP.y {
				minP.y = c.y
			}
			if c.x > maxP.x {
				maxP.x = c.x
			}
			if c.y > maxP.y {
				maxP.y = c.y
			}
		}
	}
	t.Logf("sent %d synthetic reports; cursor x %d..%d, y %d..%d (a moving cursor means the native driver processed the virtual tablet)", sent, minP.x, maxP.x, minP.y, maxP.y)
}

// TestLiveWacomModelFeed exports the CTL-4100 from the captured model (no real
// tablet needed) on 127.0.0.1:3240 and feeds it a circle of hovering pen
// reports in the tablet's own wire format (report 0x10, 27 bytes). Attach with
// usbip.exe when EXPORT-READY shows; a moving cursor means the native driver
// consumes the model.
func TestLiveWacomModelFeed(t *testing.T) {
	if os.Getenv("USBRIDGE_WACOM_MODEL") == "" {
		t.Skip("set USBRIDGE_WACOM_MODEL=1")
	}
	m := wacomModelFor(0x056a, 0x0374)
	if m == nil {
		t.Fatal("no model")
	}
	if os.Getenv("USBRIDGE_WACOM_DB") != "" { // the generic path: database descriptor + the device's own descriptors
		var dd, cd []byte
		if hp, err := findUSBHubPort(0x056a, 0x0374); err == nil {
			dd, _ = hp.control(0x80, 0x06, 0x0100, 0, 18)
			cd, _ = hp.control(0x80, 0x06, 0x0200, 0, 34)
		}
		if m = wacomModelFromDB(0x056a, 0x0374, 0x0111, "Wacom Co.,Ltd.", "Intuos S", "1EH00R2017722", dd, cd); m == nil {
			t.Fatal("no DB model")
		}
		t.Logf("DB model %q: %d interface(s), live descriptors %v", m.Name, len(m.Ifaces), dd != nil)
	}
	if os.Getenv("USBRIDGE_WACOM_ZERO") != "" { // do the drivers need the real feature values?
		z := *m
		zi := *m.Ifaces[0]
		zi.Features = map[uint8][]byte{}
		for id, v := range m.Ifaces[0].Features {
			if v != nil {
				zi.Features[id] = append([]byte{id}, make([]byte, len(v)-1)...)
			} else {
				zi.Features[id] = nil
			}
		}
		z.Ifaces = []*wacomIface{&zi}
		m = &z
	}
	dev := &ExportedDevice{BusID: "9-9", Path: "/sys/devices/usbridge/9-9", Busnum: 9, Devnum: 9}
	b := applyWacomModel(dev, m)
	srv, err := StartExport("127.0.0.1:3240", []*ExportedDevice{dev})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	t.Logf("EXPORT-READY")
	time.Sleep(20 * time.Second)

	type pt struct{ x, y int32 }
	cursor := func() pt {
		var p pt
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
		return p
	}
	minP, maxP := cursor(), cursor()
	sent := 0
	start := time.Now()
	tick := time.NewTicker(8 * time.Millisecond)
	defer tick.Stop()
	for time.Since(start) < 15*time.Second {
		<-tick.C
		ang := float64(time.Since(start)) / float64(3*time.Second) * 2 * math.Pi
		x := 7600 + int(3500*math.Cos(ang))
		y := 4750 + int(2500*math.Sin(ang))
		r := make([]byte, 27)
		r[0], r[1] = 0x10, 0x60
		r[2], r[3], r[4] = byte(x), byte(x>>8), byte(x>>16)
		r[5], r[6], r[7] = byte(y), byte(y>>8), byte(y>>16)
		b.push(0, r)
		sent++
		c := cursor()
		minP.x, minP.y = min(minP.x, c.x), min(minP.y, c.y)
		maxP.x, maxP.y = max(maxP.x, c.x), max(maxP.y, c.y)
	}
	t.Logf("sent %d reports; cursor x %d..%d, y %d..%d", sent, minP.x, maxP.x, minP.y, maxP.y)
}

// TestLiveWacomModelRemote is TestLiveWacomModelFeed with the reports of a real
// tablet on another machine: USBRIDGE_WACOM_STREAM is a command that prints one
// hex-encoded HID input report per line (e.g. python3 reading /dev/hidraw0 over
// ssh). Move the pen once the attach message shows.
func TestLiveWacomModelRemote(t *testing.T) {
	cmdline := os.Getenv("USBRIDGE_WACOM_STREAM")
	if cmdline == "" {
		t.Skip("set USBRIDGE_WACOM_STREAM")
	}
	m := wacomModelFor(0x056a, 0x0374)
	dev := &ExportedDevice{BusID: "9-9", Path: "/sys/devices/usbridge/9-9", Busnum: 9, Devnum: 9}
	b := applyWacomModel(dev, m)
	srv, err := StartExport("127.0.0.1:3240", []*ExportedDevice{dev})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	t.Logf("EXPORT-READY")
	time.Sleep(20 * time.Second)

	cmd := exec.Command("cmd", "/C", cmdline)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	procAuditBeep.Call(1000, 200)
	t.Logf("STREAMING: move the pen")

	type pt struct{ x, y int32 }
	cursor := func() pt {
		var p pt
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
		return p
	}
	minP, maxP := cursor(), cursor()
	var pushed, maxPressure atomic.Int64
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			raw, err := hex.DecodeString(strings.TrimSpace(sc.Text()))
			if err != nil {
				continue
			}
			if w := m.wireReport(raw); w != nil {
				b.push(0, w)
				pushed.Add(1)
				if w[0] == 0x10 {
					if p := int64(w[8]) | int64(w[9])<<8; p > maxPressure.Load() {
						maxPressure.Store(p)
					}
				}
			}
		}
	}()
	for start := time.Now(); time.Since(start) < 40*time.Second; time.Sleep(10 * time.Millisecond) {
		c := cursor()
		minP.x, minP.y = min(minP.x, c.x), min(minP.y, c.y)
		maxP.x, maxP.y = max(maxP.x, c.x), max(maxP.y, c.y)
	}
	t.Logf("pushed %d real reports (max pressure %d); cursor x %d..%d, y %d..%d", pushed.Load(), maxPressure.Load(), minP.x, maxP.x, minP.y, maxP.y)
}
