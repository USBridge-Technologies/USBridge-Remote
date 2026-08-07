package entitlement

import (
	"encoding/base64"
	"strings"
	"testing"
)

// These tests can't reproduce a genuine backend-signed token (that needs
// the real private key, which by design exists only as
// usbridge-entitlement-backend's own deployment secret, never in this
// repo) -- mirrors rust-shine/crates/license's own tests' same limitation
// and the same rationale: only exercise what doesn't depend on it.

func TestVerify_MalformedToken(t *testing.T) {
	cases := []string{
		"",
		"not-a-token",
		"a.b",     // only two parts
		"a.b.c.d", // four parts
		"wrongprefix." + base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte("sig")),
	}
	for _, tok := range cases {
		if _, err := Verify(tok); err == nil {
			t.Errorf("Verify(%q) = nil error, want an error", tok)
		}
	}
}

func TestVerify_BadSignature(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"provider":"patreon","sub":"1","tier":"patron_$5","iat":1,"exp":9999999999}`))
	garbageSig := base64.RawURLEncoding.EncodeToString(make([]byte, 64)) // right length, wrong bytes
	tok := tokenPrefix + "." + payload + "." + garbageSig

	_, err := Verify(tok)
	if err == nil {
		t.Fatal("Verify with a garbage signature = nil error, want a signature verification failure")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Errorf("Verify error = %q, want it to mention signature verification", err)
	}
}

func TestVerify_ExpiredClaimsRejectedEvenIfSignatureSomehowMatched(t *testing.T) {
	// Regression guard for the ordering in Verify: expiry must be checked
	// AFTER signature verification, not instead of it -- this test only
	// confirms a malformed/unverifiable token with a past-looking exp
	// still fails (for the signature reason, since we can't forge a real
	// one), not that expiry alone is enforced (that needs a real
	// signature, see the package doc comment above).
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"provider":"patreon","sub":"1","tier":"patron_$5","iat":1,"exp":1}`))
	garbageSig := base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	tok := tokenPrefix + "." + payload + "." + garbageSig

	if _, err := Verify(tok); err == nil {
		t.Fatal("Verify with a garbage signature and an expired exp = nil error, want an error")
	}
}
