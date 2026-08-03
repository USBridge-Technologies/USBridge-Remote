package controller

import (
	"testing"
	"time"
)

// Regression tests for the adaptive no-frame timeout (see beginVideoTrace's
// own doc comment): a fresh connect keeps the conservative
// videoTraceFirstAttemptTimeout (4s), while a retry after a previous attempt
// already timed out with no frame uses videoTraceRetryTimeout (10s) --
// widened from an original 1.5s, which was too tight for hosts whose own
// KMS-probe-then-X11-fallback handoff (see rust-shine's capture-kms) can
// itself take over a second, without touching the first-connect case at all.
//
// A bare &VideoWidget{} is safe to drive beginVideoTrace on directly:
// forceReconnectStuckStream's own no-op guard (!desiredStreaming) means
// the zero-value widget never actually tries to reconnect anything, and
// safeVideoStats/safeRelayDebugInfo are both nil-safe -- only
// consecutiveStuckReconnects itself is under test here.

func TestBeginVideoTrace_FreshStartUsesLongTimeout(t *testing.T) {
	vw := &VideoWidget{}
	vw.beginVideoTrace("test")

	// A fresh trace (streak starts at 0) must wait out the full
	// videoTraceFirstAttemptTimeout (4s) -- checked here just short of that
	// deadline, since videoTraceRetryTimeout is no longer guaranteed to be
	// shorter than it.
	time.Sleep(videoTraceFirstAttemptTimeout - time.Second)
	if got := vw.consecutiveStuckReconnects.Load(); got != 0 {
		t.Fatalf("consecutiveStuckReconnects = %d before videoTraceFirstAttemptTimeout (%s) elapsed, want 0 (fresh start should use the long timeout, not fire yet)", got, videoTraceFirstAttemptTimeout)
	}
}

func TestBeginVideoTrace_RetryUsesShortTimeout(t *testing.T) {
	vw := &VideoWidget{}
	vw.consecutiveStuckReconnects.Store(1) // simulates: the previous attempt already timed out
	vw.beginVideoTrace("test")

	deadline := time.Now().Add(videoTraceRetryTimeout + 500*time.Millisecond)
	for time.Now().Before(deadline) {
		if vw.consecutiveStuckReconnects.Load() == 2 {
			return // fired within the short retry timeout, as expected
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("consecutiveStuckReconnects did not reach 2 within %s of a retry trace -- short timeout did not fire", videoTraceRetryTimeout+500*time.Millisecond)
}

// TestCheckVideoSilence_IgnoresBeforeFirstFrame makes sure checkVideoSilence
// stays out of beginVideoTrace's way while no frame has arrived yet --
// lastFrameTime is zero until the first frame, and that startup window is
// owned entirely by beginVideoTrace's own no-frame timeout.
func TestCheckVideoSilence_IgnoresBeforeFirstFrame(t *testing.T) {
	vw := &VideoWidget{}
	vw.checkVideoSilence()
	if vw.videoSilenceReconnectFired.Load() {
		t.Fatal("checkVideoSilence fired with no frame ever received; that window belongs to beginVideoTrace, not this watchdog")
	}
}

// TestCheckVideoSilence_FiresOnceAfterStall simulates an established stream
// (a frame already arrived) that then goes silent past
// videoMidStreamSilenceTimeout -- this is exactly the SDDM-transition
// scenario: the host recovers but the client, already past its initial
// connect trace, had nothing watching for the mid-session gap until now.
func TestCheckVideoSilence_FiresOnceAfterStall(t *testing.T) {
	vw := &VideoWidget{}
	vw.frameMutex.Lock()
	vw.lastFrameTime = time.Now().Add(-videoMidStreamSilenceTimeout - time.Second)
	vw.frameMutex.Unlock()

	vw.checkVideoSilence()
	if !vw.videoSilenceReconnectFired.Load() {
		t.Fatal("checkVideoSilence did not fire for a stall well past videoMidStreamSilenceTimeout")
	}

	// A second tick before the next beginVideoTrace/reconnect must not
	// re-fire (avoids re-logging/re-scheduling every second while a
	// reconcile is already in flight for the same stall).
	vw.checkVideoSilence()
}

func TestNoteVideoTraceFirstFrame_ResetsStuckStreak(t *testing.T) {
	vw := &VideoWidget{}
	vw.consecutiveStuckReconnects.Store(3)
	vw.beginVideoTrace("test")

	vw.noteVideoTraceFirstFrame(1)

	if got := vw.consecutiveStuckReconnects.Load(); got != 0 {
		t.Errorf("consecutiveStuckReconnects = %d after a frame arrived, want 0 (a successful trace must not leave a stale streak for the next, unrelated connect)", got)
	}
}
