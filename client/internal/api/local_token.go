package api

import "strings"

// LocalTokenPrefix matches the agent's self-hosted scoped access token format:
// "usbloc1.<share_id>.<secret_hex>".
const LocalTokenPrefix = "usbloc1"

// Capability bits mirrored from agent/internal/guestauth/caps.go.
const (
	CapDisplay          uint32 = 1 << 10
	CapDisplayMain      uint32 = 1 << 12
	CapDisplayExclusive uint32 = 1 << 13
)

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

// SetAccessCaps records the capability bitmask reported by the agent.
func (c *USBClient) SetAccessCaps(caps uint32) {
	if c == nil {
		return
	}
	c.accessCaps.Store(caps)
	c.accessCapsSet.Store(true)
}

// HasCap reports whether the current connection holds capability bit.
// For unrestricted master-key connections (or older agents that omit
// access_caps), HasCap returns true.
func (c *USBClient) HasCap(bit uint32) bool {
	if c == nil {
		return true
	}
	if !c.accessCapsSet.Load() {
		// If using a local scoped token before sync completes, fail closed on
		// privileged display-mutation controls until the agent reports caps.
		_, _, isLocal := ParseLocalToken(c.apiSecret)
		return !isLocal
	}
	return (c.accessCaps.Load() & bit) != 0
}

func (c *USBClient) CanManageDisplays() bool       { return c.HasCap(CapDisplay) }
func (c *USBClient) CanSetMainDisplay() bool       { return c.HasCap(CapDisplayMain) }
func (c *USBClient) CanTurnOtherMonitorsOff() bool { return c.HasCap(CapDisplayExclusive) }
