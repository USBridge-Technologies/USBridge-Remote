package guestclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestJoinAndSign(t *testing.T) {
	var tickets atomic.Int32
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.URL.Path != "/v1/shares/abc/ticket" {
			w.WriteHeader(404)
			return
		}
		var b struct {
			GuestPubkey string `json:"guest_pubkey"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		gotKey = b.GuestPubkey
		tickets.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"ticket": "usbgst1.t.s", "expires_in": 600, "hw_id": "hw-9", "device_name": "Office PC", "role": "operator", "require_consent": true})
	}))
	defer srv.Close()

	s, err := Join(context.Background(), srv.URL, func() string { return "tok" }, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if s.HwID != "hw-9" || s.Role != "operator" || !s.RequireConsent || s.DeviceName != "Office PC" {
		t.Fatalf("session: %+v", s)
	}
	h, err := s.Headers(context.Background(), "POST", "/api/mouse", []byte(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if h["X-Guest-Ticket"] != "usbgst1.t.s" || tickets.Load() != 1 {
		t.Fatalf("headers %v tickets=%d", h, tickets.Load())
	}

	// The agent's check, done independently: the signature verifies against the key sent to the backend.
	raw, _ := base64.StdEncoding.DecodeString(gotKey)
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(raw[1:33]), Y: new(big.Int).SetBytes(raw[33:])}
	sig, _ := base64.StdEncoding.DecodeString(h["X-Auth-Signature"])
	d := sha256.Sum256([]byte("POST/api/mouse" + h["X-Auth-Timestamp"] + `{"x":1}`))
	if len(sig) != 64 || !ecdsa.Verify(pub, d[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("signature does not verify against the registered key")
	}
}

func TestJoinErrors(t *testing.T) {
	for status, want := range map[int]error{401: ErrNeedSignIn, 404: ErrNotInvited, 410: ErrShareEnded, 429: ErrRateLimited} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		_, err := Join(context.Background(), srv.URL, func() string { return "tok" }, "abc")
		srv.Close()
		if err != want {
			t.Errorf("%d: got %v want %v", status, err, want)
		}
	}
	if _, err := Join(context.Background(), "http://127.0.0.1:1", func() string { return "" }, "abc"); err != ErrNeedSignIn {
		t.Errorf("no token: %v", err)
	}
}

func TestTicketRenewedBeforeExpiry(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"ticket": "t", "expires_in": 60, "hw_id": "h", "role": "viewer"}) // under renewBefore
	}))
	defer srv.Close()
	s, err := Join(context.Background(), srv.URL, func() string { return "tok" }, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Headers(context.Background(), "GET", "/x", nil); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Fatalf("expected a renewal, tickets fetched = %d", n.Load())
	}
}
