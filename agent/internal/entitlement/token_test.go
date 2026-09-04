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
// that's needed to test the expiry/hardware-binding logic and
// tamper-detection genuinely.

const testHwID = "test-hw-id-abc123"

func TestVerify_MalformedToken(t *testing.T) {
	cases := []string{
		"",
		"not-a-token",
		"a.b",     // only two parts
		"a.b.c.d", // four parts
		"wrongprefix." + base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte("sig")),
	}
	for _, tok := range cases {
		if _, err := VerifyForHardware(tok, testHwID); err == nil {
			t.Errorf("VerifyForHardware(%q) = nil error, want an error", tok)
		}
	}
}

func TestVerify_BadSignature(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"provider":"desktop-device","sub":"` + testHwID + `","tier":"desktop-license","iat":1,"exp":9999999999}`))
	garbageSig := base64.RawURLEncoding.EncodeToString(make([]byte, 64)) // right length, wrong bytes
	tok := tokenPrefix + "." + payload + "." + garbageSig

	_, err := VerifyForHardware(tok, testHwID)
	if err == nil {
		t.Fatal("VerifyForHardware with a garbage signature = nil error, want a signature verification failure")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Errorf("VerifyForHardware error = %q, want it to mention signature verification", err)
	}
}

func TestVerify_ExpiredClaimsRejectedEvenIfSignatureSomehowMatched(t *testing.T) {
	// Regression guard for the ordering in VerifyForHardware: expiry must
	// be checked AFTER signature verification, not instead of it -- this
	// test only confirms a malformed/unverifiable token with a
	// past-looking exp still fails (for the signature reason, since we
	// can't forge a real one), not that expiry alone is enforced (that
	// needs a real signature, see the package doc comment above).
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"provider":"desktop-device","sub":"` + testHwID + `","tier":"desktop-license","iat":1,"exp":1}`))
	garbageSig := base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	tok := tokenPrefix + "." + payload + "." + garbageSig

	if _, err := VerifyForHardware(tok, testHwID); err == nil {
		t.Fatal("VerifyForHardware with a garbage signature and an expired exp = nil error, want an error")
	}
}

// signTestToken builds a real, verifiable usbent1 token with the given
// keypair, provider, hw id, and expiry -- same wire format as
// entitlement.ts's sign() (usbent1.<base64url(payload text)>.<base64url(sig
// over payload text bytes)>), just signed with a throwaway key instead of
// the real one.
func signTestToken(t *testing.T, priv ed25519.PrivateKey, provider, hwID string, exp int64) string {
	t.Helper()
	claims := Claims{Provider: provider, Sub: hwID, Tier: "desktop-license", IssuedAt: time.Now().Unix(), ExpireAt: exp}
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
	tok := signTestToken(t, priv, ProviderDesktopLicense, testHwID, time.Now().Add(time.Hour).Unix())

	claims, err := verifyWithKey(tok, pub, testHwID)
	if err != nil {
		t.Fatalf("verifyWithKey(valid, unexpired) = error %v, want success", err)
	}
	if claims.Tier != "desktop-license" || claims.Sub != testHwID {
		t.Errorf("claims round-tripped wrong: %+v", claims)
	}
}

func TestVerify_RealSignature_TrialProviderAccepted(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate throwaway keypair: %v", err)
	}
	tok := signTestToken(t, priv, ProviderDesktopTrial, testHwID, time.Now().Add(time.Hour).Unix())

	if _, err := verifyWithKey(tok, pub, testHwID); err != nil {
		t.Fatalf("verifyWithKey should accept a %s-provider token: %v", ProviderDesktopTrial, err)
	}
}

func TestVerify_RealSignature_WrongProviderRejected(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate throwaway keypair: %v", err)
	}
	// A real, validly-signed token, but for a DIFFERENT token kind (e.g.
	// the SBC "mender-device" provider) -- must never be accepted here
	// even though the signature and hw id both check out, since it wasn't
	// minted as a desktop license/trial token.
	tok := signTestToken(t, priv, "mender-device", testHwID, time.Now().Add(time.Hour).Unix())

	if _, err := verifyWithKey(tok, pub, testHwID); err == nil {
		t.Fatal("verifyWithKey accepted a token with an unrecognized provider -- should be impossible")
	}
}

func TestVerify_RealSignature_WrongHardwareRejected(t *testing.T) {
	// This IS the hardware-binding guarantee: a genuinely valid, unexpired,
	// correctly-provider'd token minted for one machine must be refused by
	// a DIFFERENT machine's local hw id check -- this is what makes a
	// copied token file not work elsewhere, since the backend itself would
	// happily re-verify it (it only checks the signature, not who's asking).
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate throwaway keypair: %v", err)
	}
	tok := signTestToken(t, priv, ProviderDesktopLicense, "hw-id-of-machine-A", time.Now().Add(time.Hour).Unix())

	if _, err := verifyWithKey(tok, pub, "hw-id-of-machine-B"); err == nil {
		t.Fatal("verifyWithKey accepted a token bound to a different hardware id -- hardware binding is broken")
	}
}

func TestVerify_RealSignature_ExpiredIsRejected(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate throwaway keypair: %v", err)
	}
	tok := signTestToken(t, priv, ProviderDesktopLicense, testHwID, time.Now().Add(-time.Hour).Unix())

	_, err = verifyWithKey(tok, pub, testHwID)
	if err == nil {
		t.Fatal("verifyWithKey(valid signature, past exp) = nil error, want rejection")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("verifyWithKey error = %q, want it to mention expiry", err)
	}
}

// TestVerify_RealSignature_WallClockNotCached is the actual "does the TTL /
// offline-grace mechanism really turn itself off" proof the license scheme
// depends on, compressed from real hours down to real milliseconds: a
// token that's genuinely valid right now must become genuinely rejected
// the instant wall-clock time crosses its exp, with no restart or cache
// invalidation needed -- exactly what recheckEntitlement's local-fallback
// path and the Rust watchdog both rely on between backend check-ins.
func TestVerify_RealSignature_WallClockNotCached(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate throwaway keypair: %v", err)
	}
	tok := signTestToken(t, priv, ProviderDesktopLicense, testHwID, time.Now().Add(200*time.Millisecond).Unix()+1) // >=1s out, Unix() truncates to whole seconds

	if _, err := verifyWithKey(tok, pub, testHwID); err != nil {
		t.Fatalf("verifyWithKey should still accept this token immediately after signing: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := verifyWithKey(tok, pub, testHwID); err == nil {
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
	tok := signTestToken(t, priv, ProviderDesktopLicense, testHwID, time.Now().Add(time.Hour).Unix())
	parts := strings.Split(tok, ".")

	// Tamper attempt: swap in a different (also legitimate-looking) hw id,
	// as if trying to retarget someone else's token at this machine by
	// hand-editing the payload -- must still be rejected by the signature
	// check before the hw id comparison is even reached.
	claims := Claims{Provider: ProviderDesktopLicense, Sub: "attacker-machine-hwid", Tier: "desktop-license", IssuedAt: time.Now().Unix(), ExpireAt: time.Now().Add(time.Hour).Unix()}
	tamperedPayload, _ := json.Marshal(claims)
	tampered := tokenPrefix + "." + base64.RawURLEncoding.EncodeToString(tamperedPayload) + "." + parts[2]

	if _, err := verifyWithKey(tampered, pub, "attacker-machine-hwid"); err == nil {
		t.Fatal("verifyWithKey accepted a token whose payload was edited after signing (sub swapped) -- should be impossible")
	}
}
