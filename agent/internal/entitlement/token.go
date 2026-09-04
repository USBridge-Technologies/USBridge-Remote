package entitlement

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Claims mirrors usbridge-entitlement-backend's src/entitlement.ts Claims
// interface field-for-field (JSON key names, not Go field names, are what
// must match). Verify unmarshals into this after the signature checks out
// — never before.
type Claims struct {
	Provider string `json:"provider"`
	Sub      string `json:"sub"`
	Tier     string `json:"tier"`
	IssuedAt int64  `json:"iat"` // unix seconds
	ExpireAt int64  `json:"exp"` // unix seconds
}

// tokenPrefix must match the backend's TOKEN_PREFIX exactly (entitlement.ts).
const tokenPrefix = "usbent1"

// ProviderDesktopLicense and ProviderDesktopTrial must match
// usbridge-entitlement-backend's desktopLicense.ts DESKTOP_LICENSE_PROVIDER/
// DESKTOP_TRIAL_PROVIDER exactly -- see VerifyForHardware's doc comment on
// why a caller must check this, not just the signature.
const (
	ProviderDesktopLicense = "desktop-device"
	ProviderDesktopTrial   = "desktop-trial"
)

// VerifyForHardware checks a desktop-license or trial token's Ed25519
// signature against the public key compiled into this binary, its expiry,
// that its `provider` claim is one of the two desktop token kinds above,
// AND that its `sub` claim matches expectedHwID. This last check is what
// makes these tokens hardware-bound at all: the backend will happily sign
// a token for whatever hw_id a caller sends it (see
// usbridge-entitlement-backend's deviceLicense.ts doc comment on the
// identical SBC design -- there is nothing to gain by requesting a token
// for hardware you don't possess, since only that hardware's own agent
// will ever accept it), so the actual enforcement that a copied token file
// doesn't work on a second machine happens HERE, not server-side. Nothing
// in Claims should be trusted before this returns successfully — mirrors
// internal/update's fetchManifest doing ed25519.Verify before touching a
// single field (see that package's doc comment for the full MITM-defense
// rationale, identical here).
func VerifyForHardware(token, expectedHwID string) (*Claims, error) {
	return verifyWithKey(token, publicKey, expectedHwID)
}

// verifyWithKey is VerifyForHardware's actual implementation, parameterized
// on the public key purely so tests can pass a throwaway keypair instead of
// the real compiled-in one -- that's the only way to test a genuinely
// valid-signature token (e.g. one with a controlled, near-future exp)
// without the real backend's private key, which by design never exists in
// this repo. VerifyForHardware itself is the only production caller,
// always with the real publicKey; this split changes no production
// behavior.
func verifyWithKey(token string, pub ed25519.PublicKey, expectedHwID string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != tokenPrefix {
		return nil, fmt.Errorf("entitlement: malformed token")
	}
	payloadB64, sigB64 := parts[1], parts[2]

	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("entitlement: decode signature: %w", err)
	}
	// The signed message is the base64url PAYLOAD TEXT itself (its ASCII
	// bytes), not the decoded JSON -- must match entitlement.ts's sign()
	// exactly: `crypto.subtle.sign("Ed25519", key, new
	// TextEncoder().encode(payload))` where payload is already
	// base64url-encoded at that point.
	if !ed25519.Verify(pub, []byte(payloadB64), sig) {
		return nil, fmt.Errorf("entitlement: signature verification failed — refusing to trust it")
	}

	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, fmt.Errorf("entitlement: decode payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("entitlement: parse claims: %w", err)
	}
	if claims.Provider != ProviderDesktopLicense && claims.Provider != ProviderDesktopTrial {
		return nil, fmt.Errorf("entitlement: not a desktop license/trial token (provider=%q)", claims.Provider)
	}
	if claims.Sub != expectedHwID {
		return nil, fmt.Errorf("entitlement: token is bound to different hardware")
	}
	if time.Now().Unix() >= claims.ExpireAt {
		return nil, fmt.Errorf("entitlement: token expired")
	}
	return &claims, nil
}
