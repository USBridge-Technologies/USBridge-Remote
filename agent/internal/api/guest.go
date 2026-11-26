package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"usbridge_agent/internal/guestauth"
)

// Guest access (see internal/guestauth). A guest request carries
// X-Guest-Ticket and is verified by SecurityMiddleware.verifyGuest instead of
// the master-key HMAC; routeCaps below says what each role may reach.
// Anything not listed is owner-only -- guests are denied by default.

type principalKey struct{}

// PrincipalFrom returns the guest a request was authenticated as, or nil for
// the owner (master key).
func PrincipalFrom(r *http.Request) *guestauth.Principal {
	p, _ := r.Context().Value(principalKey{}).(*guestauth.Principal)
	return p
}

// routeCaps: exact path -> capability a guest needs. Keep this the only
// place that widens what guests can reach.
var routeCaps = map[string]guestauth.Cap{
	"/api/status":              guestauth.CapView,
	"/api/service/status":      guestauth.CapView,
	"/api/screen":              guestauth.CapView,
	"/api/device/info":         guestauth.CapView,
	"/api/device/status":       guestauth.CapView,
	"/api/video/info":          guestauth.CapView,
	"/api/guest/wt-grant":      guestauth.CapView,
	"/api/video/devices":       guestauth.CapView,
	"/api/video/session_start": guestauth.CapView,
	"/api/video/session_end":   guestauth.CapView,
	"/api/audio/info":          guestauth.CapAudio,
	"/api/audio/devices":       guestauth.CapAudio,
	// Read-only hardware-KVM stubs that always return empty/disabled on the
	// software agent; permitting them under CapView prevents standard clients
	// from throwing 403 dialogs on initial connect.
	"/api/scripts/list":   guestauth.CapView,
	"/api/scripts/status": guestauth.CapView,
	"/api/storage/status": guestauth.CapView,

	// Pairing hands out a Moonlight client certificate; the agent limits it to
	// the role (view only for a Viewer) on the stream host, or refuses.
	"/api/guest/moonlight-pair": guestauth.CapView,

	"/api/keyboard":           guestauth.CapInput,
	"/api/keyboard/layout":    guestauth.CapInput,
	"/api/keyboard/text_mode": guestauth.CapInput,
	"/api/mouse":              guestauth.CapInput,
	"/api/mouse/ws":           guestauth.CapInput,

	"/api/clipboard/ws": guestauth.CapClipboard,

	"/api/usb/passthrough/status":              guestauth.CapView,
	"/api/usb/passthrough/install":             guestauth.CapUSB,
	"/api/usb/passthrough/session":             guestauth.CapUSB,
	"/api/usb/passthrough/browser-session":     guestauth.CapGamepad,
	"/api/usb/passthrough/browser-pen-session": guestauth.CapPenTablet,
	"/api/device/start":                        guestauth.CapUSB,
	"/api/device/stop":                         guestauth.CapUSB,

	"/api/drives/local":         guestauth.CapDisks,
	"/api/iso/space":            guestauth.CapDisks,
	"/api/iso/upload":           guestauth.CapDisks,
	"/api/backup/get_snapshots": guestauth.CapDisks,

	"/api/video/virtual_displays": guestauth.CapDisplay,
	"/api/video/monitors/enabled": guestauth.CapDisplayExclusive,
	"/api/video/monitors/primary": guestauth.CapDisplayMain,

	"/api/video/set_device": guestauth.CapVideoSettings,
	"/api/video/fec":        guestauth.CapVideoSettings,
	"/api/video/source":     guestauth.CapVideoSettings,
}

// guestOpenPaths are reachable by any verified guest without the host's
// consent -- they are how a guest asks for it.
var guestOpenPaths = map[string]bool{
	"/api/guest/hello": true,
	"/api/guest/me":    true,
}

// guestCap returns the capability path needs; ok is false for owner-only routes.
func guestCap(path string) (guestauth.Cap, bool) {
	if c, ok := routeCaps[path]; ok {
		return c, true
	}
	switch {
	case strings.HasPrefix(path, "/api/clipboard/blob/"):
		return guestauth.CapClipboard, true
	case strings.HasPrefix(path, "/api/video/virtual_displays/"):
		if strings.HasSuffix(path, "/primary") {
			return guestauth.CapDisplayMain, true
		}
		return guestauth.CapDisplay, true
	}
	return 0, false
}

// SetGuestManager turns guest access on. Without it every guest request is refused.
func (m *SecurityMiddleware) SetGuestManager(g *guestauth.Manager) {
	m.keyMu.Lock()
	m.guests = g
	m.keyMu.Unlock()
}

func (m *SecurityMiddleware) guestManager() *guestauth.Manager {
	m.keyMu.RLock()
	defer m.keyMu.RUnlock()
	return m.guests
}

func writeGuestDenied(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"success":false,"error":"` + code + `"}`))
}

// verifyGuest authenticates a request carrying X-Guest-Ticket, enforces the
// share's Policy capabilities and the host's consent, and stores the principal
// in the request context. body is what the guest signed (empty for the no-body variant).
func (m *SecurityMiddleware) verifyGuest(w http.ResponseWriter, r *http.Request, body []byte) bool {
	g := m.guestManager()
	if g == nil {
		writeGuestDenied(w, http.StatusForbidden, "guest_access_off")
		return false
	}
	p, err := g.Auth.Verify(r.Method, r.URL.RequestURI(), r.Header.Get("X-Auth-Timestamp"), r.Header.Get("X-Auth-Signature"), r.Header.Get("X-Guest-Ticket"), body)
	if err != nil {
		log.Printf("[security] guest rejected from %s path=%s: %v", m.clientIP(r), r.URL.Path, err)
		g.Audit(guestauth.AuditEvent{Event: "auth_failed", Detail: "guest " + m.clientIP(r) + " " + r.URL.Path})
		code := http.StatusUnauthorized
		if errors.Is(err, guestauth.ErrRevoked) {
			code = http.StatusForbidden
		}
		writeGuestDenied(w, code, "guest_auth_failed")
		return false
	}
	return m.authorizePrincipal(w, r, g, p, body, false)
}

// verifyLocalToken authenticates a self-hosted local access token carrying
// X-Access-Token-ID + X-Auth-Timestamp + X-Auth-Signature.
func (m *SecurityMiddleware) verifyLocalToken(w http.ResponseWriter, r *http.Request, body []byte) bool {
	g := m.guestManager()
	if g == nil {
		writeGuestDenied(w, http.StatusForbidden, "guest_access_off")
		return false
	}
	p, err := g.Auth.VerifyLocal(r.Method, r.URL.RequestURI(), r.Header.Get("X-Auth-Timestamp"), r.Header.Get("X-Auth-Signature"), r.Header.Get("X-Access-Token-ID"), body)
	if err != nil {
		log.Printf("[security] local token rejected from %s path=%s: %v", m.clientIP(r), r.URL.Path, err)
		g.Audit(guestauth.AuditEvent{ShareID: r.Header.Get("X-Access-Token-ID"), Event: "auth_failed", Detail: "local " + m.clientIP(r) + " " + r.URL.Path})
		code := http.StatusUnauthorized
		if errors.Is(err, guestauth.ErrRevoked) {
			code = http.StatusForbidden
		}
		writeGuestDenied(w, code, "guest_auth_failed")
		return false
	}
	return m.authorizePrincipal(w, r, g, p, body, true)
}

func (m *SecurityMiddleware) authorizePrincipal(w http.ResponseWriter, r *http.Request, g *guestauth.Manager, p *guestauth.Principal, body []byte, autoHello bool) bool {
	if guestOpenPaths[r.URL.Path] {
		*r = *r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
		return true
	}
	need, ok := guestCap(r.URL.Path)
	if !ok && r.URL.Path == "/api/pcpanel/leds" && r.Method == http.MethodGet {
		// Read-only LED status poll (stubbed on agent); POST stays owner-only.
		need, ok = guestauth.CapView, true
	}
	if ok && r.URL.Path == "/api/device/start" {
		// Each kind of device is gated by its own capability (input, pen, gamepad, disks, USB),
		// so a policy may start exactly the kinds it holds.
		if c, parsed := deviceStartCaps(body); parsed {
			need = c
		}
	}
	allowed := false
	if ok {
		switch {
		case r.URL.Path == "/api/clipboard/ws":
			// Allow opening the clipboard WS if either direction is enabled;
			// runClipboardDuplex enforces direction and file-transfer rules.
			allowed = p.Policy.HasAny(guestauth.CapClipToHost | guestauth.CapClipFromHost)
		case strings.HasPrefix(r.URL.Path, "/api/clipboard/blob/"):
			if r.Method == http.MethodPut {
				allowed = p.Policy.Has(guestauth.CapClipToHost)
			} else {
				allowed = p.Policy.Has(guestauth.CapClipFromHost)
			}
		case r.URL.Path == "/api/usb/passthrough/session":
			// USB passthrough session handshake is allowed if any USB/Pen/Gamepad cap is granted;
			// usb-broker enforces the exact per-device USBPolicy whitelist on attach.
			allowed = p.Policy.HasAny(guestauth.CapUSB | guestauth.CapPenTablet | guestauth.CapGamepad)
		default:
			allowed = p.Policy.Has(need)
		}
	}
	if !allowed {
		g.Audit(guestauth.AuditEvent{ShareID: p.ShareID, Event: "denied_cap", Detail: r.Method + " " + r.URL.Path})
		writeGuestDenied(w, http.StatusForbidden, "not_permitted")
		return false
	}
	if !g.Sessions.Allowed(p) {
		if autoHello {
			if st := g.Sessions.Hello(p); st != guestauth.StateAllowed {
				writeGuestDenied(w, http.StatusForbidden, "consent_required")
				return false
			}
		} else {
			writeGuestDenied(w, http.StatusForbidden, "consent_required")
			return false
		}
	}
	g.Store.Touch(p.ShareID, time.Now())
	*r = *r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
	return true
}

// deviceKindCap is the capability a guest needs to start one kind of device:
// keyboard / mouse need input, gamepad needs gamepad, pen/tablet/wacom need
// pen tablet, drives and MTP need disks, everything else needs USB.
func deviceKindCap(kind string) guestauth.Cap {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "keyboard", "mouse":
		return guestauth.CapInput
	case "gamepad":
		return guestauth.CapGamepad
	case "tablet", "pen", "wacom":
		return guestauth.CapPenTablet
	case "drive", "mtp":
		return guestauth.CapDisks
	}
	return guestauth.CapUSB
}

// deviceStartCaps returns every capability a /api/device/start body needs;
// ok is false for a body that is empty or not parseable (the caller then keeps
// the route's own capability).
func deviceStartCaps(body []byte) (need guestauth.Cap, ok bool) {
	var batch struct {
		Devices []struct {
			Device string `json:"device"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(body, &batch); err != nil || len(batch.Devices) == 0 {
		return 0, false
	}
	for _, d := range batch.Devices {
		need |= deviceKindCap(d.Device)
	}
	return need, true
}

// SetGuestManager enables guest access on the running server.
func (s *Server) SetGuestManager(g *guestauth.Manager) {
	s.guests = g
	s.sec.SetGuestManager(g)
}

// SetDisableMasterKeyRemote toggles TPN strict mode that blocks remote root master-key auth.
func (s *Server) SetDisableMasterKeyRemote(disabled bool) {
	s.sec.SetDisableMasterKeyRemote(disabled)
}

type guestInviteRequest struct {
	Email          string `json:"email"`
	Role           string `json:"role"`
	ExpiresIn      int64  `json:"expires_in"` // seconds from now; 0 = no end date
	RequireConsent *bool  `json:"require_consent"`
	// Policy, when set, is the checkbox matrix and replaces Role.
	Policy *guestauth.Policy `json:"policy,omitempty"`
}

type guestView struct {
	guestauth.Share
	Live    bool   `json:"live"`
	Session string `json:"session,omitempty"` // pending / allowed while connected
	// Cloud-side status ("invited" until the guest signs in), when reachable.
	Status        string `json:"status,omitempty"`
	AcceptedAt    int64  `json:"accepted_at,omitempty"`
	CloudLastSeen int64  `json:"cloud_last_seen,omitempty"`
}

// guestsList implements GET /api/guests (owner only).
func (s *Server) guestsList(w http.ResponseWriter, r *http.Request) {
	g := s.guests
	if g == nil {
		s.fail(w, http.StatusNotImplemented, "guest_access_off", nil)
		return
	}
	now := time.Now()
	sessions := map[string]guestauth.Session{}
	for _, sess := range g.Sessions.List() {
		sessions[sess.ShareID] = sess
	}
	remote := map[string]guestauth.RemoteShare{}
	if g.Cloud != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		if list, err := g.Cloud.ListShares(ctx); err == nil {
			for _, rs := range list {
				remote[rs.ID] = rs
			}
		}
		cancel()
	}
	shares := g.Store.List()
	out := make([]guestView, 0, len(shares))
	live := 0
	for _, sh := range shares {
		v := guestView{Share: sh, Live: sh.Live(now)}
		if v.Live {
			live++
		}
		if sess, ok := sessions[sh.ID]; ok {
			v.Session = string(sess.State)
		}
		if rs, ok := remote[sh.ID]; ok {
			v.Status, v.AcceptedAt, v.CloudLastSeen = rs.Status, rs.AcceptedAt, rs.LastSeen
		}
		out = append(out, v)
	}
	s.ok(w, "guests", map[string]any{"guests": out, "live": live, "free_limit": guestauth.FreeMaxShares, "paid": g.Paid != nil && g.Paid()})
}

// guestsInvite implements POST /api/guests (owner only).
func (s *Server) guestsInvite(w http.ResponseWriter, r *http.Request) {
	g := s.guests
	if g == nil {
		s.fail(w, http.StatusNotImplemented, "guest_access_off", nil)
		return
	}
	var req guestInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid_request", err)
		return
	}
	var expires int64
	if req.ExpiresIn < 0 || req.ExpiresIn > 366*24*3600 {
		s.fail(w, http.StatusBadRequest, "invalid_expiry", nil)
		return
	}
	if req.ExpiresIn > 0 {
		expires = time.Now().Add(time.Duration(req.ExpiresIn) * time.Second).Unix()
	}
	consent := true
	if req.RequireConsent != nil {
		consent = *req.RequireConsent
	}
	// The invitation outlives the client's connection: joining the guest
	// network can take longer than the owner's app waits, and a share that
	// is already created in the cloud must not be revoked just because the
	// app gave up early.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancel()
	sh, res, err := g.Invite(ctx, req.Email, guestauth.Role(req.Role), req.Policy, expires, consent)
	switch {
	case err == nil:
		s.ok(w, "guest_invited", map[string]any{"guest": sh, "invite_url": res.InviteURL, "mail_sent": res.MailSent})
	case errors.Is(err, guestauth.ErrTailnetUnavailable):
		s.fail(w, http.StatusBadGateway, "tailnet_unavailable", err)
	case errors.Is(err, guestauth.ErrFreeLimit):
		s.fail(w, http.StatusPaymentRequired, "free_limit", err)
	case errors.Is(err, guestauth.ErrBadShare):
		s.fail(w, http.StatusBadRequest, "invalid_guest", err)
	case errors.Is(err, guestauth.ErrRateLimit):
		s.fail(w, http.StatusTooManyRequests, "rate_limit", err)
	default:
		s.fail(w, http.StatusServiceUnavailable, "cloud_unavailable", err)
	}
}

// guestsRevoke implements DELETE /api/guests/{id} (owner only).
func (s *Server) guestsRevoke(w http.ResponseWriter, r *http.Request) {
	g := s.guests
	if g == nil {
		s.fail(w, http.StatusNotImplemented, "guest_access_off", nil)
		return
	}
	if err := g.Revoke(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, http.StatusNotFound, "no_such_guest", err)
		return
	}
	s.ok(w, "guest_revoked", nil)
}

// guestsEndSession implements POST /api/guests/{id}/end (owner only): disconnect now, keep the share.
func (s *Server) guestsEndSession(w http.ResponseWriter, r *http.Request) {
	if s.guests == nil {
		s.fail(w, http.StatusNotImplemented, "guest_access_off", nil)
		return
	}
	s.guests.Sessions.End(r.PathValue("id"), "kicked")
	s.ok(w, "session_ended", nil)
}

// guestHello implements POST /api/guest/hello: the guest asks to start a
// session; the answer is pending (host is being asked), allowed or denied.
func (s *Server) guestHello(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r)
	if p == nil || s.guests == nil {
		s.fail(w, http.StatusForbidden, "guest_only", nil)
		return
	}
	st := s.guests.Sessions.Hello(p)
	s.guests.Store.Touch(p.ShareID, time.Now())
	s.ok(w, "guest_hello", map[string]any{"state": st, "role": p.Role, "email": p.Email})
}

// guestPairer is implemented by the app: it relays a guest's pairing PIN to
// the stream host and records the certificate the guest then pairs with.
type guestPairer interface {
	PairGuestClient(shareID, fingerprint, pin string) error
}

// guestMoonlightPair implements POST /api/guest/moonlight-pair
// {"pin": "...", "fingerprint": "<sha256 hex of the client certificate DER>"}:
// the guest's Moonlight client is pairing and needs the PIN entered on this
// machine. Unlike the owner's /api/moonlight/pin this is authenticated by the
// guest ticket and signature, and the certificate that pairs is bound to the
// share so ending the share removes it.
func (s *Server) guestMoonlightPair(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r)
	if p == nil || s.guests == nil {
		s.fail(w, http.StatusForbidden, "guest_only", nil)
		return
	}
	if !s.guests.Sessions.Allowed(p) {
		s.fail(w, http.StatusForbidden, "consent_required", nil)
		return
	}
	pairer, ok := s.app.(guestPairer)
	if !ok {
		s.fail(w, http.StatusNotImplemented, "guest_pairing_unavailable", nil)
		return
	}
	var req struct {
		PIN         string `json:"pin"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid_request", err)
		return
	}
	req.PIN = strings.TrimSpace(req.PIN)
	req.Fingerprint = strings.ToLower(strings.TrimSpace(req.Fingerprint))
	if req.PIN == "" || len(req.PIN) > 16 || !validFingerprint(req.Fingerprint) {
		s.fail(w, http.StatusBadRequest, "invalid_request", nil)
		return
	}
	if err := pairer.PairGuestClient(p.ShareID, req.Fingerprint, req.PIN); err != nil {
		log.Printf("[guest] pairing for share %s: %v", p.ShareID, err)
		s.fail(w, http.StatusInternalServerError, "pairing_failed", err)
		return
	}
	s.guests.Audit(guestauth.AuditEvent{ShareID: p.ShareID, Event: "moonlight_pair", Detail: p.Email})
	s.ok(w, "pin_accepted", nil)
}

// validFingerprint: 64 hex digits (SHA-256).
func validFingerprint(fp string) bool {
	if len(fp) != 64 {
		return false
	}
	for _, c := range fp {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// guestMe implements GET /api/guest/me: what this guest is and where its session stands.
func (s *Server) guestMe(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r)
	if p == nil || s.guests == nil {
		s.fail(w, http.StatusForbidden, "guest_only", nil)
		return
	}
	state := guestauth.State("none")
	for _, sess := range s.guests.Sessions.List() {
		if sess.ShareID == p.ShareID {
			state = sess.State
		}
	}
	s.ok(w, "guest_me", map[string]any{"state": state, "role": p.Role, "email": p.Email})
}

// guestsDecide implements POST /api/guests/{id}/decide {"allow": bool} (owner
// only): answers a guest's pending request to connect, for an owner who is
// not at the machine.
func (s *Server) guestsDecide(w http.ResponseWriter, r *http.Request) {
	if s.guests == nil {
		s.fail(w, http.StatusNotImplemented, "guest_access_off", nil)
		return
	}
	var req struct {
		Allow bool `json:"allow"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid_request", err)
		return
	}
	if !s.guests.Sessions.Decide(r.PathValue("id"), req.Allow) {
		s.fail(w, http.StatusConflict, "nothing_pending", nil)
		return
	}
	s.ok(w, "decided", nil)
}

// Headers the agent sets, for rust-shine only, on an offer it has verified for a guest.
const (
	GuestRoleHeader  = "X-Usbridge-Guest-Role"
	GuestShareHeader = "X-Usbridge-Guest-Share"
	GuestCapsHeader  = "X-Usbridge-Guest-Caps"
)

func (s *Server) AuthorizeOffer(method, uri string, h http.Header, body []byte) (int, string) {
	h.Del(GuestRoleHeader)
	h.Del(GuestShareHeader)
	h.Del(GuestCapsHeader)
	ticket := h.Get("X-Guest-Ticket")
	localID := h.Get("X-Access-Token-ID")
	if ticket == "" && localID == "" {
		if s.sec.isMasterKeyRemoteDisabled() {
			return http.StatusForbidden, "master_key_disabled"
		}
		return 0, ""
	}
	g := s.guests
	if g == nil {
		return http.StatusForbidden, "guest_access_off"
	}
	var (
		p   *guestauth.Principal
		err error
	)
	if localID != "" {
		p, err = g.Auth.VerifyLocal(method, uri, h.Get("X-Auth-Timestamp"), h.Get("X-Auth-Signature"), localID, offerSignedBody(body))
	} else {
		p, err = g.Auth.Verify(method, uri, h.Get("X-Auth-Timestamp"), h.Get("X-Auth-Signature"), ticket, offerSignedBody(body))
	}
	if err != nil {
		log.Printf("[security] guest offer rejected: %v", err)
		if errors.Is(err, guestauth.ErrRevoked) {
			return http.StatusForbidden, "share_ended"
		}
		return http.StatusUnauthorized, "guest_auth_failed"
	}
	if !p.Policy.Has(guestauth.CapView) {
		return http.StatusForbidden, "not_permitted"
	}
	if st := g.Sessions.Hello(p); st != guestauth.StateAllowed {
		if st == guestauth.StatePending {
			return http.StatusAccepted, "consent_pending"
		}
		return http.StatusForbidden, "consent_denied"
	}
	g.Store.Touch(p.ShareID, time.Now())

	key := s.currentMasterKey()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	h.Del("X-Guest-Ticket")
	h.Del("X-Access-Token-ID")
	h.Set("X-Auth-Timestamp", ts)
	h.Set("X-Auth-Signature", CalculateHMAC(method, uri, ts, string(body), key))
	h.Set(GuestRoleHeader, string(p.Role))
	h.Set(GuestShareHeader, p.ShareID)
	h.Set(GuestCapsHeader, fmt.Sprintf("0x%x", uint32(p.Policy.Caps)))
	return 0, ""
}

// webRTCKickURL is rust-shine's call that ends a guest's live sessions.
var webRTCKickURL = "http://127.0.0.1:8444/webrtc/kick"

// KickGuest ends the live streaming sessions (WebRTC and WebTransport) of a
// share that was revoked, expired or kicked. New requests are refused by the
// agent already; this is what stops a stream that is running.
func (s *Server) KickGuest(shareID string) {
	key := s.currentMasterKey()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequest(http.MethodPost, webRTCKickURL, strings.NewReader(shareID))
	if err != nil {
		return
	}
	req.Header.Set("X-Auth-Timestamp", ts)
	req.Header.Set("X-Auth-Signature", CalculateHMAC("POST", "/webrtc/kick", ts, shareID, key))
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return // no streamer running: nothing to end
	}
	resp.Body.Close()
}

// webTransportInfoURL is rust-shine's local signaling port (see webrtcProxy).
var webTransportInfoURL = "http://127.0.0.1:8444/webtransport/info"

// guestWTGrant implements POST /api/guest/wt-grant: what a guest's browser
// needs to open a WebTransport session. rust-shine authenticates the CONNECT
// with the master-key HMAC, which a guest doesn't have, so the agent signs it
// for them -- short-lived (rust-shine's 60 s window) and with the guest's role
// as the signed body, so rust-shine enforces that role and the grant can't be
// turned into an owner's.
func (s *Server) guestWTGrant(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r)
	if p == nil {
		s.fail(w, http.StatusForbidden, "guest_only", nil)
		return
	}
	key := s.currentMasterKey()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, webTransportInfoURL, nil)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "internal", err)
		return
	}
	req.Header.Set("X-Auth-Timestamp", ts)
	req.Header.Set("X-Auth-Signature", CalculateHMAC("GET", "/webtransport/info", ts, "", key))
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		s.fail(w, http.StatusBadGateway, "webtransport_unavailable", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		s.fail(w, http.StatusBadGateway, "webtransport_unavailable", nil)
		return
	}
	var info map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&info); err != nil {
		s.fail(w, http.StatusBadGateway, "webtransport_unavailable", err)
		return
	}
	capsHex := fmt.Sprintf("0x%x", uint32(p.Policy.Caps))
	info["ts"] = ts
	info["role"] = string(p.Role)
	info["caps"] = capsHex
	info["share"] = p.ShareID
	info["sig"] = CalculateHMAC("CONNECT", "/webtransport", ts, string(p.Role)+"|"+p.ShareID+"|"+capsHex, key)
	s.ok(w, "wt_grant", info)
}

// offerSignedBody is what a guest signs of its offer: the SDP itself, not the
// JSON around it. The cloud relay re-wraps the browser's JSON (adds type,
// request_id, hw_id, headers) before the agent sees it, so a signature over
// the body bytes could never survive that hop; the SDP does.
func offerSignedBody(body []byte) []byte {
	var o struct {
		SDP string `json:"sdp"`
	}
	if json.Unmarshal(body, &o) == nil && o.SDP != "" {
		return []byte(o.SDP)
	}
	return body
}
