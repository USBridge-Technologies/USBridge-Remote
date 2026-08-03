package controller

import (
	"testing"
	"time"
)

// Regression tests for the adaptive no-frame timeout (see beginVideoTrace's
// own doc comment): a fresh connect keeps the conservative
// videoTraceFirstAttemptTimeout (4s), but once a previous attempt already
// timed out with no frame, subsequent retries switch to the much shorter
// videoTraceRetryTimeout (1.5s) -- this is what actually shrinks the
// SDDM login/logout recovery window from ~10-15s down to a couple of
// seconds, without touching the first-connect case at all.
//
// A bare &VideoWidget{} is safe to drive beginVideoTrace on directly:
// forceReconnectStuckStream's own no-op guard (!desiredStreaming) means
// the zero-value widget never actually tries to reconnect anything, and
// safeVideoStats/safeRelayDebugInfo are both nil-safe -- only
// consecutiveStuckReconnects itself is under test here.

func TestBeginVideoTrace_FreshStartUsesLongTimeout(t *testing.T) {
	vw := &VideoWidget{}
	vw.beginVideoTrace("test")

	// videoTraceRetryTimeout (1.5s) has already elapsed, but a fresh trace
	// (streak starts at 0) must still be waiting out the full 4s
	// videoTraceFirstAttemptTimeout -- the streak counter must not have
	// incremented yet.
	time.Sleep(videoTraceRetryTimeout + 200*time.Millisecond)
	if got := vw.consecutiveStuckReconnects.Load(); got != 0 {
		t.Fatalf("consecutiveStuckReconnects = %d after %s, want 0 (fresh start should use the long timeout, not fire yet)", got, videoTraceRetryTimeout)
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

func TestNoteVideoTraceFirstFrame_ResetsStuckStreak(t *testing.T) {
	vw := &VideoWidget{}
	vw.consecutiveStuckReconnects.Store(3)
	vw.beginVideoTrace("test")

	vw.noteVideoTraceFirstFrame(1)

	if got := vw.consecutiveStuckReconnects.Load(); got != 0 {
		t.Errorf("consecutiveStuckReconnects = %d after a frame arrived, want 0 (a successful trace must not leave a stale streak for the next, unrelated connect)", got)
	}
}
