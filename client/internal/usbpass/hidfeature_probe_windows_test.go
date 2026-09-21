//go:build windows

package usbpass

import (
	"encoding/hex"
	"os"
	"testing"
)

// TestLiveFeatureProbe asks a real device for feature reports that its HID
// descriptor does not declare (what the vendor driver sends at USB level) and
// reports whether hid.dll lets them through. Read-only (GET_FEATURE).
func TestLiveFeatureProbe(t *testing.T) {
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
			t.Logf("%s: open: %v", n.InstanceID, err)
			continue
		}
		t.Logf("%s (queryOnly=%v, feature len %d, declared feature ids %v)", n.InstanceID, c.queryOnly, c.featLen, c.ids[2])
		for _, id := range []uint8{0x02, 0x03, 0x07, 0x0C, 0x0D, 0x14, 0x16, 0x83, 0xBA, 0xD1} {
			got, err := c.getReport(3, id, 64, true)
			if err != nil {
				t.Logf("   GET_FEATURE %#02x: %v", id, err)
			} else {
				t.Logf("   GET_FEATURE %#02x: OK %s", id, hex.EncodeToString(got))
			}
		}
		c.close()
	}
}
