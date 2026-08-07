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

// Verify checks an entitlement token's Ed25519 signature against the
// public key compiled into this binary, then its expiry. Nothing in
// Claims should be trusted before this returns successfully — mirrors
// internal/update's fetchManifest doing ed25519.Verify before touching a
// single field (see that package's doc comment for the full MITM-defense
// rationale, identical here).
func Verify(token string) (*Claims, error) {
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
	if !ed25519.Verify(publicKey, []byte(payloadB64), sig) {
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
	if time.Now().Unix() >= claims.ExpireAt {
		return nil, fmt.Errorf("entitlement: token expired")
	}
	return &claims, nil
}
