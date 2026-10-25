package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func guestTestClient(t *testing.T, h http.HandlerFunc) *USBClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewUSBClientWithScheme("http", "127.0.0.1", 0, 5, nil)
	c.baseURL = srv.URL
	c.SetAPISecretV2([]byte("k"))
	return c
}

func TestInviteGuestMapsAgentRefusals(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{402, `{"success":false,"message":"free_limit"}`, ErrGuestFreeLimit},
		{503, `{"success":false,"message":"cloud_unavailable"}`, ErrGuestCloudDown},
		{429, `{"success":false,"message":"rate_limit"}`, ErrGuestRateLimit},
		{404, `404 page not found`, ErrGuestUnsupported},
	}
	for _, tc := range cases {
		c := guestTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		})
		if _, _, err := c.InviteGuest(GuestInvite{Email: "a@b.example", Role: "viewer"}); !errors.Is(err, tc.want) {
			t.Errorf("%d: got %v, want %v", tc.status, err, tc.want)
		}
	}
}

func TestListAndInviteGuest(t *testing.T) {
	c := guestTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Signature") == "" {
			w.WriteHeader(401)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/guests":
			_, _ = w.Write([]byte(`{"success":true,"data":{"guests":[{"id":"x","email":"a@b.example","role":"operator","live":true,"status":"accepted"}],"live":1,"free_limit":1,"paid":false}}`))
		case "POST /api/guests":
			_, _ = w.Write([]byte(`{"success":true,"data":{"invite_url":"https://web.usbridge.io/#/join/x","mail_sent":true}}`))
		default:
			w.WriteHeader(404)
		}
	})
	l, err := c.ListGuests()
	if err != nil || len(l.Guests) != 1 || l.Guests[0].Role != "operator" || l.Live != 1 || l.Paid {
		t.Fatalf("list: %+v %v", l, err)
	}
	u, sent, err := c.InviteGuest(GuestInvite{Email: "a@b.example", Role: "operator", RequireConsent: true})
	if err != nil || !sent || u == "" {
		t.Fatalf("invite: %q %v %v", u, sent, err)
	}
}
