//go:build linux && !android

package platform

import (
	"strings"
	"testing"
)

const procInputSample = `I: Bus=0003 Vendor=1532 Product=0a29 Version=0111
N: Name="Razer Wolverine V2"
P: Phys=usb-0000:00:14.0-2/input0
H: Handlers=event27 js2 
B: PROP=0
B: EV=20000b
B: KEY=7cdb000000000000 0 0 0 0

I: Bus=0003 Vendor=0000 Product=0000 Version=0000
N: Name="usbridge-mouse"
H: Handlers=mouse0 event3 js0 
B: EV=b
B: KEY=1f0000 0 0 0 0

I: Bus=0003 Vendor=046d Product=c31c Version=0110
N: Name="Logitech USB Keyboard"
H: Handlers=sysrq kbd event4 leds 
B: KEY=1000000000007 ff9f207ac14057ff febeffdfffefffff fffffffffffffffe

I: Bus=0003 Vendor=0079 Product=0006 Version=0110
N: Name="Generic USB Joystick"
H: Handlers=event9 js1 
B: KEY=1ff00000000 0 0 0 0
`

func TestParseInputDevicesSkipsPointerWithJSHandler(t *testing.T) {
	pads := parseInputDevices(strings.NewReader(procInputSample))
	var ids []string
	for _, p := range pads {
		ids = append(ids, p.ID)
	}
	want := []string{"/dev/input/event27", "/dev/input/event9"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("pads = %v, want %v", ids, want)
	}
	if pads[0].VendorID != "0x1532" || pads[0].ProductID != "0x0a29" {
		t.Fatalf("vendor/product = %s/%s", pads[0].VendorID, pads[0].ProductID)
	}
}
