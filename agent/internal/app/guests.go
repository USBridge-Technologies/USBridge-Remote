package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/config"
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
		Store:      store,
		Auth:       guestauth.NewAuthenticator(store, entitlement.PublicKey(), hwID),
		LocalAudit: guestauth.NewLocalAuditWriter(a.cfg.StateDir),
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
			a.syncUSBPolicyFile()
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
	apiServer.SetDisableMasterKeyRemote(a.cfg.DisableMasterKeyRemote)
	relayOfferAuthorizer = apiServer.AuthorizeOffer
	a.syncUSBPolicyFile()
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
	pol := sh.EffectivePolicy()
	perm, limited := guestMoonlightPermPolicy(pol)
	if a.guestPermissioner() == nil {
		if !pol.Has(guestauth.CapInput) {
			// This stream host trusts a paired client fully: it cannot be told to
			// keep a view-only guest from the keyboard, so none is paired.
			return errors.New("this stream host cannot limit a guest's input; use the browser")
		}
		limited = false // host cannot enforce per-client masks
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

// guestMoonlightPerm is the permission mask a legacy role's Moonlight client gets.
func guestMoonlightPerm(role guestauth.Role) (perm uint32, limited bool) {
	return guestMoonlightPermPolicy(guestauth.PolicyForRole(role))
}

// guestMoonlightPermPolicy maps a granular Policy bitmask onto Sunshine/RustShine
// per-client Moonlight permissions (streamhost.Perm*).
func guestMoonlightPermPolicy(pol guestauth.Policy) (perm uint32, limited bool) {
	if pol.Has(guestauth.CapView) {
		perm |= streamhost.PermList | streamhost.PermView | streamhost.PermLaunch
	}
	if pol.Has(guestauth.CapInput) {
		perm |= streamhost.PermInputMouse | streamhost.PermInputKbd | streamhost.PermInputTouch
	}
	if pol.Has(guestauth.CapPenTablet) {
		perm |= streamhost.PermInputPen
	}
	if pol.Has(guestauth.CapGamepad) {
		perm |= streamhost.PermInputController
	}
	if pol.Has(guestauth.CapClipToHost) {
		perm |= streamhost.PermClipboardSet
	}
	if pol.Has(guestauth.CapClipFromHost) {
		perm |= streamhost.PermClipboardRead
	}
	if pol.Has(guestauth.CapFileTransfer) {
		perm |= streamhost.PermFileUpload | streamhost.PermFileDownload
	}
	return perm, true
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

// ErrAdminPINRequired is returned when an operation requires a valid Admin PIN
// (either because AlwaysRequirePIN is enabled or a restricted client is connected).
var ErrAdminPINRequired = errors.New("admin PIN required")

// usbPolicyFile is the on-disk JSON contract read by rust-shine's usb-broker
// (via USBRIDGE_USB_POLICY_FILE) before every vhci_attach.
type usbPolicyFile struct {
	Restricted bool                `json:"restricted"`
	Policy     guestauth.USBPolicy `json:"policy"`
}

// syncUSBPolicyFile atomically writes <stateDir>/usb_policy.json (0600) with
// the intersection of all currently active restricted sessions' USB policies.
// When no restricted session is active, restricted=false (owner/root unrestricted).
func (a *App) syncUSBPolicyFile() {
	if a.cfg.StateDir == "" {
		return
	}
	var out usbPolicyFile
	if a.guests != nil {
		active := a.guests.Sessions.ActiveAllowed()
		if len(active) > 0 {
			out.Restricted = true
			merged := active[0].Policy.USB
			for _, sess := range active[1:] {
				u := sess.Policy.USB
				merged.AllowHID = merged.AllowHID && u.AllowHID
				merged.AllowPenTablet = merged.AllowPenTablet && u.AllowPenTablet
				merged.AllowAudio = merged.AllowAudio && u.AllowAudio
				merged.AllowMassStorage = merged.AllowMassStorage && u.AllowMassStorage
				merged.AllowOther = merged.AllowOther && u.AllowOther
				// Intersect VID:PID lists across concurrent restricted sessions.
				if len(merged.AllowedVIDPIDs) > 0 && len(u.AllowedVIDPIDs) > 0 {
					set := map[string]bool{}
					for _, id := range u.AllowedVIDPIDs {
						set[strings.ToLower(strings.TrimSpace(id))] = true
					}
					var common []string
					for _, id := range merged.AllowedVIDPIDs {
						if set[strings.ToLower(strings.TrimSpace(id))] {
							common = append(common, id)
						}
					}
					merged.AllowedVIDPIDs = common
				} else {
					merged.AllowedVIDPIDs = nil
				}
			}
			out.Policy = merged
		}
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(a.cfg.StateDir, "usb_policy.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (a *App) checkAdminPIN(pin string) error {
	if !a.cfg.AdminPINConfigured() {
		return nil
	}
	needPIN := a.cfg.AlwaysRequirePIN || len(a.ActiveRestrictedSessions()) > 0
	if !needPIN {
		return nil
	}
	if !config.VerifyAdminPIN(a.cfg.AdminPINHash, pin) {
		if a.guests != nil {
			a.guests.Audit(guestauth.AuditEvent{Event: "admin_pin_failed"})
		}
		return ErrAdminPINRequired
	}
	return nil
}

// ActiveRestrictedSessions returns all currently connected non-root sessions.
func (a *App) ActiveRestrictedSessions() []guestauth.Session {
	if a.guests == nil {
		return nil
	}
	return a.guests.Sessions.ActiveAllowed()
}

// ListAccessTokens returns all recorded local tokens and guest shares (newest first).
func (a *App) ListAccessTokens() []guestauth.Share {
	if a.guests == nil {
		return nil
	}
	return a.guests.Store.List()
}

// CreateLocalToken creates a self-hosted local access token directly on this
// agent with a granular Policy matrix. Returns the share and formatted usbloc1 token.
func (a *App) CreateLocalToken(adminPIN, label string, policy guestauth.Policy, expiresInSec int64, requireConsent bool) (*guestauth.Share, string, error) {
	if err := a.checkAdminPIN(adminPIN); err != nil {
		return nil, "", err
	}
	if a.guests == nil {
		return nil, "", errors.New("access manager unavailable")
	}
	var expiresAt int64
	if expiresInSec > 0 {
		expiresAt = time.Now().Add(time.Duration(expiresInSec) * time.Second).Unix()
	}
	return a.guests.CreateLocal(label, policy, expiresAt, requireConsent)
}

// UpdateAccessPolicy updates an existing share/token's label and Policy matrix.
func (a *App) UpdateAccessPolicy(adminPIN, id, label string, policy guestauth.Policy, requireConsent bool, expiresAt int64) (*guestauth.Share, error) {
	if err := a.checkAdminPIN(adminPIN); err != nil {
		return nil, err
	}
	if a.guests == nil {
		return nil, errors.New("access manager unavailable")
	}
	sh, err := a.guests.UpdatePolicy(id, label, policy, requireConsent, expiresAt)
	if err != nil {
		return nil, err
	}
	// Re-apply Moonlight client permissions for any certificates bound to this share.
	if a.guestPermissioner() != nil && len(sh.Fingerprints) > 0 {
		perm, _ := guestMoonlightPermPolicy(sh.EffectivePolicy())
		for _, fp := range sh.Fingerprints {
			_ = a.limitGuestClient(fp, perm)
		}
	}
	a.syncUSBPolicyFile()
	return sh, nil
}

// RevokeAccessToken permanently revokes a share/token and disconnects live sessions.
func (a *App) RevokeAccessToken(adminPIN, id string) error {
	if err := a.checkAdminPIN(adminPIN); err != nil {
		return err
	}
	if a.guests == nil {
		return errors.New("access manager unavailable")
	}
	err := a.guests.Revoke(context.Background(), id)
	a.syncUSBPolicyFile()
	return err
}

// KickAccessSession ends a live session without revoking its token.
func (a *App) KickAccessSession(adminPIN, id string) error {
	if err := a.checkAdminPIN(adminPIN); err != nil {
		return err
	}
	if a.guests == nil {
		return errors.New("access manager unavailable")
	}
	a.guests.Sessions.End(id, "kicked")
	a.syncUSBPolicyFile()
	return nil
}

// AdminPINStatus reports the current TPN security & Admin PIN flags.
func (a *App) AdminPINStatus() (configured, alwaysRequire, disableMasterRemote bool) {
	return a.cfg.AdminPINConfigured(), a.cfg.AlwaysRequirePIN, a.cfg.DisableMasterKeyRemote
}

// VerifyAdminPIN checks pin against the configured Argon2id hash.
func (a *App) VerifyAdminPIN(pin string) bool {
	if !a.cfg.AdminPINConfigured() {
		return true
	}
	return config.VerifyAdminPIN(a.cfg.AdminPINHash, pin)
}

// SetAdminPIN sets, changes, or clears (when newPIN == "") the Admin PIN.
// Changing or clearing an existing PIN requires currentPIN to match.
func (a *App) SetAdminPIN(currentPIN, newPIN string) error {
	if a.cfg.AdminPINConfigured() && !config.VerifyAdminPIN(a.cfg.AdminPINHash, currentPIN) {
		return ErrAdminPINRequired
	}
	next := a.cfg
	if newPIN == "" {
		next.AdminPINHash = ""
		next.AlwaysRequirePIN = false
	} else {
		hash, err := config.HashAdminPIN(newPIN)
		if err != nil {
			return err
		}
		next.AdminPINHash = hash
	}
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	if a.guests != nil {
		a.guests.Audit(guestauth.AuditEvent{Event: "admin_pin_changed"})
	}
	return nil
}

// SetSecurityOptions updates AlwaysRequirePIN and DisableMasterKeyRemote.
func (a *App) SetSecurityOptions(adminPIN string, alwaysRequirePIN, disableMasterKeyRemote bool) error {
	if a.cfg.AdminPINConfigured() && !config.VerifyAdminPIN(a.cfg.AdminPINHash, adminPIN) {
		return ErrAdminPINRequired
	}
	if alwaysRequirePIN && !a.cfg.AdminPINConfigured() {
		return errors.New("set an Admin PIN before enabling Always Require PIN")
	}
	next := a.cfg
	next.AlwaysRequirePIN = alwaysRequirePIN
	next.DisableMasterKeyRemote = disableMasterKeyRemote
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	if a.apiServer != nil {
		a.apiServer.SetDisableMasterKeyRemote(disableMasterKeyRemote)
	}
	if a.guests != nil {
		a.guests.Audit(guestauth.AuditEvent{Event: "security_options_updated"})
	}
	return nil
}
