package gui

import (
	"testing"
	"time"
)

// TestConnectionRecoveryRetryDelaysCoverRealisticTsnetReconnect guards
// against connectionRecoveryRetryDelays shrinking back down below what a
// real Tailscale/tsnet re-establishment needs after a client-side network
// path change (Wi-Fi<->cellular handoff, AP roam, DHCP renewal).
//
// Field logs (Android, tsnet client) show netcheck probing every DERP
// region plus a WireGuard re-handshake taking 15-30s after such a blip. The
// original {1,2,5}s schedule (~8s total) gave up mid-reconnect on every
// such hiccup, surfacing it to the user as a full "connection lost" dialog
// instead of riding it out -- see main_window_connection.go's
// tryRecoverConnectionAfterLoss.
func TestConnectionRecoveryRetryDelaysCoverRealisticTsnetReconnect(t *testing.T) {
	delays := connectionRecoveryRetryDelays

	if len(delays) < 4 {
		t.Fatalf("expected at least 4 recovery attempts, got %d (%v) -- too few attempts gives up before tsnet reconnects", len(delays), delays)
	}

	var total time.Duration
	for i, d := range delays {
		if d <= 0 {
			t.Fatalf("delay[%d] = %v, must be positive", i, d)
		}
		if i > 0 && d < delays[i-1] {
			t.Fatalf("delay[%d] = %v is shorter than delay[%d] = %v; schedule should back off, not speed up", i, d, i-1, delays[i-1])
		}
		total += d
	}

	const minBudget = 30 * time.Second
	if total < minBudget {
		t.Fatalf("total recovery budget = %v, want at least %v to outlast a real tsnet reconnect after a network path change (observed 15-30s in the field)", total, minBudget)
	}
}
