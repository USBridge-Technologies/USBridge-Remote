package app

import (
	"testing"
	"time"
)

// TestSubmitMoonlightPIN_WaitsForInFlightBackendSwitch is the actual live
// bug this guards against: a client's auto-pair PIN submission landing
// mid-switch used to read a.stream (or send against the backend it pointed
// at) while SetStreamBackend was still tearing down the old process and
// starting the new one -- confirmed live, with an active client session,
// a genuine SetStreamBackend switch in flight, and the very next auto-pair
// attempt getting rejected while it settled. The Moonlight client on the
// other end tries exactly once and falls back to manual PIN entry on any
// failure, so this surfaced as "switching streamers broke auto-pairing".
//
// SetStreamBackend holds streamMu for its entire Stop-old/Start-new/
// WaitReady sequence; SubmitMoonlightPIN must now do the same instead of
// reading a.stream unguarded, so a PIN that arrives mid-switch simply waits
// for it to finish (well inside the client's own 10s HTTP timeout -- see
// SubmitMoonlightPIN's doc comment) instead of racing it.
func TestSubmitMoonlightPIN_WaitsForInFlightBackendSwitch(t *testing.T) {
	a := &App{}

	// Simulate SetStreamBackend mid-switch: it holds streamMu for the
	// duration, and a.stream is genuinely in flux (nil here is enough --
	// this test only needs to prove SubmitMoonlightPIN can't proceed past
	// the lock, not exercise the real streamhost call).
	a.streamMu.Lock()

	done := make(chan error, 1)
	go func() {
		done <- a.SubmitMoonlightPIN("1234")
	}()

	select {
	case <-done:
		t.Fatal("SubmitMoonlightPIN returned while streamMu was still held by the simulated in-flight switch -- it's not actually synchronized with SetStreamBackend")
	case <-time.After(200 * time.Millisecond):
		// Still blocked, as expected.
	}

	a.streamMu.Unlock()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("SubmitMoonlightPIN did not return after the simulated switch released streamMu")
	}
}

// TestListAndUnpairSunshineClient_TakeStreamMu is a lighter check that the
// two sibling pairing-flow methods got the same fix, for the same reason
// (they're read/called from the same GUI clients-list UI that can just as
// easily race a switch).
func TestListAndUnpairSunshineClient_TakeStreamMu(t *testing.T) {
	t.Run("ListSunshineClients", func(t *testing.T) {
		a := &App{}
		a.streamMu.Lock()
		done := make(chan struct{})
		go func() {
			_, _ = a.ListSunshineClients()
			close(done)
		}()
		select {
		case <-done:
			t.Fatal("ListSunshineClients did not block on streamMu")
		case <-time.After(200 * time.Millisecond):
		}
		a.streamMu.Unlock()
		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatal("ListSunshineClients did not return after streamMu was released")
		}
	})

	t.Run("UnpairSunshineClient", func(t *testing.T) {
		a := &App{}
		a.streamMu.Lock()
		done := make(chan struct{})
		go func() {
			_ = a.UnpairSunshineClient("some-uuid")
			close(done)
		}()
		select {
		case <-done:
			t.Fatal("UnpairSunshineClient did not block on streamMu")
		case <-time.After(200 * time.Millisecond):
		}
		a.streamMu.Unlock()
		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatal("UnpairSunshineClient did not return after streamMu was released")
		}
	})
}
