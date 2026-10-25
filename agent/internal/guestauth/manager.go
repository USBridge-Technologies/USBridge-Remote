package guestauth

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
)

// Manager ties the local share list, request verification, live sessions and
// the cloud together. It is what the API and the host UI talk to.
type Manager struct {
	Store    *Store
	Auth     *Authenticator
	Sessions *Sessions
	Cloud    *Cloud

	// DeviceName is how this machine is called in the invite e-mail.
	DeviceName func() string
	// Paid reports whether the license is Pro/Enterprise (Free: one guest).
	Paid func() bool

	regOnce sync.Once
	regErr  error

	auditMu sync.Mutex
	pending []AuditEvent
}

// FreeMaxShares is the Free tier's limit on simultaneously live shares.
const FreeMaxShares = 1

// ErrCloudUnavailable: an invitation needs the cloud (it sends the e-mail).
var ErrCloudUnavailable = errors.New("guestauth: cannot reach the USBridge cloud")

// Invite creates a share and has the cloud e-mail it. The local record is
// removed again if the cloud refuses, so nothing is left half-created.
func (m *Manager) Invite(ctx context.Context, email string, role Role, expiresAt int64, requireConsent bool) (*Share, *CreateResult, error) {
	now := time.Now()
	if (m.Paid == nil || !m.Paid()) && m.Store.LiveCount(now) >= FreeMaxShares {
		return nil, nil, ErrFreeLimit
	}
	sh, err := m.Store.Add(email, role, expiresAt, requireConsent, now)
	if err != nil {
		return nil, nil, err
	}
	if m.Cloud == nil {
		m.Store.Remove(sh.ID)
		return nil, nil, ErrCloudUnavailable
	}
	m.regOnce.Do(func() { m.regErr = m.Cloud.Register(ctx) })
	if m.regErr != nil {
		err := m.regErr
		m.regOnce = sync.Once{} // try again next time
		m.Store.Remove(sh.ID)
		return nil, nil, wrapCloud(err)
	}
	name := ""
	if m.DeviceName != nil {
		name = m.DeviceName()
	}
	res, err := m.Cloud.CreateShare(ctx, sh, name)
	if err != nil {
		m.Store.Remove(sh.ID)
		return nil, nil, wrapCloud(err)
	}
	m.Audit(AuditEvent{ShareID: sh.ID, Event: "created", TS: now.Unix(), Detail: sh.Email + " " + string(sh.Role)})
	return sh, res, nil
}

func wrapCloud(err error) error {
	if errors.Is(err, ErrFreeLimit) || errors.Is(err, ErrRateLimit) {
		return err
	}
	return errors.Join(ErrCloudUnavailable, err)
}

// Revoke ends a share for good: the local record decides access, so this
// takes effect at once even when the cloud is unreachable.
func (m *Manager) Revoke(ctx context.Context, id string) error {
	if err := m.Store.Revoke(id, time.Now()); err != nil {
		return err
	}
	m.Sessions.End(id, "revoked")
	if m.Cloud != nil {
		if err := m.Cloud.RevokeShare(ctx, id); err != nil {
			// Harmless for security (the agent refuses the share regardless);
			// the cloud merely keeps handing the guest tickets that fail here.
			log.Printf("[guest] revoke %s: cloud not told: %v", id, err)
		}
	}
	return nil
}

// Audit queues an event for the cloud's trail (kept for Pro/Enterprise).
func (m *Manager) Audit(ev AuditEvent) {
	if ev.TS == 0 {
		ev.TS = time.Now().Unix()
	}
	ev.Detail = strings.TrimSpace(ev.Detail)
	m.auditMu.Lock()
	if len(m.pending) < 500 {
		m.pending = append(m.pending, ev)
	}
	m.auditMu.Unlock()
}

// FlushAudit uploads queued events; failed uploads stay queued.
func (m *Manager) FlushAudit(ctx context.Context) {
	if m.Cloud == nil || m.Paid == nil || !m.Paid() {
		m.auditMu.Lock()
		m.pending = nil
		m.auditMu.Unlock()
		return
	}
	m.auditMu.Lock()
	batch := m.pending
	m.pending = nil
	m.auditMu.Unlock()
	if len(batch) == 0 {
		return
	}
	if err := m.Cloud.PostAudit(ctx, batch); err != nil {
		m.auditMu.Lock()
		m.pending = append(batch, m.pending...)
		m.auditMu.Unlock()
	}
}

// Sweep ends sessions whose share has expired or been revoked; call it periodically.
func (m *Manager) Sweep() {
	now := time.Now()
	for _, sess := range m.Sessions.List() {
		sh := m.Store.Get(sess.ShareID)
		if sh == nil || !sh.Live(now) {
			m.Sessions.End(sess.ShareID, "expired")
		}
	}
}
