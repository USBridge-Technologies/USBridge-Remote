//go:build windows

package platform

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// Live check of the whole WinMM capture path on a real pad:
//
//	USBRIDGE_PAD_LIVE=winmm:0 USBRIDGE_PAD_LIVE_SECS=60 go test -v -run TestLiveGamepadCapture ./internal/platform/
//
// Press things on the pad; every change of the decoded Moonlight state is
// printed with the same fields the host receives.
func TestLiveGamepadCapture(t *testing.T) {
	id := os.Getenv("USBRIDGE_PAD_LIVE")
	if id == "" {
		t.Skip("set USBRIDGE_PAD_LIVE=winmm:N (or xinput:N) to run against a real pad")
	}
	secs := 30
	if v, err := strconv.Atoi(os.Getenv("USBRIDGE_PAD_LIVE_SECS")); err == nil && v > 0 {
		secs = v
	}
	for _, d := range EnumerateGamepads() {
		t.Logf("pad %s name=%q vid=%s pid=%s", d.ID, d.Name, d.VendorID, d.ProductID)
	}

	var last GamepadCaptureState
	c, err := StartGamepadCapture(id, func(s GamepadCaptureState) {
		near := func(a, b int16) bool { return a-b < 400 && b-a < 400 }
		if s.Buttons == last.Buttons && s.LeftTrigger == last.LeftTrigger && s.RightTrigger == last.RightTrigger &&
			near(s.LeftX, last.LeftX) && near(s.LeftY, last.LeftY) && near(s.RightX, last.RightX) && near(s.RightY, last.RightY) {
			return
		}
		last = s
		t.Logf("buttons=%#04x LT=%3d RT=%3d L=(%6d,%6d) R=(%6d,%6d)", s.Buttons, s.LeftTrigger, s.RightTrigger, s.LeftX, s.LeftY, s.RightX, s.RightY)
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Duration(secs) * time.Second)
	c.Stop()
}
