package api

import (
	"context"
	"encoding/json"
	"errors"
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

	"/api/keyboard":        guestauth.CapInput,
	"/api/keyboard/layout": guestauth.CapInput,
	"/api/mouse":           guestauth.CapInput,
	"/api/mouse/ws":        guestauth.CapInput,

	"/api/clipboard/ws": guestauth.CapClipboard,

	"/api/usb/passthrough/status":              guestauth.CapUSB,
	"/api/usb/passthrough/install":             guestauth.CapUSB,
	"/api/usb/passthrough/session":             guestauth.CapUSB,
	"/api/usb/passthrough/browser-session":     guestauth.CapUSB,
	"/api/usb/passthrough/browser-pen-session": guestauth.CapUSB,
	"/api/device/start":                        guestauth.CapUSB,
	"/api/device/stop":                         guestauth.CapUSB,

	"/api/drives/local":         guestauth.CapDisks,
	"/api/iso/space":            guestauth.CapDisks,
	"/api/backup/get_snapshots": guestauth.CapDisks,

	"/api/video/virtual_displays": guestauth.CapDisplay,
	"/api/video/monitors/enabled": guestauth.CapDisplay,

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
// role's capabilities and the host's consent, and stores the principal in the
// request context. body is what the guest signed (empty for the no-body variant).
func (m *SecurityMiddleware) verifyGuest(w http.ResponseWriter, r *http.Request, body []byte) bool {
	g := m.guestManager()
	if g == nil {
		writeGuestDenied(w, http.StatusForbidden, "guest_access_off")
		return false
	}
	p, err := g.Auth.Verify(r.Method, r.URL.RequestURI(), r.Header.Get("X-Auth-Timestamp"), r.Header.Get("X-Auth-Signature"), r.Header.Get("X-Guest-Ticket"), body)
	if err != nil {
		log.Printf("[security] guest rejected from %s path=%s: %v", m.clientIP(r), r.URL.Path, err)
		code := http.StatusUnauthorized
		if errors.Is(err, guestauth.ErrRevoked) {
			code = http.StatusForbidden
		}
		writeGuestDenied(w, code, "guest_auth_failed")
		return false
	}
	if guestOpenPaths[r.URL.Path] {
		*r = *r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
		return true
	}
	need, ok := guestCap(r.URL.Path)
	if !ok || !p.Role.Has(need) {
		writeGuestDenied(w, http.StatusForbidden, "not_permitted")
		return false
	}
	if !g.Sessions.Allowed(p) {
		writeGuestDenied(w, http.StatusForbidden, "consent_required")
		return false
	}
	g.Store.Touch(p.ShareID, time.Now())
	*r = *r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
	return true
}

// SetGuestManager enables guest access on the running server.
func (s *Server) SetGuestManager(g *guestauth.Manager) {
	s.guests = g
	s.sec.SetGuestManager(g)
}

type guestInviteRequest struct {
	Email          string `json:"email"`
	Role           string `json:"role"`
	ExpiresIn      int64  `json:"expires_in"` // seconds from now; 0 = no end date
	RequireConsent *bool  `json:"require_consent"`
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
	sh, res, err := g.Invite(r.Context(), req.Email, guestauth.Role(req.Role), expires, consent)
	switch {
	case err == nil:
		s.ok(w, "guest_invited", map[string]any{"guest": sh, "invite_url": res.InviteURL, "mail_sent": res.MailSent})
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
)

// AuthorizeOffer vets a WebRTC "POST /webrtc/offer" before it reaches
// rust-shine, whether it arrived directly or through the cloud signaling
// relay. h is the request's headers and is rewritten in place.
//
// An owner's request (no guest ticket) is passed on untouched -- rust-shine
// verifies the master-key HMAC itself -- except that any guest role headers
// are stripped, so a request can't claim a role it wasn't given.
//
// A guest's request is verified here (ticket, guest signature, share live in
// the local record, view permission, host consent -- an offer is also the
// guest's "hello", since over the relay it is the only thing that gets
// through) and then re-signed with the master key so rust-shine accepts it,
// with the guest's role attached for rust-shine to enforce. It returns the
// HTTP status and a short code on refusal, 0 on success.
func (s *Server) AuthorizeOffer(method, uri string, h http.Header, body []byte) (int, string) {
	h.Del(GuestRoleHeader)
	h.Del(GuestShareHeader)
	ticket := h.Get("X-Guest-Ticket")
	if ticket == "" {
		return 0, ""
	}
	g := s.guests
	if g == nil {
		return http.StatusForbidden, "guest_access_off"
	}
	p, err := g.Auth.Verify(method, uri, h.Get("X-Auth-Timestamp"), h.Get("X-Auth-Signature"), ticket, offerSignedBody(body))
	if err != nil {
		log.Printf("[security] guest offer rejected: %v", err)
		if errors.Is(err, guestauth.ErrRevoked) {
			return http.StatusForbidden, "share_ended"
		}
		return http.StatusUnauthorized, "guest_auth_failed"
	}
	if !p.Role.Has(guestauth.CapView) {
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
	h.Set("X-Auth-Timestamp", ts)
	h.Set("X-Auth-Signature", CalculateHMAC(method, uri, ts, string(body), key))
	h.Set(GuestRoleHeader, string(p.Role))
	h.Set(GuestShareHeader, p.ShareID)
	return 0, ""
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
	info["ts"] = ts
	info["role"] = string(p.Role)
	info["sig"] = CalculateHMAC("CONNECT", "/webtransport", ts, string(p.Role), key)
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
