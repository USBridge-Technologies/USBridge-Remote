package entitlement

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// These first tests can't reproduce a genuine backend-signed token (that
// needs the real private key, which by design exists only as
// usbridge-entitlement-backend's own deployment secret, never in this
// repo) -- mirrors rust-shine/crates/license's own tests' same limitation
// and the same rationale: only exercise what doesn't depend on it.
//
// TestVerify_RealSignature* below close that gap using verifyWithKey and a
// throwaway keypair generated fresh per test run -- a REAL signature that
// verifies, just not one the production public key accepts, which is all
// that's needed to test the expiry logic and tamper-detection genuinely.

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

// signTestToken builds a real, verifiable usbent1 token with the given
// keypair and expiry -- same wire format as entitlement.ts's sign()
// (usbent1.<base64url(payload text)>.<base64url(sig over payload text
// bytes)>), just signed with a throwaway key instead of the real one.
func signTestToken(t *testing.T, priv ed25519.PrivateKey, exp int64) string {
	t.Helper()
	claims := Claims{Provider: "patreon", Sub: "test-user", Tier: "patron_$5", IssuedAt: time.Now().Unix(), ExpireAt: exp}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	sig := ed25519.Sign(priv, []byte(payloadB64))
	return fmt.Sprintf("%s.%s.%s", tokenPrefix, payloadB64, base64.RawURLEncoding.EncodeToString(sig))
}

func TestVerify_RealSignature_ValidAndUnexpired(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate throwaway keypair: %v", err)
	}
	tok := signTestToken(t, priv, time.Now().Add(time.Hour).Unix())

	claims, err := verifyWithKey(tok, pub)
	if err != nil {
		t.Fatalf("verifyWithKey(valid, unexpired) = error %v, want success", err)
	}
	if claims.Tier != "patron_$5" || claims.Sub != "test-user" {
		t.Errorf("claims round-tripped wrong: %+v", claims)
	}
}

func TestVerify_RealSignature_ExpiredIsRejected(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate throwaway keypair: %v", err)
	}
	tok := signTestToken(t, priv, time.Now().Add(-time.Hour).Unix())

	_, err = verifyWithKey(tok, pub)
	if err == nil {
		t.Fatal("verifyWithKey(valid signature, past exp) = nil error, want rejection")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("verifyWithKey error = %q, want it to mention expiry", err)
	}
}

// TestVerify_RealSignature_WallClockNotCached is the actual "does the 72h
// TTL / offline-grace mechanism really turn itself off" proof the license
// scheme depends on, compressed from real hours down to real milliseconds:
// a token that's genuinely valid right now must become genuinely rejected
// the instant wall-clock time crosses its exp, with no restart or cache
// invalidation needed -- exactly what recheckEntitlement's local-fallback
// path and the Rust watchdog both rely on between backend check-ins.
func TestVerify_RealSignature_WallClockNotCached(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate throwaway keypair: %v", err)
	}
	tok := signTestToken(t, priv, time.Now().Add(200*time.Millisecond).Unix()+1) // >=1s out, Unix() truncates to whole seconds

	if _, err := verifyWithKey(tok, pub); err != nil {
		t.Fatalf("verifyWithKey should still accept this token immediately after signing: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := verifyWithKey(tok, pub); err == nil {
		t.Fatal("verifyWithKey accepted a token whose exp has now passed -- expiry check is not wall-clock live")
	}
}

// TestVerify_RealSignature_TamperedPayloadIsRejected demonstrates the core
// tamper-resistance guarantee directly: start from a genuinely
// valid-signature token, flip a claim, and confirm the signature check
// (not just "well-formed JSON") is what actually gates every field --
// nobody can hand-edit tier/exp/sub in a real token without invalidating
// it, unlike the pre-existing bad-signature tests which use a signature
// that was never valid for anything.
func TestVerify_RealSignature_TamperedPayloadIsRejected(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate throwaway keypair: %v", err)
	}
	tok := signTestToken(t, priv, time.Now().Add(time.Hour).Unix())
	parts := strings.Split(tok, ".")

	claims := Claims{Provider: "patreon", Sub: "test-user", Tier: "patron_$50", IssuedAt: time.Now().Unix(), ExpireAt: time.Now().Add(time.Hour).Unix()}
	tamperedPayload, _ := json.Marshal(claims)
	tampered := tokenPrefix + "." + base64.RawURLEncoding.EncodeToString(tamperedPayload) + "." + parts[2]

	if _, err := verifyWithKey(tampered, pub); err == nil {
		t.Fatal("verifyWithKey accepted a token whose payload was edited after signing (tier bumped) -- should be impossible")
	}
}
