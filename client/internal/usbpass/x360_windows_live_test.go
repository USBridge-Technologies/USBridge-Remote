//go:build windows

package usbpass

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestLiveX360Loopback exports the synthetic Xbox 360 on 127.0.0.1:3240 as bus
// id 9-8 so a real importer (usbip-win2's usbip.exe) can attach to it:
//
//	usbip attach -r 127.0.0.1 -b 9-8
//
// Skipped unless USBRIDGE_X360_LIVE_SECS is set. USBRIDGE_X360_SOURCE=xinput
// mirrors the first connected XInput pad (rumble is sent back to it);
// otherwise a scripted pattern is played.
func TestLiveX360Loopback(t *testing.T) {
	secs, _ := strconv.Atoi(os.Getenv("USBRIDGE_X360_LIVE_SECS"))
	if secs <= 0 {
		t.Skip("USBRIDGE_X360_LIVE_SECS not set")
	}
	slot := -1
	if os.Getenv("USBRIDGE_X360_SOURCE") == "xinput" {
		if slot = xinputFirstConnected(); slot < 0 {
			t.Fatal("no XInput pad connected")
		}
		t.Logf("source: XInput slot %d", slot)
	}
	b := NewX360Backend("USBRIDGE-TEST", func(c X360Command) {
		t.Logf("importer command: %+v", c)
		if slot >= 0 && c.Rumble {
			xinputRumble(slot, c.Left, c.Right)
		}
	})
	srv, err := StartExport("127.0.0.1:3240", []*ExportedDevice{b.ExportedDevice("9-8")})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	t.Logf("EXPORT-READY 127.0.0.1:3240 bus id 9-8 (%ds)", secs)

	pattern := []X360State{
		{Buttons: 0x1000},            // A
		{Buttons: 0x2000},            // B
		{LX: 32767},                  // left stick right
		{LT: 255},                    // left trigger
		{Buttons: 0x0100, RY: 20000}, // LB + right stick up
		{},
	}
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	tick := time.NewTicker(8 * time.Millisecond)
	defer tick.Stop()
	start := time.Now()
	for time.Now().Before(deadline) {
		<-tick.C
		if slot >= 0 {
			if s, ok := xinputRead(slot); ok {
				b.SetState(s)
			}
			continue
		}
		b.SetState(pattern[int(time.Since(start)/(2*time.Second))%len(pattern)])
	}
}

func TestXboxSetupClass(t *testing.T) {
	for class, want := range map[string]bool{
		"XboxComposite": true, "XnaComposite": true, "xboxgip": true,
		"HIDClass": false, "USB": false, "": false,
	} {
		if got := isXboxSetupClass(class); got != want {
			t.Errorf("isXboxSetupClass(%q) = %v, want %v", class, got, want)
		}
	}
	if got := x360Serial(`USB\VID_1532&PID_0A29\0000790365644C2A`); got != "0000790365644C2A" {
		t.Errorf("x360Serial = %q", got)
	}
}

// TestLiveX360Claim runs the real claim path against a physically attached Xbox
// pad: USBRIDGE_X360_LIVE_USB='USB\VID_xxxx&PID_yyyy\<serial>'.
func TestLiveX360Claim(t *testing.T) {
	id := os.Getenv("USBRIDGE_X360_LIVE_USB")
	if id == "" {
		t.Skip("USBRIDGE_X360_LIVE_USB not set")
	}
	dev := &ExportedDevice{BusID: "9-8", InstanceID: id}
	handled, err := tryClaimX360(dev)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v (class %q, xinput slot %d)", handled, err, usbSetupClass(id), xinputFirstConnected())
	}
	if dev.VID != x360VID || dev.PID != x360PID || len(dev.Interfaces) != 4 || dev.Backend == nil {
		t.Fatalf("device not rewritten: %+v", dev)
	}
	// Move the sticks / press buttons: reports must follow within the window.
	deadline := time.Now().Add(5 * time.Second)
	got := 0
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		st, data := dev.Backend.HandleBulk(ctx, x360EPIn, true, 32, nil)
		cancel()
		if st == 0 && len(data) == x360ReportLen {
			got++
		}
	}
	t.Logf("received %d reports in 5s", got)
	if err := dev.Backend.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dev.Backend.Close(); err != nil { // idempotent
		t.Fatal(err)
	}
}
