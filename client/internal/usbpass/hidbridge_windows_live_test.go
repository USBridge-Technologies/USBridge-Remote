//go:build windows

package usbpass

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestLiveWindowsHIDBridge exercises the HID bridge against a real, physically
// attached HID device. It is skipped unless USBRIDGE_HID_LIVE_USB holds the
// device's USB instance ID (for example USB\VID_056A&PID_0374\6&2100962A&0&1).
// Move the pen / press buttons during the read window to see input reports.
func TestLiveWindowsHIDBridge(t *testing.T) {
	usb := os.Getenv("USBRIDGE_HID_LIVE_USB")
	if usb == "" {
		t.Skip("USBRIDGE_HID_LIVE_USB not set")
	}
	t.Setenv("USBRIDGE_HID_BRIDGE", "1") // the bridge is opt-in
	nodes, err := hidNodesOfUSBDevice(usb)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatalf("no HID nodes below %s", usb)
	}
	total := 0
	for _, n := range nodes {
		c, err := openHIDCollection(n.InstanceID)
		if err != nil {
			t.Logf("node %s (iface %q): open failed: %v", n.InstanceID, n.Interface, err)
			continue
		}
		desc, err := reconstructReportDescriptor(c.pp)
		if err != nil {
			t.Logf("node %s: reconstruct failed: %v", n.InstanceID, err)
			c.close()
			continue
		}
		total += len(desc)
		colls := hidDescriptorWalk(t, desc)
		t.Logf("node %s: top usage %04x:%04x, %d bytes, %d collection(s), report lengths in/out/feature = %d/%d/%d, ids=%v",
			n.InstanceID, c.pp.UsagePage, c.pp.Usage, len(desc), colls, c.inLen, c.outLen, c.featLen, c.ids)
		t.Logf("  descriptor: %s", hex.EncodeToString(desc))
		c.close()
	}
	t.Logf("all collections together: %d descriptor bytes", total)

	dev := &ExportedDevice{InstanceID: usb}
	handled, err := tryClaimHID(dev)
	if err != nil || !handled {
		t.Fatalf("tryClaimHID handled=%v err=%v", handled, err)
	}
	defer dev.Backend.Close()

	cfg := dev.ConfigDesc
	if int(binary.LittleEndian.Uint16(cfg[2:])) != len(cfg) {
		t.Fatalf("config wTotalLength mismatch")
	}
	t.Logf("claimed: %04x:%04x, %d interface(s), config %d bytes, HID wDescriptorLength=%d",
		dev.VID, dev.PID, len(dev.Interfaces), len(cfg), binary.LittleEndian.Uint16(cfg[9+9+7:]))

	// Read the descriptor back through the URB path.
	// (setup: GET_DESCRIPTOR(REPORT) on interface 0)
	st, rd := dev.Backend.HandleControl(nil, [8]byte{0x81, 0x06, 0x00, 0x22, 0x00, 0x00, 0x00, 0x10}, 4096, nil)
	if st != 0 || len(rd) == 0 {
		t.Fatalf("report descriptor over the control path: st=%d len=%d", st, len(rd))
	}

	deadline := time.Now().Add(12 * time.Second)
	got := 0
	t.Log("reading input reports for 12s (move the pen / press buttons now)...")
	for time.Now().Before(deadline) {
		type res struct {
			st  int32
			rep []byte
		}
		ch := make(chan res, 1)
		go func() {
			s, r := dev.Backend.HandleBulk(timeoutCtx(500*time.Millisecond), 0x81, true, 64, nil)
			ch <- res{s, r}
		}()
		r := <-ch
		if r.st == 0 {
			got++
			if got <= 8 {
				t.Logf("  input report %d: %s", got, strings.ToUpper(hex.EncodeToString(r.rep)))
			}
		}
	}
	t.Logf("received %d input report(s) in 12s", got)
}

// TestLiveHIDImportServer exports a physically attached HID device over plain
// USB/IP on 127.0.0.1:3240 through the HID bridge and holds it for
// USBRIDGE_HID_IMPORT_SECS seconds, so a real importer (usbip-win2's usbip.exe)
// can attach to it: `usbip attach -r 127.0.0.1 -b 9-9`.
// Skipped unless USBRIDGE_HID_IMPORT_USB names the device's USB instance ID.
func TestLiveHIDImportServer(t *testing.T) {
	usb := os.Getenv("USBRIDGE_HID_IMPORT_USB")
	if usb == "" {
		t.Skip("USBRIDGE_HID_IMPORT_USB not set")
	}
	t.Setenv("USBRIDGE_HID_BRIDGE", "1") // the bridge is opt-in
	secs := 120
	if v, err := strconv.Atoi(os.Getenv("USBRIDGE_HID_IMPORT_SECS")); err == nil && v > 0 {
		secs = v
	}
	dev := &ExportedDevice{InstanceID: usb, BusID: "9-9", Path: "/sys/devices/usbridge/9-9", Busnum: 9, Devnum: 9}
	handled, err := tryClaimHID(dev)
	if err != nil || !handled {
		t.Fatalf("tryClaimHID handled=%v err=%v", handled, err)
	}
	srv, err := StartExport("127.0.0.1:3240", []*ExportedDevice{dev})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	t.Logf("EXPORT-READY 127.0.0.1:3240 bus id 9-9 (holding %ds)", secs)
	time.Sleep(time.Duration(secs) * time.Second)
}
