package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	usbapi "usbridge-client/internal/api"
	"usbridge-client/internal/guestclient"
)

// guestConsentWait is how long a guest waits for the person at the computer.
const guestConsentWait = 100 * time.Second

// SetGuestSession makes this service connect as a guest of the computer: the
// agent is told who is asking with the guest's ticket instead of the master
// key, the person at the computer is asked to allow it, and pairing is bound
// to the share, so ending the share removes this client's certificate from
// the host. nil goes back to the owner's master key.
func (m *MoonlightService) SetGuestSession(s *guestclient.Session) {
	m.guest = s
}

// guestAPI is an agent client that authenticates as the guest.
func (m *MoonlightService) guestAPI() *usbapi.USBClient {
	host := m.serverHost
	if host == "" {
		host = "127.0.0.1"
	}
	port := m.config.USBPort
	if port == 0 {
		port = 8080
	}
	c := usbapi.NewUSBClientWithScheme("http", host, port, 10, nil)
	if m.tailscaleSvc != nil && isLikelyTailnetHost(host) {
		if hc, err := m.tailscaleSvc.HTTPClient(); err == nil {
			c = usbapi.NewUSBClientWithHTTPClient(host, port, 10, hc)
		}
	}
	g := m.guest
	c.SetGuestSigner(func(ctx context.Context, method, uri string, body []byte) (map[string]string, error) {
		return g.Headers(ctx, method, uri, body)
	})
	return c
}

// clientFingerprint is the hex SHA-256 of this client's Moonlight certificate
// (DER), the name the host's trust list knows it by.
func (m *MoonlightService) clientFingerprint() string {
	sum := sha256.Sum256(m.client.Identity.Cert.Raw)
	return hex.EncodeToString(sum[:])
}

// submitGuestPin has the agent enter the pairing PIN on the guest's behalf and
// trust this client's certificate for the share.
func (m *MoonlightService) submitGuestPin(c *usbapi.USBClient, pin string) error {
	body, _ := json.Marshal(map[string]string{"pin": pin, "fingerprint": m.clientFingerprint()})
	_, err := c.PostRaw("/api/guest/moonlight-pair", body)
	return err
}

// awaitGuestConsent asks the agent to start a guest session and waits until
// the person at the computer allows it.
func (m *MoonlightService) awaitGuestConsent(abort <-chan struct{}) error {
	api := m.guestAPI()
	deadline := time.Now().Add(guestConsentWait)
	for {
		raw, err := api.PostRaw("/api/guest/hello", []byte("{}"))
		if err != nil {
			return fmt.Errorf("guest access: %w", err)
		}
		var env struct {
			Data struct {
				State string `json:"state"`
			} `json:"data"`
		}
		_ = json.Unmarshal(raw, &env)
		switch strings.ToLower(env.Data.State) {
		case "allowed":
			return nil
		case "denied":
			return errors.New("the person at the computer declined the connection")
		}
		if time.Now().After(deadline) {
			return errors.New("the person at the computer did not answer")
		}
		if m.onStateChanged != nil {
			m.onStateChanged("waiting-for-host")
		}
		select {
		case <-abort:
			return errors.New("connect aborted by disconnect (waiting for the host)")
		case <-time.After(3 * time.Second):
		}
	}
}
