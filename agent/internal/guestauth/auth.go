package guestauth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TicketPrefix tags backend-signed guest tickets; the signature covers the
// prefix too, so a ticket can never be passed off as an entitlement token.
const TicketPrefix = "usbgst1"

// Skew is how far a request timestamp may be from our clock -- the same
// window the master-key HMAC uses.
const Skew = 60 * time.Second

// Claims is the signed ticket body (usbridge-entitlement-backend src/shares.ts TicketClaims).
type Claims struct {
	V    int    `json:"v"`
	SID  string `json:"sid"`
	HW   string `json:"hw"`
	Em   string `json:"em"`
	Role Role   `json:"role"`
	RC   bool   `json:"rc"`
	SExp int64  `json:"sexp"`
	GPK  string `json:"gpk"`
	N    string `json:"n"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

// Principal is who a verified guest request came from.
type Principal struct {
	ShareID        string
	Email          string
	Role           Role
	RequireConsent bool
}

var (
	ErrNoTicket = errors.New("guestauth: no guest ticket")
	ErrInvalid  = errors.New("guestauth: invalid guest credentials")
	ErrRevoked  = errors.New("guestauth: share ended")
	ErrReplay   = errors.New("guestauth: replayed request")
)

// Authenticator verifies guest requests against the local Store.
type Authenticator struct {
	store *Store
	pub   ed25519.PublicKey
	hwID  func() string
	now   func() time.Time

	mu   sync.Mutex
	seen map[string]time.Time // request signature -> when first seen
}

// NewAuthenticator verifies tickets against pub (the backend's key) for the
// machine hwID() names.
func NewAuthenticator(store *Store, pub ed25519.PublicKey, hwID func() string) *Authenticator {
	return &Authenticator{store: store, pub: pub, hwID: hwID, now: time.Now, seen: map[string]time.Time{}}
}

// VerifyTicket checks the backend signature and the ticket's own validity
// window, and that it is for this machine. It does not look at the Store.
func (a *Authenticator) VerifyTicket(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != TicketPrefix {
		return nil, ErrInvalid
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !ed25519.Verify(a.pub, []byte(TicketPrefix+"."+parts[1]), sig) {
		return nil, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalid
	}
	var c Claims
	if json.Unmarshal(raw, &c) != nil || c.V != 1 {
		return nil, ErrInvalid
	}
	now := a.now()
	if c.Exp <= now.Unix() || c.Iat > now.Add(Skew).Unix() || c.HW != a.hwID() {
		return nil, ErrInvalid
	}
	return &c, nil
}

// Verify authenticates one request: ticket, the share still being live in the
// local Store, and the guest's signature over method+uri+ts+body.
func (a *Authenticator) Verify(method, requestURI, ts, sigB64, ticket string, body []byte) (*Principal, error) {
	if ticket == "" {
		return nil, ErrNoTicket
	}
	c, err := a.VerifyTicket(ticket)
	if err != nil {
		return nil, err
	}
	now := a.now()
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || absDur(now.Sub(time.Unix(unix, 0))) > Skew {
		return nil, ErrInvalid
	}
	pub, err := ParseGuestKey(c.GPK)
	if err != nil {
		return nil, ErrInvalid
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil || len(sig) != 64 {
		return nil, ErrInvalid
	}
	digest := sha256.Sum256([]byte(method + requestURI + ts + string(body)))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, digest[:], r, s) {
		return nil, ErrInvalid
	}

	// The local record is the authority: the ticket only proves the guest
	// controls the invited address. A share this agent doesn't know, or that
	// was revoked/expired here, gets nothing -- whatever the cloud says.
	sh := a.store.Get(c.SID)
	if sh == nil || !strings.EqualFold(sh.Email, c.Em) {
		return nil, ErrInvalid
	}
	if !sh.Live(now) {
		return nil, ErrRevoked
	}
	if a.replayed(sigB64, now) {
		return nil, ErrReplay
	}
	return &Principal{ShareID: sh.ID, Email: sh.Email, Role: sh.Role, RequireConsent: sh.RequireConsent}, nil
}

func (a *Authenticator) replayed(sig string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, t := range a.seen {
		if now.Sub(t) > 2*Skew {
			delete(a.seen, k)
		}
	}
	if _, dup := a.seen[sig]; dup {
		return true
	}
	a.seen[sig] = now
	return false
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// ParseGuestKey reads a P-256 public key given as base64 of the 65-byte
// uncompressed point (WebCrypto "raw") or of an SPKI document.
func ParseGuestKey(b64 string) (*ecdsa.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	if len(raw) == 65 && raw[0] == 4 {
		x, y := new(big.Int).SetBytes(raw[1:33]), new(big.Int).SetBytes(raw[33:])
		if !elliptic.P256().IsOnCurve(x, y) {
			return nil, ErrInvalid
		}
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
	}
	k, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, err
	}
	ek, ok := k.(*ecdsa.PublicKey)
	if !ok || ek.Curve != elliptic.P256() {
		return nil, ErrInvalid
	}
	return ek, nil
}
