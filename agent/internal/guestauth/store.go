package guestauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Share is one guest's or local scoped token's access, as the agent itself recorded it.
type Share struct {
	ID             string  `json:"id"`
	Label          string  `json:"label,omitempty"`
	Email          string  `json:"email,omitempty"`
	Role           Role    `json:"role"`
	Policy         *Policy `json:"policy,omitempty"`
	Local          bool    `json:"local,omitempty"`
	LocalSecret    string  `json:"local_secret,omitempty"`
	RequireConsent bool    `json:"require_consent"`
	// ExpiresAt is unix seconds; 0 means no end date.
	ExpiresAt  int64 `json:"expires_at,omitempty"`
	CreatedAt  int64 `json:"created_at"`
	RevokedAt  int64 `json:"revoked_at,omitempty"`
	LastSeenAt int64 `json:"last_seen_at,omitempty"`
	// Fingerprints are the Moonlight client certificates (SHA-256 of the DER,
	// hex) this guest paired with. They are removed from the stream host's
	// trust list when the share ends, so a revoked guest cannot stream again.
	Fingerprints []string `json:"fingerprints,omitempty"`
}

// EffectivePolicy returns the granular Policy for this share, falling back to
// PolicyForRole for legacy shares that only recorded a Role.
func (s *Share) EffectivePolicy() Policy {
	if s.Policy != nil {
		return *s.Policy
	}
	return PolicyForRole(s.Role)
}

// DisplayName returns a human-readable identifier (Label if set, else Email, else ID).
func (s *Share) DisplayName() string {
	if l := strings.TrimSpace(s.Label); l != "" {
		return l
	}
	if e := strings.TrimSpace(s.Email); e != "" {
		return e
	}
	return s.ID
}

// LocalToken returns the formatted "usbloc1.<id>.<secret>" token for a local
// share, or "" if this is not a local share.
func (s *Share) LocalToken() string {
	if !s.Local || s.LocalSecret == "" {
		return ""
	}
	return FormatLocalToken(s.ID, s.LocalSecret)
}

// Live reports whether the share still grants access at now.
func (s *Share) Live(now time.Time) bool {
	if s.RevokedAt != 0 {
		return false
	}
	return s.ExpiresAt == 0 || s.ExpiresAt > now.Unix()
}

// Store persists shares in <stateDir>/guests.json.
type Store struct {
	mu     sync.Mutex
	path   string
	shares map[string]*Share
}

// OpenStore loads (or starts) the share list under stateDir.
func OpenStore(stateDir string) (*Store, error) {
	s := &Store{path: filepath.Join(stateDir, "guests.json"), shares: map[string]*Share{}}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f struct {
		Shares []*Share `json:"shares"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	for _, sh := range f.Shares {
		if sh != nil && sh.ID != "" {
			s.shares[sh.ID] = sh
		}
	}
	return s, nil
}

// NewID makes an unguessable share id (128 random bits, URL-safe).
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("guestauth: no randomness: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

var (
	ErrBadShare = errors.New("guestauth: invalid share")
	ErrNoShare  = errors.New("guestauth: no such share")
)

// Add records a new share and returns it.
func (s *Store) Add(email string, role Role, expiresAt int64, requireConsent bool, now time.Time) (*Share, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || len(email) > 254 || !ValidRole(role) {
		return nil, ErrBadShare
	}
	if expiresAt != 0 && expiresAt <= now.Unix() {
		return nil, ErrBadShare
	}
	sh := &Share{ID: NewID(), Email: email, Role: role, RequireConsent: requireConsent, ExpiresAt: expiresAt, CreatedAt: now.Unix()}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shares[sh.ID] = sh
	if err := s.saveLocked(); err != nil {
		delete(s.shares, sh.ID)
		return nil, err
	}
	cp := *sh
	return &cp, nil
}

// AddLocal creates a self-hosted local access token directly on the agent
// (no cloud or email required) with a granular Policy matrix.
func (s *Store) AddLocal(label string, policy Policy, expiresAt int64, requireConsent bool, now time.Time) (*Share, string, error) {
	label = strings.TrimSpace(label)
	if label == "" || len(label) > 120 {
		return nil, "", ErrBadShare
	}
	if expiresAt != 0 && expiresAt <= now.Unix() {
		return nil, "", ErrBadShare
	}
	secret, err := GenerateLocalSecret()
	if err != nil {
		return nil, "", err
	}
	pol := policy
	sh := &Share{
		ID:             NewID(),
		Label:          label,
		Role:           RoleCustom,
		Policy:         &pol,
		Local:          true,
		LocalSecret:    secret,
		RequireConsent: requireConsent,
		ExpiresAt:      expiresAt,
		CreatedAt:      now.Unix(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shares[sh.ID] = sh
	if err := s.saveLocked(); err != nil {
		delete(s.shares, sh.ID)
		return nil, "", err
	}
	cp := *sh
	return &cp, sh.LocalToken(), nil
}

// UpdatePolicy updates an existing share's label, granular Policy, consent
// requirement, and expiry in place.
func (s *Store) UpdatePolicy(id, label string, policy Policy, requireConsent bool, expiresAt int64) (*Share, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.shares[id]
	if !ok {
		return nil, ErrNoShare
	}
	if l := strings.TrimSpace(label); l != "" {
		if len(l) > 120 {
			return nil, ErrBadShare
		}
		sh.Label = l
	}
	pol := policy
	sh.Policy = &pol
	sh.Role = RoleCustom
	sh.RequireConsent = requireConsent
	sh.ExpiresAt = expiresAt
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	cp := *sh
	return &cp, nil
}

// Remove drops a share that never made it to the cloud (rollback of Add).
func (s *Store) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.shares, id)
	_ = s.saveLocked()
}

// Revoke ends a share. Idempotent.
func (s *Store) Revoke(id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.shares[id]
	if !ok {
		return ErrNoShare
	}
	if sh.RevokedAt == 0 {
		sh.RevokedAt = now.Unix()
	}
	return s.saveLocked()
}

// maxFingerprints bounds the certificates one share may pair.
const maxFingerprints = 4

// AddFingerprint records a Moonlight client certificate paired for the share.
func (s *Store) AddFingerprint(id, fp string) error {
	fp = strings.ToLower(strings.TrimSpace(fp))
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.shares[id]
	if !ok {
		return ErrNoShare
	}
	for _, f := range sh.Fingerprints {
		if f == fp {
			return nil
		}
	}
	if len(sh.Fingerprints) >= maxFingerprints {
		return ErrBadShare
	}
	sh.Fingerprints = append(sh.Fingerprints, fp)
	return s.saveLocked()
}

// RemoveFingerprint forgets one certificate (it never finished pairing).
func (s *Store) RemoveFingerprint(id, fp string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.shares[id]
	if !ok {
		return
	}
	out := sh.Fingerprints[:0]
	for _, f := range sh.Fingerprints {
		if f != fp {
			out = append(out, f)
		}
	}
	sh.Fingerprints = out
	_ = s.saveLocked()
}

// TakeFingerprints returns the share's certificates and forgets them.
func (s *Store) TakeFingerprints(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.shares[id]
	if !ok || len(sh.Fingerprints) == 0 {
		return nil
	}
	out := sh.Fingerprints
	sh.Fingerprints = nil
	_ = s.saveLocked()
	return out
}

// Get returns a copy of the share, or nil.
func (s *Store) Get(id string) *Share {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sh, ok := s.shares[id]; ok {
		cp := *sh
		return &cp
	}
	return nil
}

// Touch records that the guest connected.
func (s *Store) Touch(id string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sh, ok := s.shares[id]; ok {
		sh.LastSeenAt = now.Unix()
		_ = s.saveLocked()
	}
}

// List returns every share, newest first. Revoked shares are kept for the audit view.
func (s *Store) List() []Share {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Share, 0, len(s.shares))
	for _, sh := range s.shares {
		out = append(out, *sh)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// LiveCount is how many shares grant access right now.
func (s *Store) LiveCount(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, sh := range s.shares {
		if sh.Live(now) {
			n++
		}
	}
	return n
}

func (s *Store) saveLocked() error {
	list := make([]*Share, 0, len(s.shares))
	for _, sh := range s.shares {
		list = append(list, sh)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt < list[j].CreatedAt })
	raw, err := json.MarshalIndent(struct {
		Shares []*Share `json:"shares"`
	}{list}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
