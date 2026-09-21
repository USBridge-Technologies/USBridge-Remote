package controller

import "testing"

func TestSlotsAreLowestFreeAndStable(t *testing.T) {
	var s gamepadSlots
	for i, id := range []string{"winmm:0", "xinput:0", "winmm:1"} {
		if got, ok := s.assign(id); !ok || got != i {
			t.Fatalf("assign %s = %d,%v want %d", id, got, ok, i)
		}
	}
	// Asking again keeps the slot.
	if got, _ := s.assign("xinput:0"); got != 1 {
		t.Fatalf("slot changed to %d", got)
	}
	// Pad 0 leaves: the others keep their numbers, and the next pad reuses 0.
	if got, ok := s.release("winmm:0"); !ok || got != 0 {
		t.Fatalf("release = %d,%v", got, ok)
	}
	if got, _ := s.slot("xinput:0"); got != 1 {
		t.Fatalf("remaining pad renumbered to %d", got)
	}
	if got, _ := s.assign("winmm:5"); got != 0 {
		t.Fatalf("freed slot not reused: %d", got)
	}
}

func TestMaskFollowsTheAssignedSlots(t *testing.T) {
	var s gamepadSlots
	if s.mask() != 0 {
		t.Fatal("empty table must have an empty mask")
	}
	s.assign("a")
	s.assign("b")
	s.assign("c")
	if got := s.mask(); got != 0b111 {
		t.Fatalf("mask = %04b", got)
	}
	s.release("b")
	if got := s.mask(); got != 0b101 {
		t.Fatalf("mask after release = %04b", got)
	}
}

func TestSlotsAreLimited(t *testing.T) {
	var s gamepadSlots
	for i := 0; i < maxGamepadSlots; i++ {
		if _, ok := s.assign(string(rune('a' + i))); !ok {
			t.Fatalf("slot %d refused", i)
		}
	}
	if _, ok := s.assign("overflow"); ok {
		t.Fatal("more pads than slots must be refused")
	}
	if _, ok := s.slot("overflow"); ok {
		t.Fatal("a refused pad must not hold a slot")
	}
}

func TestIDAtFindsTheHolder(t *testing.T) {
	var s gamepadSlots
	s.assign("a")
	s.assign("b")
	if id, ok := s.idAt(1); !ok || id != "b" {
		t.Fatalf("idAt(1) = %q,%v", id, ok)
	}
	if _, ok := s.idAt(3); ok {
		t.Fatal("empty slot must not resolve")
	}
}

func TestMountingASecondPadKeepsTheFirstOnSoftwareAgents(t *testing.T) {
	pad := func(id, vid, pid string, mounted bool) DriveItem {
		return DriveItem{Source: "gamepad", IsGamepad: true, IsMounted: mounted, GamepadID: id, GamepadVendorID: vid, GamepadProductID: pid}
	}
	first := pad("xinput:0", "0x1532", "0x0a29", true)
	second := pad("winmm:0", "0x1532", "0x1007", false)

	dw := &DiskWidget{agentOS: "Ubuntu 24.04", allDrives: []DriveItem{first, second}}
	keep := dw.keepMountedHIDRequests([]DriveItem{second})
	if len(keep) != 1 || keep[0].Device != "gamepad" || keep[0].ProductID != "0x0a29" {
		t.Fatalf("the already mounted pad must be kept in the batch: %+v", keep)
	}

	// Mounting a pad that is itself mounted must not list it twice.
	dw.allDrives = []DriveItem{first, pad("winmm:0", "0x1532", "0x1007", true)}
	keep = dw.keepMountedHIDRequests([]DriveItem{dw.allDrives[1]})
	if len(keep) != 1 || keep[0].ProductID != "0x0a29" {
		t.Fatalf("only the other pad is kept: %+v", keep)
	}

	// The KVM hardware has one gamepad gadget: a new pad replaces the old one.
	hw := &DiskWidget{agentOS: "USBridge OS", allDrives: []DriveItem{first, second}}
	if keep := hw.keepMountedHIDRequests([]DriveItem{second}); len(keep) != 0 {
		t.Fatalf("hardware keeps a single gamepad: %+v", keep)
	}
}
