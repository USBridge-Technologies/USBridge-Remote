package guestauth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

type fixture struct {
	t       *testing.T
	store   *Store
	auth    *Authenticator
	backend ed25519.PrivateKey
	guest   *ecdsa.PrivateKey
	now     time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	f := &fixture{t: t, store: st, backend: priv, guest: g, now: time.Unix(1_800_000_000, 0)}
	f.auth = NewAuthenticator(st, pub, func() string { return "hw-1" })
	f.auth.now = func() time.Time { return f.now }
	return f
}

func (f *fixture) gpk() string {
	raw := append([]byte{4}, append(f.guest.X.FillBytes(make([]byte, 32)), f.guest.Y.FillBytes(make([]byte, 32))...)...)
	return base64.StdEncoding.EncodeToString(raw)
}

func (f *fixture) ticket(c Claims) string {
	raw, _ := json.Marshal(c)
	p := base64.RawURLEncoding.EncodeToString(raw)
	sig := ed25519.Sign(f.backend, []byte(TicketPrefix+"."+p))
	return TicketPrefix + "." + p + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *fixture) claims(sh *Share) Claims {
	return Claims{V: 1, SID: sh.ID, HW: "hw-1", Em: sh.Email, Role: sh.Role, GPK: f.gpk(), Iat: f.now.Unix(), Exp: f.now.Add(10 * time.Minute).Unix()}
}

func (f *fixture) sign(method, uri string, body []byte) (ts, sig string) {
	ts = strconv.FormatInt(f.now.Unix(), 10)
	d := sha256.Sum256([]byte(method + uri + ts + string(body)))
	r, s, _ := ecdsa.Sign(rand.Reader, f.guest, d[:])
	raw := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	return ts, base64.StdEncoding.EncodeToString(raw)
}

func (f *fixture) share(role Role) *Share {
	sh, err := f.store.Add("Anna@Corp.example", role, 0, true, f.now)
	if err != nil {
		f.t.Fatal(err)
	}
	return sh
}

func TestVerifyAcceptsValidGuest(t *testing.T) {
	f := newFixture(t)
	sh := f.share(RoleOperator)
	ts, sig := f.sign("POST", "/api/mouse", []byte(`{"x":1}`))
	p, err := f.auth.Verify("POST", "/api/mouse", ts, sig, f.ticket(f.claims(sh)), []byte(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Role != RoleOperator || p.Email != "anna@corp.example" || !p.RequireConsent {
		t.Fatalf("principal: %+v", p)
	}
}

func TestVerifyRejects(t *testing.T) {
	f := newFixture(t)
	sh := f.share(RoleViewer)
	good := f.ticket(f.claims(sh))
	ts, sig := f.sign("GET", "/api/status", nil)

	cases := map[string]func() error{
		"no ticket": func() error { _, e := f.auth.Verify("GET", "/api/status", ts, sig, "", nil); return e },
		"tampered request": func() error {
			_, e := f.auth.Verify("GET", "/api/other", ts, sig, good, nil)
			return e
		},
		"body changed": func() error { _, e := f.auth.Verify("GET", "/api/status", ts, sig, good, []byte("x")); return e },
		"foreign backend key": func() error {
			other := newFixture(t)
			other.guest = f.guest
			_, e := f.auth.Verify("GET", "/api/status", ts, sig, other.ticket(other.claims(sh)), nil)
			return e
		},
		"other machine": func() error {
			c := f.claims(sh)
			c.HW = "hw-2"
			_, e := f.auth.Verify("GET", "/api/status", ts, sig, f.ticket(c), nil)
			return e
		},
		"expired ticket": func() error {
			c := f.claims(sh)
			c.Exp = f.now.Unix() - 1
			_, e := f.auth.Verify("GET", "/api/status", ts, sig, f.ticket(c), nil)
			return e
		},
		"unknown share": func() error {
			c := f.claims(sh)
			c.SID = "nope"
			_, e := f.auth.Verify("GET", "/api/status", ts, sig, f.ticket(c), nil)
			return e
		},
		"email mismatch": func() error {
			c := f.claims(sh)
			c.Em = "eve@evil.example"
			_, e := f.auth.Verify("GET", "/api/status", ts, sig, f.ticket(c), nil)
			return e
		},
		"stale timestamp": func() error {
			f.now = f.now.Add(2 * time.Minute)
			defer func() { f.now = f.now.Add(-2 * time.Minute) }()
			c := f.claims(sh)
			_, e := f.auth.Verify("GET", "/api/status", ts, sig, f.ticket(c), nil)
			return e
		},
	}
	for name, run := range cases {
		if err := run(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStolenTicketUselessWithoutGuestKey(t *testing.T) {
	f := newFixture(t)
	sh := f.share(RoleTechnician)
	tk := f.ticket(f.claims(sh))
	thief, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	mine := f.guest
	f.guest = thief
	ts, sig := f.sign("GET", "/api/status", nil)
	f.guest = mine
	if _, err := f.auth.Verify("GET", "/api/status", ts, sig, tk, nil); err == nil {
		t.Fatal("ticket accepted with a signature from another key")
	}
}

func TestRevokeExpireAndReplay(t *testing.T) {
	f := newFixture(t)
	sh := f.share(RoleOperator)
	tk := f.ticket(f.claims(sh))
	ts, sig := f.sign("GET", "/api/status", nil)
	if _, err := f.auth.Verify("GET", "/api/status", ts, sig, tk, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.auth.Verify("GET", "/api/status", ts, sig, tk, nil); err != ErrReplay {
		t.Fatalf("replay: %v", err)
	}
	ts2, sig2 := f.sign("GET", "/api/status", nil)
	if err := f.store.Revoke(sh.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.auth.Verify("GET", "/api/status", ts2, sig2, tk, nil); err != ErrRevoked {
		t.Fatalf("revoked: %v", err)
	}

	f2 := newFixture(t)
	exp, _ := f2.store.Add("a@b.example", RoleViewer, f2.now.Add(time.Hour).Unix(), false, f2.now)
	tk2 := f2.ticket(f2.claims(exp))
	f2.now = f2.now.Add(2 * time.Hour)
	c := f2.claims(exp) // fresh ticket after the share ended
	ts3, sig3 := f2.sign("GET", "/x", nil)
	if _, err := f2.auth.Verify("GET", "/x", ts3, sig3, f2.ticket(c), nil); err != ErrRevoked {
		t.Fatalf("expired share: %v", err)
	}
	_ = tk2
}

func TestStorePersistsAndValidates(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir)
	now := time.Unix(1_800_000_000, 0)
	if _, err := s.Add("not-an-email", RoleViewer, 0, true, now); err == nil {
		t.Fatal("bad email accepted")
	}
	if _, err := s.Add("a@b.example", Role("root"), 0, true, now); err == nil {
		t.Fatal("bad role accepted")
	}
	if _, err := s.Add("a@b.example", RoleViewer, now.Unix()-1, true, now); err == nil {
		t.Fatal("past expiry accepted")
	}
	sh, _ := s.Add("a@b.example", RoleViewer, 0, true, now)
	s2, _ := OpenStore(dir)
	if s2.Get(sh.ID) == nil || s2.LiveCount(now) != 1 {
		t.Fatal("share lost on reload")
	}
}

func TestRoleCaps(t *testing.T) {
	if RoleViewer.Has(CapInput) || !RoleOperator.Has(CapInput|CapClipboard) || RoleOperator.Has(CapUSB) || !RoleTechnician.Has(CapUSB|CapDisks) {
		t.Fatal("role capability table wrong")
	}
	if Role("x").Has(CapView) || RoleTechnician.Has(0) {
		t.Fatal("unknown role / empty need must grant nothing")
	}
}
