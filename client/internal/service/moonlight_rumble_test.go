package service

import "testing"

func TestRumbleHandlerDispatch(t *testing.T) {
	t.Cleanup(func() { SetRumbleHandler(nil) })

	dispatchRumble(0, 1, 2) // no handler installed: must not panic

	var gotC, gotL, gotH uint16
	calls := 0
	SetRumbleHandler(func(c, l, h uint16) { gotC, gotL, gotH = c, l, h; calls++ })
	dispatchRumble(3, 0xAAAA, 0x5555)
	if calls != 1 || gotC != 3 || gotL != 0xAAAA || gotH != 0x5555 {
		t.Fatalf("handler got calls=%d c=%d l=%#x h=%#x", calls, gotC, gotL, gotH)
	}

	SetRumbleHandler(nil)
	dispatchRumble(0, 9, 9)
	if calls != 1 {
		t.Fatalf("removed handler was still called (%d calls)", calls)
	}
}
