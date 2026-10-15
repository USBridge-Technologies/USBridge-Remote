//go:build windows

package displaypower

import (
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"
)

// The structs are passed to user32 as raw memory: their sizes must match the SDK's.
func TestStructSizesMatchTheSDK(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want uintptr
	}{
		{"DISPLAYCONFIG_PATH_INFO", unsafe.Sizeof(pathInfo{}), 72},
		{"DISPLAYCONFIG_MODE_INFO", unsafe.Sizeof(modeInfo{}), 64},
		{"DISPLAYCONFIG_TARGET_DEVICE_NAME", unsafe.Sizeof(targetDeviceName{}), 420},
		{"DISPLAYCONFIG_SOURCE_DEVICE_NAME", unsafe.Sizeof(sourceDeviceName{}), 84},
		{"DEVMODEW", unsafe.Sizeof(devMode{}), 220},
	} {
		if c.got != c.want {
			t.Errorf("%s: %d bytes, want %d", c.name, c.got, c.want)
		}
	}
}

// Read-only: lists the box's monitors and checks there is exactly one primary.
func TestListReportsOnePrimary(t *testing.T) {
	ms, err := List()
	if err != nil {
		t.Fatal(err)
	}
	primaries := 0
	for _, m := range ms {
		t.Logf("%+v", m)
		if m.Primary {
			primaries++
		}
	}
	if primaries != 1 {
		t.Errorf("%d primary monitors, want 1", primaries)
	}
}

// Live: USBRIDGE_LIVE_DISPLAYPOWER=<part of a monitor's name> switches that monitor off,
// checks it went, switches it back on and checks it came back.
func TestLiveToggle(t *testing.T) {
	want := os.Getenv("USBRIDGE_LIVE_DISPLAYPOWER")
	if want == "" {
		t.Skip("set USBRIDGE_LIVE_DISPLAYPOWER=<monitor name> to toggle a real monitor")
	}
	find := func() Monitor {
		ms, err := List()
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ms {
			if strings.Contains(strings.ToLower(m.Name), strings.ToLower(want)) {
				return m
			}
		}
		t.Fatalf("no monitor named like %q", want)
		return Monitor{}
	}
	m := find()
	start := m.Active
	for _, on := range []bool{!start, start} {
		if err := SetEnabled(m.ID, on); err != nil {
			t.Fatalf("SetEnabled(%v): %v", on, err)
		}
		time.Sleep(3 * time.Second)
		if got := find(); got.Active != on {
			t.Fatalf("after SetEnabled(%v) the monitor is active=%v", on, got.Active)
		}
		t.Logf("%s active=%v ok", m.Name, on)
	}
	primaries := 0
	ms, _ := List()
	for _, x := range ms {
		if x.Primary {
			primaries++
			t.Logf("primary: %s", x.Name)
		}
	}
	if primaries != 1 {
		t.Errorf("%d primaries after the round trip", primaries)
	}
}

// An unknown monitor is reported, and nothing on the desktop changes.
func TestUnknownMonitorIsNotFound(t *testing.T) {
	before, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, on := range []bool{true, false} {
		if err := SetEnabled(`\?\DISPLAY#NOPE0000#0&0&0&UID0#{e6f07b5f-ee97-4a90-b076-33f57bf4eaa7}`, on); err != ErrNotFound {
			t.Errorf("SetEnabled(unknown, %v) = %v, want ErrNotFound", on, err)
		}
	}
	after, _ := List()
	if len(after) != len(before) {
		t.Errorf("monitor list changed: %d -> %d", len(before), len(after))
	}
}
