package guestauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Cloud is this agent's side of the backend's share endpoints. Its requests
// are signed with a device key generated on first use and registered with the
// backend trust-on-first-use per hw_id, so nobody else who learns the hw_id
// can e-mail invites in this machine's name.
type Cloud struct {
	BaseURL func() string
	HwID    func() string
	HTTP    *http.Client
	key     ed25519.PrivateKey
}

var (
	// ErrFreeLimit: the Free tier allows one active guest.
	ErrFreeLimit = errors.New("guestauth: free tier allows one guest")
	ErrRateLimit = errors.New("guestauth: too many invitations")
)

// OpenCloud loads or creates the device key under stateDir.
func OpenCloud(stateDir string, baseURL, hwID func() string) (*Cloud, error) {
	path := filepath.Join(stateDir, "guest_device.key")
	var priv ed25519.PrivateKey
	if raw, err := os.ReadFile(path); err == nil {
		seed, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if derr == nil && len(seed) == ed25519.SeedSize {
			priv = ed25519.NewKeyFromSeed(seed)
		}
	}
	if priv == nil {
		_, p, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		priv = p
		if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(p.Seed())), 0o600); err != nil {
			return nil, err
		}
	}
	return &Cloud{BaseURL: baseURL, HwID: hwID, HTTP: &http.Client{Timeout: 15 * time.Second}, key: priv}, nil
}

func sigMessage(method, pathAndQuery, ts string, body []byte) string {
	h := sha256.Sum256(body)
	return method + "\n" + pathAndQuery + "\n" + ts + "\n" + hex.EncodeToString(h[:])
}

func (c *Cloud) do(ctx context.Context, method, pathAndQuery string, body []byte, out any) (int, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL(), "/")+pathAndQuery, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", c.HwID())
	req.Header.Set("X-Device-Ts", ts)
	req.Header.Set("X-Device-Sig", base64.StdEncoding.EncodeToString(ed25519.Sign(c.key, []byte(sigMessage(method, pathAndQuery, ts, body)))))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		switch {
		case resp.StatusCode == http.StatusPaymentRequired && e.Error == "free_limit":
			return resp.StatusCode, ErrFreeLimit
		case resp.StatusCode == http.StatusTooManyRequests:
			return resp.StatusCode, ErrRateLimit
		}
		return resp.StatusCode, fmt.Errorf("guestauth: backend %d: %s", resp.StatusCode, e.Error)
	}
	if out != nil {
		return resp.StatusCode, json.Unmarshal(raw, out)
	}
	return resp.StatusCode, nil
}

// Register tells the backend which key signs this device's share requests.
// Safe to repeat. The registration is self-signed, so it works before the
// backend knows the key.
func (c *Cloud) Register(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{
		"hw_id":  c.HwID(),
		"pubkey": base64.StdEncoding.EncodeToString(c.key.Public().(ed25519.PublicKey)),
	})
	_, err := c.do(ctx, http.MethodPost, "/v1/device/key", body, nil)
	return err
}

// CreateResult is the backend's answer to CreateShare.
type CreateResult struct {
	InviteURL string `json:"invite_url"`
	MailSent  bool   `json:"mail_sent"`
}

// CreateShare asks the backend to record sh and e-mail the invite.
func (c *Cloud) CreateShare(ctx context.Context, sh *Share, deviceName, network string) (*CreateResult, error) {
	req := map[string]any{
		"id": sh.ID, "guest_email": sh.Email, "role": sh.Role,
		"require_consent": sh.RequireConsent, "device_name": deviceName, "network": network,
	}
	if sh.ExpiresAt != 0 {
		req["share_exp"] = sh.ExpiresAt
	}
	body, _ := json.Marshal(req)
	var out CreateResult
	if _, err := c.do(ctx, http.MethodPost, "/v1/device/shares", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetShareHost tells the backend where guests reach this machine for id.
func (c *Cloud) SetShareHost(ctx context.Context, id, host string) error {
	body, _ := json.Marshal(map[string]string{"host": host})
	_, err := c.do(ctx, http.MethodPost, "/v1/device/shares/"+id+"/host", body, nil)
	return err
}

// TailnetKey is what this machine's guest node joins the guest network with.
type TailnetKey struct {
	ControlURL string `json:"control_url"`
	AuthKey    string `json:"auth_key"`
	Hostname   string `json:"hostname"`
}

// TailnetKey asks for a one-use key for the guest node (needs a live share).
func (c *Cloud) TailnetKey(ctx context.Context) (*TailnetKey, error) {
	var out TailnetKey
	if _, err := c.do(ctx, http.MethodPost, "/v1/device/tailnet-key", []byte("{}"), &out); err != nil {
		return nil, err
	}
	if out.ControlURL == "" || out.AuthKey == "" {
		return nil, fmt.Errorf("guestauth: backend sent no tailnet key")
	}
	return &out, nil
}

// RevokeShare tells the backend to stop issuing tickets for id.
func (c *Cloud) RevokeShare(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/v1/device/shares/"+id, nil, nil)
	return err
}

// RemoteShare is the cloud's view of a share (when the guest accepted, last sign-in).
type RemoteShare struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	AcceptedAt int64  `json:"accepted_at"`
	LastSeen   int64  `json:"last_seen"`
}

// ListShares fetches the cloud's view of this device's shares.
func (c *Cloud) ListShares(ctx context.Context) ([]RemoteShare, error) {
	var out struct {
		Shares []RemoteShare `json:"shares"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/v1/device/shares", nil, &out); err != nil {
		return nil, err
	}
	return out.Shares, nil
}

// AuditEvent is one guest-session event reported for the Pro/Enterprise trail.
type AuditEvent struct {
	ShareID string `json:"share_id"`
	Event   string `json:"event"`
	TS      int64  `json:"ts"`
	Detail  string `json:"detail,omitempty"`
}

// PostAudit uploads session events (the backend stores them for paid tiers only).
func (c *Cloud) PostAudit(ctx context.Context, events []AuditEvent) error {
	body, _ := json.Marshal(map[string]any{"events": events})
	_, err := c.do(ctx, http.MethodPost, "/v1/device/shares/audit", body, nil)
	return err
}
