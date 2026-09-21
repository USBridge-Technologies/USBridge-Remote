//go:build linux

package usbpass

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// TestLiveX360Linux runs the real Linux claim against a physically attached Xbox
// pad and exports it on 0.0.0.0:3240 so a USB/IP importer can attach:
//
//	USBRIDGE_X360_LIVE_BUS=1-4 USBRIDGE_X360_LIVE_SECS=90 go test -v -run TestLiveX360Linux ./internal/usbpass/
//	usbip attach -r <this host> -b 1-4
//
// Every state change is logged, and rumble commands from the importer are
// printed and forwarded to the pad.
func TestLiveX360Linux(t *testing.T) {
	bus := os.Getenv("USBRIDGE_X360_LIVE_BUS")
	secs, _ := strconv.Atoi(os.Getenv("USBRIDGE_X360_LIVE_SECS"))
	if bus == "" || secs <= 0 {
		t.Skip("USBRIDGE_X360_LIVE_BUS / USBRIDGE_X360_LIVE_SECS not set")
	}
	dev := &ExportedDevice{BusID: bus, Path: "/sys/devices/usbridge/" + bus}
	handled, err := tryClaimX360(dev)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v kind=%q", handled, err, xboxPadKind(bus))
	}
	b := dev.Backend.(*X360Backend)
	prevCmd := b.onCommand
	b.onCommand = func(c X360Command) {
		t.Logf("importer command: %+v", c)
		prevCmd(c)
	}
	srv, err := StartExport("0.0.0.0:3240", []*ExportedDevice{dev})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	t.Logf("EXPORT-READY 0.0.0.0:3240 bus id %s (%ds)", bus, secs)

	var last X360State
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		b.mu.Lock()
		s := b.last
		b.mu.Unlock()
		if s != last {
			t.Logf("state: buttons=%#04x lt=%d rt=%d lx=%d ly=%d rx=%d ry=%d", s.Buttons, s.LT, s.RT, s.LX, s.LY, s.RX, s.RY)
			last = s
		}
	}
}
