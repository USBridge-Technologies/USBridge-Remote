package api

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"usbridge_agent/internal/guestauth"
)

type guestKit struct {
	t       *testing.T
	srv     http.Handler
	mgr     *guestauth.Manager
	backend ed25519.PrivateKey
	guest   *ecdsa.PrivateKey
	asked   chan guestauth.Session
}

func newGuestKit(t *testing.T) *guestKit {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	st, _ := guestauth.OpenStore(t.TempDir())
	k := &guestKit{t: t, backend: priv, asked: make(chan guestauth.Session, 4)}
	k.guest, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	k.mgr = &guestauth.Manager{
		Store: st,
		Auth:  guestauth.NewAuthenticator(st, pub, func() string { return "hw-1" }),
		Paid:  func() bool { return true },
	}
	k.mgr.Sessions = guestauth.NewSessions(func(s guestauth.Session) { k.asked <- s }, nil, nil)
	s := NewServerWithAuth(nil, []byte("owner-master-key"), 0)
	s.SetGuestManager(k.mgr)
	k.srv = s.Routes()
	return k
}

func (k *guestKit) ticketFor(sh *guestauth.Share) string {
	raw := append([]byte{4}, append(k.guest.X.FillBytes(make([]byte, 32)), k.guest.Y.FillBytes(make([]byte, 32))...)...)
	now := time.Now()
	c, _ := json.Marshal(guestauth.Claims{V: 1, SID: sh.ID, HW: "hw-1", Em: sh.Email, Role: sh.Role, RC: sh.RequireConsent,
		GPK: base64.StdEncoding.EncodeToString(raw), Iat: now.Unix(), Exp: now.Add(10 * time.Minute).Unix()})
	p := base64.RawURLEncoding.EncodeToString(c)
	return guestauth.TicketPrefix + "." + p + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(k.backend, []byte(guestauth.TicketPrefix+"."+p)))
}

func (k *guestKit) do(sh *guestauth.Share, method, uri, body string) *httptest.ResponseRecorder {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	d := sha256.Sum256([]byte(method + uri + ts + body))
	r, s, _ := ecdsa.Sign(rand.Reader, k.guest, d[:])
	sig := base64.StdEncoding.EncodeToString(append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...))
	req := httptest.NewRequest(method, uri, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Guest-Ticket", k.ticketFor(sh))
	req.Header.Set("X-Auth-Timestamp", ts)
	req.Header.Set("X-Auth-Signature", sig)
	w := httptest.NewRecorder()
	k.srv.ServeHTTP(w, req)
	return w
}

func (k *guestKit) share(role guestauth.Role, consent bool) *guestauth.Share {
	sh, err := k.mgr.Store.Add("anna@corp.example", role, 0, consent, time.Now())
	if err != nil {
		k.t.Fatal(err)
	}
	return sh
}

func TestGuestOwnerOnlyRoutesAreDenied(t *testing.T) {
	k := newGuestKit(t)
	sh := k.share(guestauth.RoleTechnician, false) // the most privileged role
	k.mgr.Sessions.Hello(&guestauth.Principal{ShareID: sh.ID, Email: sh.Email, Role: sh.Role})
	for _, c := range []struct{ method, uri string }{
		{"POST", "/api/service/stop"}, {"POST", "/api/service/restart"}, {"POST", "/api/mcp"},
		{"POST", "/api/moonlight/pin"}, {"GET", "/api/guests"}, {"POST", "/api/guests"},
		{"DELETE", "/api/guests/abc"}, {"POST", "/api/bench/prepare"}, {"POST", "/api/auth/tailscale/register"},
		{"POST", "/api/pcpanel/leds"},
	} {
		w := k.do(sh, c.method, c.uri, "")
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "not_permitted") {
			t.Errorf("%s %s: got %d %s, want 403 not_permitted", c.method, c.uri, w.Code, w.Body.String())
		}
	}
}

func TestGuestRolesAndConsent(t *testing.T) {
	k := newGuestKit(t)
	viewer := k.share(guestauth.RoleViewer, true)

	// No session yet: even a permitted route needs the host's consent first.
	if w := k.do(viewer, "GET", "/api/status", ""); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "consent_required") {
		t.Fatalf("before hello: %d %s", w.Code, w.Body.String())
	}
	if w := k.do(viewer, "POST", "/api/guest/hello", "{}"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pending") {
		t.Fatalf("hello: %d %s", w.Code, w.Body.String())
	}
	select {
	case s := <-k.asked:
		if s.Email != "anna@corp.example" {
			t.Fatalf("asked about %q", s.Email)
		}
	default:
		t.Fatal("host was not asked")
	}
	if w := k.do(viewer, "GET", "/api/status", ""); w.Code != http.StatusForbidden {
		t.Fatalf("pending session must not be allowed: %d", w.Code)
	}
	k.mgr.Sessions.Decide(viewer.ID, true)
	if w := k.do(viewer, "GET", "/api/guest/me", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "allowed") {
		t.Fatalf("me: %d %s", w.Code, w.Body.String())
	}
	// Viewer reaches view routes but not input or video settings.
	if w := k.do(viewer, "POST", "/api/mouse", `{}`); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "not_permitted") {
		t.Fatalf("viewer mouse: %d %s", w.Code, w.Body.String())
	}
	if w := k.do(viewer, "POST", "/api/video/fec", `{}`); w.Code != http.StatusForbidden {
		t.Fatalf("viewer fec: %d", w.Code)
	}
}

func TestGuestRevokedMidSession(t *testing.T) {
	k := newGuestKit(t)
	sh := k.share(guestauth.RoleOperator, false)
	if w := k.do(sh, "POST", "/api/guest/hello", "{}"); w.Code != http.StatusOK {
		t.Fatalf("hello: %d", w.Code)
	}
	if err := k.mgr.Revoke(t.Context(), sh.ID); err != nil {
		t.Fatal(err)
	}
	if w := k.do(sh, "GET", "/api/status", ""); w.Code != http.StatusForbidden {
		t.Fatalf("after revoke: %d %s", w.Code, w.Body.String())
	}
}

func TestGuestWithoutManagerIsRefusedAndOwnerUnaffected(t *testing.T) {
	s := NewServerWithAuth(nil, []byte("owner-master-key"), 0)
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("X-Guest-Ticket", "usbgst1.x.y")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d", w.Code)
	}
}

func TestAuthorizeOffer(t *testing.T) {
	k := newGuestKit(t)
	s := NewServerWithAuth(nil, []byte("owner-master-key"), 0)
	s.SetGuestManager(k.mgr)
	sh := k.share(guestauth.RoleViewer, false)
	body := []byte(`{"sdp":"v=0"}`)

	signed := func(ticket string) http.Header {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		d := sha256.Sum256([]byte("POST/webrtc/offer" + ts + "v=0")) // a guest signs the SDP, see offerSignedBody
		r, sg, _ := ecdsa.Sign(rand.Reader, k.guest, d[:])
		h := http.Header{}
		h.Set("X-Guest-Ticket", ticket)
		h.Set("X-Auth-Timestamp", ts)
		h.Set("X-Auth-Signature", base64.StdEncoding.EncodeToString(append(r.FillBytes(make([]byte, 32)), sg.FillBytes(make([]byte, 32))...)))
		return h
	}

	h := signed(k.ticketFor(sh))
	if status, code := s.AuthorizeOffer("POST", "/webrtc/offer", h, body); status != 0 {
		t.Fatalf("guest offer refused: %d %s", status, code)
	}
	if h.Get("X-Guest-Ticket") != "" || h.Get(GuestRoleHeader) != "viewer" || h.Get(GuestShareHeader) != sh.ID {
		t.Fatalf("headers not rewritten: %v", h)
	}
	want := CalculateHMAC("POST", "/webrtc/offer", h.Get("X-Auth-Timestamp"), string(body), []byte("owner-master-key"))
	if h.Get("X-Auth-Signature") != want {
		t.Fatal("offer not re-signed with the master key for rustshine")
	}

	// Owner passthrough: untouched, but a forged role header is stripped.
	oh := http.Header{}
	oh.Set("X-Auth-Signature", "owner-sig")
	oh.Set(GuestRoleHeader, "viewer")
	if status, _ := s.AuthorizeOffer("POST", "/webrtc/offer", oh, body); status != 0 || oh.Get("X-Auth-Signature") != "owner-sig" || oh.Get(GuestRoleHeader) != "" {
		t.Fatalf("owner offer altered: %v", oh)
	}

	// Revoked share, tampered body, consent pending.
	_ = k.mgr.Store.Revoke(sh.ID, time.Now())
	if status, _ := s.AuthorizeOffer("POST", "/webrtc/offer", signed(k.ticketFor(sh)), body); status == 0 {
		t.Fatal("revoked guest offer accepted")
	}
	ask := k.share(guestauth.RoleViewer, true)
	if status, code := s.AuthorizeOffer("POST", "/webrtc/offer", signed(k.ticketFor(ask)), body); status != http.StatusAccepted || code != "consent_pending" {
		t.Fatalf("consent: %d %s", status, code)
	}
	if status, _ := s.AuthorizeOffer("POST", "/webrtc/offer", signed(k.ticketFor(ask)), []byte(`{"sdp":"v=0 tampered"}`)); status == 0 {
		t.Fatal("tampered SDP accepted")
	}
	// The relay wraps the browser's JSON in more fields; the signature must survive that.
	wrapped := []byte(`{"type":"offer","request_id":"r","headers":{},"hw_id":"hw-1","sdp":"v=0"}`)
	if status, code := s.AuthorizeOffer("POST", "/webrtc/offer", signed(k.ticketFor(sh2(k))), wrapped); status != 0 {
		t.Fatalf("relay-wrapped offer refused: %d %s", status, code)
	}
}

func TestGuestWTGrantSignsRoleForRustshine(t *testing.T) {
	info := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts := r.Header.Get("X-Auth-Timestamp")
		if r.Header.Get("X-Auth-Signature") != CalculateHMAC("GET", "/webtransport/info", ts, "", []byte("owner-master-key")) {
			http.Error(w, "bad sig", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"port":4433,"cert_hash":"abc","path":"/webtransport","version":1}`))
	}))
	defer info.Close()
	prev := webTransportInfoURL
	webTransportInfoURL = info.URL
	defer func() { webTransportInfoURL = prev }()

	k := newGuestKit(t)
	sh := k.share(guestauth.RoleViewer, false)
	k.do(sh, "POST", "/api/guest/hello", "{}")
	w := k.do(sh, "POST", "/api/guest/wt-grant", "{}")
	if w.Code != http.StatusOK {
		t.Fatalf("grant: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	ts, _ := resp.Data["ts"].(string)
	share, _ := resp.Data["share"].(string)
	if resp.Data["role"] != "viewer" || resp.Data["cert_hash"] != "abc" || share == "" ||
		resp.Data["sig"] != CalculateHMAC("CONNECT", "/webtransport", ts, "viewer|"+share, []byte("owner-master-key")) {
		t.Fatalf("grant content: %v", resp.Data)
	}
	// The role is signed in: the same signature is not an owner's (empty body) one.
	if resp.Data["sig"] == CalculateHMAC("CONNECT", "/webtransport", ts, "", []byte("owner-master-key")) {
		t.Fatal("guest grant is valid as an owner grant")
	}
}

func sh2(k *guestKit) *guestauth.Share { return k.share(guestauth.RoleViewer, false) }

func TestKickGuestSignsTheShareForRustshine(t *testing.T) {
	var gotBody, gotSig, gotTS string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotSig, gotTS = string(b), r.Header.Get("X-Auth-Signature"), r.Header.Get("X-Auth-Timestamp")
	}))
	defer ts.Close()
	old := webRTCKickURL
	webRTCKickURL = ts.URL + "/webrtc/kick"
	defer func() { webRTCKickURL = old }()

	s := &Server{masterKey: []byte("owner-master-key")}
	s.KickGuest("share-1")
	if gotBody != "share-1" || gotSig != CalculateHMAC("POST", "/webrtc/kick", gotTS, "share-1", []byte("owner-master-key")) {
		t.Fatalf("kick request: body=%q sig=%q", gotBody, gotSig)
	}
}

func TestGuestDeviceStartNeedsPerKindCaps(t *testing.T) {
	in, disks, usb := guestauth.CapInput, guestauth.CapDisks, guestauth.CapUSB
	for body, want := range map[string]guestauth.Cap{
		`{"devices":[{"device":"keyboard"},{"device":"mouse","type":"absolute"}]}`: in,
		`{"devices":[{"device":"gamepad"}]}`:                                       in,
		`{"devices":[{"device":"keyboard"},{"device":"drive"}]}`:                   in | disks,
		`{"devices":[{"device":"mtp"}]}`:                                           disks,
		`{"devices":[{"device":"audio"}]}`:                                         usb,
	} {
		if got, ok := deviceStartCaps([]byte(body)); !ok || got != want {
			t.Errorf("%q: got %v/%v, want %v", body, got, ok, want)
		}
	}
	for _, body := range []string{`{"devices":[]}`, `not json`, ``} {
		if _, ok := deviceStartCaps([]byte(body)); ok {
			t.Errorf("%q: should not parse", body)
		}
	}

	// Through the middleware: an Operator is refused a drive (USB capability),
	// even mixed with a keyboard, before the handler runs.
	k := newGuestKit(t)
	sh := k.share(guestauth.RoleOperator, false)
	k.mgr.Sessions.Hello(&guestauth.Principal{ShareID: sh.ID, Email: sh.Email, Role: sh.Role})
	for _, body := range []string{`{"devices":[{"device":"drive"}]}`, `{"devices":[{"device":"keyboard"},{"device":"drive"}]}`} {
		w := k.do(sh, "POST", "/api/device/start", body)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "not_permitted") {
			t.Errorf("%s: got %d %s, want 403 not_permitted", body, w.Code, w.Body.String())
		}
	}
	// A Viewer has no input capability at all.
	viewer := k.share(guestauth.RoleViewer, false)
	k.mgr.Sessions.Hello(&guestauth.Principal{ShareID: viewer.ID, Email: viewer.Email, Role: viewer.Role})
	if w := k.do(viewer, "POST", "/api/device/start", `{"devices":[{"device":"keyboard"}]}`); w.Code != http.StatusForbidden {
		t.Errorf("viewer starting a keyboard: got %d", w.Code)
	}
}
