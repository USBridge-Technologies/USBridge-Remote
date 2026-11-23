package guestauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// LocalTokenPrefix tags self-hosted local access tokens issued directly by the
// agent (no cloud or email required). Format: "usbloc1.<share_id>.<secret_hex>".
const LocalTokenPrefix = "usbloc1"

// GenerateLocalSecret generates a 32-byte cryptographically random secret
// encoded as 64 lowercase hex characters.
func GenerateLocalSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// FormatLocalToken packs shareID and secret into the transferable token string
// embedded in QR codes and usbridge://connect links.
func FormatLocalToken(shareID, secret string) string {
	return LocalTokenPrefix + "." + shareID + "." + secret
}

// ParseLocalToken splits a "usbloc1.<share_id>.<secret>" token string.
func ParseLocalToken(raw string) (shareID, secret string, ok bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, LocalTokenPrefix+".") {
		return "", "", false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != LocalTokenPrefix || parts[1] == "" || len(parts[2]) < 32 {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// CalculateLocalHMAC computes the HMAC-SHA256 signature for a local access
// token request using the same SHA-256 key derivation and message layout as
// the agent's master-key HMAC: METHOD + REQUEST_URI + TIMESTAMP + BODY.
func CalculateLocalHMAC(method, requestURI, timestamp string, body []byte, secret string) string {
	key := sha256.Sum256([]byte(secret))
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(method))
	mac.Write([]byte(requestURI))
	mac.Write([]byte(timestamp))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
