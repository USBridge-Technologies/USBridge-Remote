// Package guestclient is the guest's side of shared access to an agent: the
// person was invited by e-mail, signs in with that address (the same account
// login the app already has), and connects with a role instead of the master
// key.
//
// Join generates a P-256 key that never leaves this process, asks the backend
// for a ticket bound to that key (the backend only issues it to the invited
// address), and Headers then signs each request with the key and attaches the
// ticket. The agent checks both -- see agent/internal/guestauth -- so a
// ticket on its own is worthless. Tickets are short-lived; Headers fetches a
// fresh one before the old one runs out.
//
// Builds natively and for the browser (wasm): net/http is fetch there.
package guestclient

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultBackend is the entitlement backend (the same address internal/account uses).
var DefaultBackend = "https://usbridge-entitlement.fatkulinamir80.workers.dev"

// renewBefore is how early a ticket is replaced.
const renewBefore = 2 * time.Minute

// Errors the UI tells apart.
var (
	ErrNotInvited  = errors.New("this account is not the one that was invited")
	ErrShareEnded  = errors.New("this access has ended")
	ErrNeedSignIn  = errors.New("sign in first")
	ErrRateLimited = errors.New("too many attempts, try again later")
)

// Tailnet is what a guest's app needs to join the guest network: the control
// server, a one-use key and the name to register under (the agent only lets
// nodes with a live share's name through).
type Tailnet struct {
	ControlURL string `json:"control_url"`
	AuthKey    string `json:"auth_key"`
	Hostname   string `json:"hostname"`
}

// Session is one guest's way into one agent.
type Session struct {
	ShareID    string
	HwID       string
	DeviceName string
	Role       string
	// Host is the computer's address on the tailnet, as the agent reported it
	// when the invitation was made ("" if it was not on a tailnet then).
	Host string
	// Network is "usbridge" (the guest network, join it with Tailnet) or
	// "tailscale" (the guest's own Tailscale must see the computer).
	Network string
	// Tailnet is how this app joins the network the computer is on (nil when
	// the cloud has none set up).
	Tailnet *Tailnet
	// RequireConsent: the person at the computer will be asked to allow the connection.
	RequireConsent bool

	backend string
	bearer  func() string
	http    *http.Client
	key     *ecdsa.PrivateKey

	mu        sync.Mutex
	ticket    string
	ticketExp time.Time
	renewMu   sync.Mutex
	clockSkew time.Duration
}

// Join signs the guest in to the share and returns a ready session. bearer
// returns the current account token (it is called again on renewals).
func Join(ctx context.Context, backend string, bearer func() string, shareID string) (*Session, error) {
	if backend == "" {
		backend = DefaultBackend
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	s := &Session{ShareID: shareID, backend: strings.TrimRight(backend, "/"), bearer: bearer, http: &http.Client{Timeout: 15 * time.Second}, key: key}
	if err := s.renew(ctx, true); err != nil {
		return nil, err
	}
	return s, nil
}

// PublicKey is the guest key as the backend wants it: the 65-byte
// uncompressed point, base64 (what WebCrypto calls "raw").
func (s *Session) PublicKey() string {
	return base64.StdEncoding.EncodeToString(rawPublicKey(&s.key.PublicKey))
}

func rawPublicKey(k *ecdsa.PublicKey) []byte {
	out := make([]byte, 65)
	out[0] = 4
	k.X.FillBytes(out[1:33])
	k.Y.FillBytes(out[33:])
	return out
}

func (s *Session) renew(ctx context.Context, first bool) error {
	s.renewMu.Lock()
	defer s.renewMu.Unlock()
	if !first {
		s.mu.Lock()
		fresh := time.Until(s.ticketExp) > renewBefore
		s.mu.Unlock()
		if fresh {
			return nil
		}
	}
	token := ""
	if s.bearer != nil {
		token = s.bearer()
	}
	if token == "" {
		return ErrNeedSignIn
	}
	body, _ := json.Marshal(map[string]string{"guest_pubkey": s.PublicKey()})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.backend+"/v1/shares/"+url.PathEscape(s.ShareID)+"/ticket", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return ErrNeedSignIn
	case http.StatusNotFound:
		return ErrNotInvited
	case http.StatusGone:
		return ErrShareEnded
	case http.StatusTooManyRequests:
		return ErrRateLimited
	default:
		return fmt.Errorf("guest ticket: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Ticket         string   `json:"ticket"`
		ExpiresIn      int      `json:"expires_in"`
		HwID           string   `json:"hw_id"`
		DeviceName     string   `json:"device_name"`
		Role           string   `json:"role"`
		RequireConsent bool     `json:"require_consent"`
		Host           string   `json:"host"`
		Tailnet        *Tailnet `json:"tailnet"`
		Network        string   `json:"network"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Ticket == "" {
		return fmt.Errorf("guest ticket: bad answer")
	}
	s.mu.Lock()
	s.ticket, s.ticketExp = out.Ticket, time.Now().Add(time.Duration(out.ExpiresIn)*time.Second)
	s.HwID, s.DeviceName, s.Role, s.RequireConsent = out.HwID, out.DeviceName, out.Role, out.RequireConsent
	s.Host, s.Tailnet, s.Network = out.Host, out.Tailnet, out.Network
	s.mu.Unlock()
	return nil
}

// Headers returns the headers that authenticate one request to the agent.
// uri is the path plus query exactly as sent; signedBody is the request body
// (for a WebRTC offer, the SDP -- see agent api.offerSignedBody).
func (s *Session) Headers(ctx context.Context, method, uri string, signedBody []byte) (map[string]string, error) {
	if err := s.renew(ctx, false); err != nil {
		return nil, err
	}
	s.mu.Lock()
	ticket := s.ticket
	s.mu.Unlock()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	digest := sha256.Sum256([]byte(method + uri + ts + string(signedBody)))
	r, sg, err := ecdsa.Sign(rand.Reader, s.key, digest[:])
	if err != nil {
		return nil, err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sg.FillBytes(sig[32:])
	return map[string]string{
		"X-Guest-Ticket":   ticket,
		"X-Auth-Timestamp": ts,
		"X-Auth-Signature": base64.StdEncoding.EncodeToString(sig),
	}, nil
}
