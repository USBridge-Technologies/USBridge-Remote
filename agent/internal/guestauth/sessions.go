package guestauth

import (
	"sort"
	"sync"
	"time"
)

// State is where a guest's session stands with the person at the machine.
type State string

const (
	StatePending State = "pending" // waiting for the host to allow it
	StateAllowed State = "allowed"
	StateDenied  State = "denied"
)

// IdleTimeout ends an allowed session that has made no request for this
// long, so a guest who walked away needs the host's OK again.
const IdleTimeout = 10 * time.Minute

// PendingTimeout turns an unanswered consent prompt into a denial.
const PendingTimeout = 90 * time.Second

// Session is one guest's live access.
type Session struct {
	ShareID    string    `json:"share_id"`
	Email      string    `json:"email"`
	Role       Role      `json:"role"`
	State      State     `json:"state"`
	StartedAt  time.Time `json:"started_at"`
	LastActive time.Time `json:"last_active"`
}

// Event is reported to the audit trail and the host UI.
type Event struct {
	ShareID string
	Email   string
	Kind    string // "requested", "allowed", "denied", "ended", "kicked", "revoked"
	Detail  string
}

// Sessions tracks consent and liveness of guest sessions in memory. A
// restart of the agent ends them all, which is the safe direction.
type Sessions struct {
	mu   sync.Mutex
	m    map[string]*Session
	now  func() time.Time
	ask  func(Session)
	emit func(Event)
	end  func(shareID string)
}

// NewSessions makes an empty tracker. ask is called (outside the lock) when a
// session needs the host's decision; emit receives lifecycle events; end is
// called whenever a session stops being allowed, so open streams can be cut.
func NewSessions(ask func(Session), emit func(Event), end func(shareID string)) *Sessions {
	return &Sessions{m: map[string]*Session{}, now: time.Now, ask: ask, emit: emit, end: end}
}

func (s *Sessions) fire(ev Event) {
	if s.emit != nil {
		s.emit(ev)
	}
}

// Hello is a guest asking to start (or continue) a session. When the share
// doesn't require consent it is allowed at once.
func (s *Sessions) Hello(p *Principal) State {
	now := s.now()
	s.mu.Lock()
	cur := s.m[p.ShareID]
	if cur != nil && s.expiredLocked(cur, now) {
		delete(s.m, p.ShareID)
		cur = nil
	}
	if cur != nil && cur.State != StateDenied {
		cur.LastActive = now
		st := cur.State
		s.mu.Unlock()
		return st
	}
	sess := &Session{ShareID: p.ShareID, Email: p.Email, Role: p.Role, StartedAt: now, LastActive: now, State: StatePending}
	if !p.RequireConsent {
		sess.State = StateAllowed
	}
	s.m[p.ShareID] = sess
	cp := *sess
	s.mu.Unlock()

	if cp.State == StateAllowed {
		s.fire(Event{ShareID: cp.ShareID, Email: cp.Email, Kind: "allowed", Detail: "no consent required"})
		return StateAllowed
	}
	s.fire(Event{ShareID: cp.ShareID, Email: cp.Email, Kind: "requested"})
	if s.ask != nil {
		s.ask(cp)
	}
	return StatePending
}

// Allowed reports whether the guest may use the machine right now, and counts
// the call as activity. A request with no session at all is not allowed.
func (s *Sessions) Allowed(p *Principal) bool {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.m[p.ShareID]
	if cur == nil || cur.State != StateAllowed || s.expiredLocked(cur, now) {
		return false
	}
	cur.LastActive = now
	return true
}

func (s *Sessions) expiredLocked(cur *Session, now time.Time) bool {
	switch cur.State {
	case StateAllowed:
		return now.Sub(cur.LastActive) > IdleTimeout
	case StatePending:
		return now.Sub(cur.StartedAt) > PendingTimeout
	}
	return false
}

// Decide records the host's answer to a pending prompt.
func (s *Sessions) Decide(shareID string, allow bool) bool {
	s.mu.Lock()
	cur := s.m[shareID]
	if cur == nil || cur.State != StatePending {
		s.mu.Unlock()
		return false
	}
	if allow {
		cur.State = StateAllowed
		cur.LastActive = s.now()
	} else {
		cur.State = StateDenied
	}
	cp := *cur
	s.mu.Unlock()
	kind := "denied"
	if allow {
		kind = "allowed"
	}
	s.fire(Event{ShareID: cp.ShareID, Email: cp.Email, Kind: kind, Detail: "host"})
	if !allow && s.end != nil {
		s.end(shareID)
	}
	return true
}

// End stops a guest's session (host kick, revoke, share expiry).
func (s *Sessions) End(shareID, kind string) {
	s.mu.Lock()
	cur := s.m[shareID]
	delete(s.m, shareID)
	s.mu.Unlock()
	if cur != nil {
		s.fire(Event{ShareID: cur.ShareID, Email: cur.Email, Kind: kind})
	}
	// Even without a record: a stream can outlive its idle-expired entry, and
	// ending a share that has none running is harmless.
	if s.end != nil {
		s.end(shareID)
	}
}

// List returns the current sessions, oldest first.
func (s *Sessions) List() []Session {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Session, 0, len(s.m))
	for id, cur := range s.m {
		if s.expiredLocked(cur, now) {
			delete(s.m, id)
			continue
		}
		out = append(out, *cur)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}
