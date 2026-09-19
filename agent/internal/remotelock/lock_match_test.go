package remotelock

import (
	"reflect"
	"testing"
)

func TestEventPathsFromDevices_SunshinePassthrough(t *testing.T) {
	const sample = `I: Bus=0003 Vendor=beef Product=dead Version=0111
N: Name="Mouse passthrough"
P: Phys=
S: Sysfs=/devices/virtual/input/input20
U: Uniq=
H: Handlers=mouse1 event12

I: Bus=0003 Vendor=beef Product=dead Version=0111
N: Name="Keyboard passthrough"
H: Handlers=sysrq kbd event13

I: Bus=0003 Vendor=046d Product=c52b Version=0111
N: Name="Logitech USB Receiver"
H: Handlers=mouse0 event4
`
	got := eventPathsFromDevices(sample)
	want := []string{"/dev/input/event12", "/dev/input/event13"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("eventPathsFromDevices = %v, want %v", got, want)
	}
}

func TestIsVirtualInput_UsbridgeNames(t *testing.T) {
	if !isVirtualInput("usbridge-mouse", 0, 0) {
		t.Fatal("usbridge-mouse must be treated as remote")
	}
	if isVirtualInput("AT Translated Set 2 keyboard", 0x1, 0x1) {
		t.Fatal("real laptop keyboard must not be grabbed")
	}
}
