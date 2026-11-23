package api

import "strings"

// LocalTokenPrefix matches the agent's self-hosted scoped access token format:
// "usbloc1.<share_id>.<secret_hex>".
const LocalTokenPrefix = "usbloc1"

// ParseLocalToken splits a scoped local token into its share ID and raw HMAC
// secret bytes. If raw is a regular master key, isLocal is false and secret is raw.
func ParseLocalToken(raw []byte) (tokenID string, secret []byte, isLocal bool) {
	s := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(s, LocalTokenPrefix+".") {
		return "", raw, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 || parts[0] != LocalTokenPrefix || parts[1] == "" || len(parts[2]) < 32 {
		return "", raw, false
	}
	return parts[1], []byte(parts[2]), true
}
