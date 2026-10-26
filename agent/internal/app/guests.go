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
	"usbridge_agent/internal/streamhost"
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
	mgr.OwnTailscale = func() bool { return a.cfg.GuestOwnTailscale }
	mgr.Tailnet = func(ctx context.Context) (string, error) {
		if a.cfg.GuestOwnTailscale {
			return a.ownTailnetAddress(ctx)
		}
		return a.ensureGuestNode(ctx)
	}
	mgr.Unpair = func(fp string) error { return a.UnpairSunshineClient(fp) }
	a.guests = mgr
	a.guestKick = apiServer.KickGuest
	apiServer.SetGuestManager(mgr)
	relayOfferAuthorizer = apiServer.AuthorizeOffer
}

// PairGuestClient relays a guest's pairing PIN to the stream host and binds
// the certificate that then pairs (fingerprint) to the share. The binding is
// recorded first and the share is checked again once the host trusts the
// certificate, so a share revoked mid-pairing leaves nothing behind.
func (a *App) PairGuestClient(shareID, fingerprint, pin string) error {
	if a.guests == nil {
		return errors.New("guest access is off")
	}
	sh := a.guests.Store.Get(shareID)
	if sh == nil {
		return errors.New("unknown share")
	}
	perm, limited := guestMoonlightPerm(sh.Role)
	if a.guestPermissioner() == nil {
		if sh.Role == guestauth.RoleViewer {
			// This stream host trusts a paired client fully: it cannot be told to
			// keep a Viewer from the keyboard, so none is paired.
			return errors.New("this stream host cannot limit a guest's input; use the browser")
		}
		limited = false // Operator / Technician get what such a host gives every client
	}
	if err := a.guests.Store.AddFingerprint(shareID, fingerprint); err != nil {
		return err
	}
	if err := a.SubmitMoonlightPIN(pin); err != nil {
		return err
	}
	go func() {
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			if a.guestCertTrusted(fingerprint) {
				if sh := a.guests.Store.Get(shareID); sh == nil || !sh.Live(time.Now()) {
					if err := a.UnpairSunshineClient(fingerprint); err != nil {
						log.Printf("[guest] unpair late certificate: %v", err)
					}
				} else if limited {
					if err := a.limitGuestClient(fingerprint, perm); err != nil {
						// Fail closed: a client that could not be limited is not kept.
						log.Printf("[guest] limit moonlight client of share %s: %v", shareID, err)
						if uerr := a.UnpairSunshineClient(fingerprint); uerr != nil {
							log.Printf("[guest] unpair unlimited client: %v", uerr)
						}
					}
				}
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		// Never paired: forget the binding.
		a.guests.Store.RemoveFingerprint(shareID, fingerprint)
	}()
	return nil
}

// guestMoonlightPerm is the permission mask a role's Moonlight client gets
// (streamhost.Perm*). limited is false for a role that needs nothing taken away.
func guestMoonlightPerm(role guestauth.Role) (perm uint32, limited bool) {
	const (
		view  = streamhost.PermList | streamhost.PermView | streamhost.PermLaunch
		input = streamhost.PermInputController | streamhost.PermInputTouch | streamhost.PermInputPen |
			streamhost.PermInputMouse | streamhost.PermInputKbd
		clip = streamhost.PermClipboardSet | streamhost.PermClipboardRead
	)
	switch role {
	case guestauth.RoleViewer:
		return view, true
	case guestauth.RoleOperator:
		return view | input | clip, true
	case guestauth.RoleTechnician:
		// Everything but running commands on the host.
		return view | input | clip | streamhost.PermFileUpload | streamhost.PermFileDownload, true
	}
	return 0, true
}

func (a *App) guestPermissioner() streamhost.ClientPermissioner {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.stream == nil {
		return nil
	}
	p, _ := a.stream.(streamhost.ClientPermissioner)
	return p
}

// limitGuestClient applies perm to the guest's paired Moonlight client.
func (a *App) limitGuestClient(fingerprint string, perm uint32) error {
	p := a.guestPermissioner()
	if p == nil {
		return errors.New("stream host cannot limit clients")
	}
	port := a.cfg.SunshinePort
	if port == 0 {
		port = 47990
	}
	return p.SetClientPermissions(port, a.cfg.StateDir, fingerprint, perm)
}

func (a *App) guestCertTrusted(fingerprint string) bool {
	a.streamMu.Lock()
	kind := ""
	if a.stream != nil {
		kind = streamhost.BackendKind(a.stream)
	}
	a.streamMu.Unlock()
	return kind != "" && streamhost.TrustedFingerprints(a.cfg.StateDir, kind)[fingerprint]
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
			a.syncGuestNode(ctx)
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
func (a *App) guestSessionEnded(shareID string) {
	if a.guestKick != nil {
		go a.guestKick(shareID)
	}
}

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

// hasLiveGuestShare reports whether anyone currently has guest access here.
func (a *App) hasLiveGuestShare() bool {
	return a.guests != nil && a.guests.Store.LiveCount(time.Now()) > 0
}

// ownTailnetAddress is this machine's address on the owner's own Tailscale,
// for shares that run on it (Config.GuestOwnTailscale).
func (a *App) ownTailnetAddress(ctx context.Context) (string, error) {
	if a.ts == nil {
		return "", errors.New("Tailscale is not available")
	}
	st, err := a.ts.Status(ctx)
	if err != nil || st == nil || !st.Running || st.Self.IP4 == "" {
		return "", errors.New("this computer is not connected to Tailscale: sign in to Tailscale first")
	}
	return st.Self.IP4, nil
}
