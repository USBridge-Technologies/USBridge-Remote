package app

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/guestauth"
	"usbridge_agent/internal/hwid"
)

// initGuestAccess builds the guest-access manager (see internal/guestauth)
// and hands it to the API server. A failure only leaves guest access off --
// the owner's master-key access is untouched.
func (a *App) initGuestAccess(apiServer *api.Server) {
	store, err := guestauth.OpenStore(a.cfg.StateDir)
	if err != nil {
		log.Printf("[app] guest access disabled: %v", err)
		return
	}
	hwID := func() string {
		id, _ := hwid.Get()
		return id
	}
	mgr := &guestauth.Manager{
		Store: store,
		Auth:  guestauth.NewAuthenticator(store, entitlement.PublicKey(), hwID),
		DeviceName: func() string {
			h, _ := os.Hostname()
			return strings.TrimSpace(h)
		},
		Paid: a.paidTier,
	}
	if cloud, err := guestauth.OpenCloud(a.cfg.StateDir, entitlement.BackendBaseURL, hwID); err != nil {
		log.Printf("[app] guest access: no cloud (invitations unavailable): %v", err)
	} else {
		mgr.Cloud = cloud
	}
	mgr.Sessions = guestauth.NewSessions(
		func(s guestauth.Session) { a.guestConsent(s) },
		func(ev guestauth.Event) {
			log.Printf("[guest] %s share=%s email=%s %s", ev.Kind, ev.ShareID, ev.Email, ev.Detail)
			mgr.Audit(guestauth.AuditEvent{ShareID: ev.ShareID, Event: ev.Kind, Detail: ev.Email + " " + ev.Detail})
		},
		func(shareID string) { a.guestSessionEnded(shareID) },
	)
	a.guests = mgr
	apiServer.SetGuestManager(mgr)
	relayOfferAuthorizer = apiServer.AuthorizeOffer
}

// paidTier reports whether this machine's license is Pro or Enterprise.
func (a *App) paidTier() bool {
	id, err := hwid.Get()
	if err != nil {
		return false
	}
	claims, err := entitlement.VerifyForHardware(a.cfg.EntitlementToken, id)
	return err == nil && (claims.Tier == "pro" || claims.Tier == "enterprise")
}

// guestsWatchdog ends sessions whose share expired or was revoked, and
// uploads the audit trail.
func (a *App) guestsWatchdog(ctx context.Context) {
	if a.guests == nil {
		return
	}
	sweep := time.NewTicker(15 * time.Second)
	flush := time.NewTicker(2 * time.Minute)
	defer sweep.Stop()
	defer flush.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sweep.C:
			a.guests.Sweep()
		case <-flush.C:
			fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			a.guests.FlushAudit(fctx)
			cancel()
		}
	}
}

// guestConsent is called when a guest wants to start a session that needs the
// person at this machine to say yes. The host UI (and the owner's client,
// through POST /api/guests/{id}/decide) answers via guests.Sessions.Decide;
// until one does, the request lapses to a denial after guestauth.PendingTimeout.
func (a *App) guestConsent(s guestauth.Session) {
	log.Printf("[guest] %s (%s) is asking to connect", s.Email, s.Role)
}

// guestSessionEnded is called whenever a guest stops being allowed (kicked,
// revoked, expired, denied).
func (a *App) guestSessionEnded(shareID string) {}

// GuestPending lists guests waiting for the person at this machine to allow
// them in (ui.TokenProvider; the window asks about each one).
func (a *App) GuestPending() []guestauth.Session {
	if a.guests == nil {
		return nil
	}
	var out []guestauth.Session
	for _, s := range a.guests.Sessions.List() {
		if s.State == guestauth.StatePending {
			out = append(out, s)
		}
	}
	return out
}

// GuestDecide records the host's answer to a pending guest.
func (a *App) GuestDecide(shareID string, allow bool) error {
	if a.guests == nil || !a.guests.Sessions.Decide(shareID, allow) {
		return errors.New("no guest is waiting for an answer")
	}
	return nil
}
