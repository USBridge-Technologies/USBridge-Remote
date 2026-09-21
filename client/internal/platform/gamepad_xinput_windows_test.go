//go:build windows

package platform

import (
	"os"
	"sync"
	"testing"
	"time"
)

func TestXInputToCaptureIsAFieldCopy(t *testing.T) {
	got := xinputToCapture(xinputGamepad{Buttons: 0x1401, LT: 255, RT: 3, LX: -32768, LY: 32767, RX: 5, RY: -6})
	want := GamepadCaptureState{Buttons: 0x1401, LeftTrigger: 255, RightTrigger: 3, LeftX: -32768, LeftY: 32767, RightX: 5, RightY: -6}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestMergeXInputPadsHidesTheWinMMTwin(t *testing.T) {
	winmm := []GamepadDevice{
		{ID: "winmm:0", Name: "Razer Wolverine V2", VendorID: "0x1532", ProductID: "0x0a29"},
		{ID: "winmm:1", Name: "Generic USB Joystick", VendorID: "0x0079", ProductID: "0x0006"},
	}
	xin := []GamepadDevice{{ID: "xinput:0", VendorID: "0x1532", ProductID: "0x0A29"}}
	got := mergeXInputPads(winmm, xin)
	if len(got) != 2 {
		t.Fatalf("got %d pads, want 2: %+v", len(got), got)
	}
	if got[0].ID != "xinput:0" || got[0].Name != "Razer Wolverine V2" {
		t.Errorf("first = %+v, want the XInput pad named after its WinMM twin", got[0])
	}
	if got[1].ID != "winmm:1" {
		t.Errorf("second = %+v, want the DirectInput pad left as winmm:1", got[1])
	}
}

func TestMergeXInputPadsWithoutVIDPIDKeepsBoth(t *testing.T) {
	winmm := []GamepadDevice{{ID: "winmm:0", Name: "Pad", VendorID: "0x045e", ProductID: "0x028e"}}
	xin := []GamepadDevice{{ID: "xinput:0"}} // an older DLL cannot report VID/PID
	got := mergeXInputPads(winmm, xin)
	if len(got) != 2 || got[0].ID != "xinput:0" || got[0].Name == "" {
		t.Fatalf("got %+v", got)
	}
}

// TestLiveXInputCapture captures every connected XInput slot for a few seconds
// and checks that the union of what was seen covers full-range triggers, sticks
// and the buttons. Opt in with USBRIDGE_XINPUT_LIVE=1 while
// `virtual_x360_demo --sweep` (rust-shine) is running.
func TestLiveXInputCapture(t *testing.T) {
	if os.Getenv("USBRIDGE_XINPUT_LIVE") == "" {
		t.Skip("set USBRIDGE_XINPUT_LIVE=1 with virtual_x360_demo --sweep running")
	}
	var mu sync.Mutex
	var seen struct {
		ltMax, rtMax   uint8
		lxMin, lxMax   int16
		lyMin, lyMax   int16
		rxMin, rxMax   int16
		ryMin, ryMax   int16
		buttons        uint16
		captures, pads int
	}
	var caps []*GamepadCapture
	for _, p := range xinputPads() {
		c, err := StartGamepadCapture(p.ID, func(s GamepadCaptureState) {
			mu.Lock()
			defer mu.Unlock()
			seen.captures++
			seen.buttons |= s.Buttons
			seen.ltMax, seen.rtMax = max8(seen.ltMax, s.LeftTrigger), max8(seen.rtMax, s.RightTrigger)
			seen.lxMin, seen.lxMax = min16(seen.lxMin, s.LeftX), max16(seen.lxMax, s.LeftX)
			seen.lyMin, seen.lyMax = min16(seen.lyMin, s.LeftY), max16(seen.lyMax, s.LeftY)
			seen.rxMin, seen.rxMax = min16(seen.rxMin, s.RightX), max16(seen.rxMax, s.RightX)
			seen.ryMin, seen.ryMax = min16(seen.ryMin, s.RightY), max16(seen.ryMax, s.RightY)
		})
		if err != nil {
			t.Fatalf("capture %s: %v", p.ID, err)
		}
		caps = append(caps, c)
		seen.pads++
	}
	if len(caps) == 0 {
		t.Fatal("no XInput pad connected")
	}
	time.Sleep(14 * time.Second)
	for _, c := range caps {
		c.Stop()
	}

	mu.Lock()
	defer mu.Unlock()
	t.Logf("pads=%d captures=%d lt<=%d rt<=%d lx[%d..%d] ly[%d..%d] rx[%d..%d] ry[%d..%d] buttons=%#04x",
		seen.pads, seen.captures, seen.ltMax, seen.rtMax, seen.lxMin, seen.lxMax, seen.lyMin, seen.lyMax,
		seen.rxMin, seen.rxMax, seen.ryMin, seen.ryMax, seen.buttons)
	if seen.ltMax != 255 || seen.rtMax != 255 {
		t.Errorf("triggers do not reach 255 independently: lt=%d rt=%d", seen.ltMax, seen.rtMax)
	}
	if seen.lxMin != -32768 || seen.lxMax != 32767 || seen.lyMin != -32768 || seen.lyMax != 32767 ||
		seen.rxMin != -32768 || seen.rxMax != 32767 || seen.ryMin != -32768 || seen.ryMax != 32767 {
		t.Errorf("sticks do not reach both extremes on every axis")
	}
	if seen.buttons != 0xF3FF && seen.buttons&0xF3FF != 0xF3FF {
		t.Errorf("not every button was seen: %#04x", seen.buttons)
	}
}

func max8(a, b uint8) uint8 {
	if b > a {
		return b
	}
	return a
}

func min16(a, b int16) int16 {
	if b < a {
		return b
	}
	return a
}

func max16(a, b int16) int16 {
	if b > a {
		return b
	}
	return a
}
