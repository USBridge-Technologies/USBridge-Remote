package api

import "testing"

// TestCalculateHMAC_WebRTCOfferKnownVector pins CalculateHMAC's exact wire
// format against a fixed vector shared with rust-shine's
// crates/webrtc-video/src/signaling.rs (see that file's
// calculate_hmac_matches_known_vector test, which asserts the identical
// expected hex string for the identical inputs). The two implementations
// live in separate languages/repos with no shared source of truth -- this
// is what would catch either side silently drifting from the other, which
// would otherwise only surface as every /webrtc/offer request failing with
// a live 401.
func TestCalculateHMAC_WebRTCOfferKnownVector(t *testing.T) {
	got := CalculateHMAC("POST", "/webrtc/offer", "1700000000", `{"sdp":"v=0..."}`, []byte("test-master-key"))
	want := "62891cc77d10dab3aeef5ea25753c356396242c37d8264787ddd76761b347327"
	if got != want {
		t.Fatalf("CalculateHMAC mismatch (would break rust-shine's mirrored test vector too): got %s, want %s", got, want)
	}
}
