package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Guest access on an agent (agent/internal/api/guest.go). All of these are
// owner calls -- signed with the master key -- and only a software agent
// answers them; a KVM or an older agent answers 404.

// Guest is one share as the agent reports it.
type Guest struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Role           string `json:"role"`
	RequireConsent bool   `json:"require_consent"`
	ExpiresAt      int64  `json:"expires_at"`
	CreatedAt      int64  `json:"created_at"`
	RevokedAt      int64  `json:"revoked_at"`
	LastSeenAt     int64  `json:"last_seen_at"`
	Live           bool   `json:"live"`
	// Session is "pending" while the guest waits for the host's OK, "allowed"
	// while connected, "" otherwise.
	Session string `json:"session"`
	// Status is the cloud's view: "invited" until the guest signs in, then "accepted".
	Status     string `json:"status"`
	AcceptedAt int64  `json:"accepted_at"`
}

// GuestList is GET /api/guests.
type GuestList struct {
	Guests    []Guest `json:"guests"`
	Live      int     `json:"live"`
	FreeLimit int     `json:"free_limit"`
	Paid      bool    `json:"paid"`
}

// GuestInvite is what the owner chooses when sharing.
type GuestInvite struct {
	Email string `json:"email"`
	Role  string `json:"role"` // viewer | operator | technician
	// ExpiresIn is seconds from now; 0 = no end date.
	ExpiresIn      int64 `json:"expires_in"`
	RequireConsent bool  `json:"require_consent"`
}

// Errors the owner UI tells apart.
var (
	ErrGuestFreeLimit   = errors.New("the free plan allows one guest")
	ErrGuestCloudDown   = errors.New("cannot reach the USBridge cloud to send the invitation")
	ErrGuestRateLimit   = errors.New("too many invitations, try again later")
	ErrGuestUnsupported = errors.New("this agent does not support guest access")
)

func (c *USBClient) guestCall(method, endpoint string, body any, out any) error {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	resp, err := c.makeRequest(method, endpoint, raw)
	if err != nil {
		return mapGuestError(err)
	}
	var env struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Error   string          `json:"error"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resp, &env); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}
	if !env.Success {
		return guestCodeError(env.Message, fmt.Errorf("%s", env.Message))
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

// ListGuests returns the agent's shares and whether the plan is paid.
func (c *USBClient) ListGuests() (*GuestList, error) {
	var out GuestList
	if err := c.guestCall("GET", "/api/guests", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// InviteGuest shares the agent with an e-mail address; the invitation is mailed by the cloud.
func (c *USBClient) InviteGuest(inv GuestInvite) (inviteURL string, mailSent bool, err error) {
	var out struct {
		InviteURL string `json:"invite_url"`
		MailSent  bool   `json:"mail_sent"`
	}
	if err = c.guestCall("POST", "/api/guests", inv, &out); err != nil {
		return "", false, err
	}
	return out.InviteURL, out.MailSent, nil
}

// RevokeGuest ends a share for good.
func (c *USBClient) RevokeGuest(id string) error {
	return c.guestCall("DELETE", "/api/guests/"+url.PathEscape(id), nil, nil)
}

// EndGuestSession disconnects a guest now but keeps the share.
func (c *USBClient) EndGuestSession(id string) error {
	return c.guestCall("POST", "/api/guests/"+url.PathEscape(id)+"/end", nil, nil)
}

// DecideGuest answers a guest's pending request to connect.
func (c *USBClient) DecideGuest(id string, allow bool) error {
	return c.guestCall("POST", "/api/guests/"+url.PathEscape(id)+"/decide", map[string]bool{"allow": allow}, nil)
}

// mapGuestError turns makeRequest's "HTTP error 402: {json}" into a typed
// error: the agent answers its refusals with a non-200 status and the code in
// the JSON body's message.
func mapGuestError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "HTTP error 404") || strings.Contains(msg, "HTTP error 501") {
		return ErrGuestUnsupported
	}
	if i := strings.Index(msg, "{"); i >= 0 {
		var env struct {
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(msg[i:]), &env) == nil && env.Message != "" {
			return guestCodeError(env.Message, err)
		}
	}
	return err
}

func guestCodeError(code string, fallback error) error {
	switch code {
	case "free_limit":
		return ErrGuestFreeLimit
	case "cloud_unavailable":
		return ErrGuestCloudDown
	case "rate_limit":
		return ErrGuestRateLimit
	case "guest_access_off":
		return ErrGuestUnsupported
	}
	return fallback
}
