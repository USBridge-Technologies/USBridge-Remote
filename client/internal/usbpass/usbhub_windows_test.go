//go:build windows

package usbpass

import (
	"encoding/hex"
	"os"
	"strconv"
	"testing"
)

// TestLiveUSBHubControl reads a device's descriptors through its hub (env
// USBRIDGE_HUB_VIDPID=056a:0374).
func TestLiveUSBHubControl(t *testing.T) {
	vp := os.Getenv("USBRIDGE_HUB_VIDPID")
	if len(vp) != 9 {
		t.Skip("set USBRIDGE_HUB_VIDPID=vvvv:pppp")
	}
	v, _ := strconv.ParseUint(vp[:4], 16, 16)
	d, _ := strconv.ParseUint(vp[5:], 16, 16)
	p, err := findUSBHubPort(uint16(v), uint16(d))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("hub %s port %d", p.hubPath, p.port)
	try := func(name string, bm, br uint8, wv, wi uint16, n int) {
		b, err := p.control(bm, br, wv, wi, n)
		if err != nil {
			t.Logf("%-28s ERR %v", name, err)
			return
		}
		s := hex.EncodeToString(b)
		if len(s) > 80 {
			s = s[:80] + "..."
		}
		t.Logf("%-28s %d bytes %s", name, len(b), s)
	}
	try("device descriptor", 0x80, 0x06, 0x0100, 0, 18)
	try("config descriptor", 0x80, 0x06, 0x0200, 0, 34)
	try("HID report descriptor", 0x81, 0x06, 0x2200, 0, 2048)
	try("GET_REPORT feature 0x14", 0xA1, 0x01, 0x0314, 0, 14)
	try("GET_REPORT feature 0x07", 0xA1, 0x01, 0x0307, 0, 16)
}
