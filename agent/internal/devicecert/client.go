// Package devicecert talks to the usbridge-entitlement backend's
// per-device dynamic-DNS + shared wildcard TLS scheme (see
// usbridge-entitlement-backend's README "Per-device dynamic DNS + wildcard
// TLS" section, and this repo's client/web mixed-content problem it
// solves): a browser served from https://web.usbridge.io can't
// fetch()/WebSocket to this agent's plain-HTTP or self-signed-HTTPS local
// listener at all (mixed content / untrusted-cert rejection, neither of
// which has a click-through for a background fetch the way top-level
// navigation does). This package gets the agent a real, browser-trusted
// hostname (`<label>.device.usbridge.io`) and the shared wildcard
// certificate to present for it -- see internal/tlshost for what actually
// installs that cert into the agent's HTTPS listener.
//
// Identified by this machine's hwid.Get() value, the SAME identifier
// internal/entitlement already uses -- no separate registration step, no
// Mender/device-serial concept (that's usbridge_service's SBC firmware,
// not this desktop agent). See the backend's deviceDns.ts module doc
// comment for why a bare hw_id is already the right trust bar here: it's
// the entire trust anchor for the whole desktop entitlement system too.
package devicecert

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// backendBaseURL is the usbridge-entitlement Worker's deployed URL. A var,
// not a const, purely so tests can point it at a local httptest.Server --
// mirrors internal/entitlement/pubkey.go's identical reasoning. A separate
// copy rather than importing internal/entitlement's var: every package in
// this repo that talks to this backend keeps its own (see
// internal/entitlement, license/devicelicense in usbridge_service) so
// redirecting one package's tests at a fake server can never accidentally
// leak into another's.
var backendBaseURL = "https://usbridge-entitlement.fatkulinamir80.workers.dev"

// TestSetBackendBaseURL points every call in this package at url (typically
// a local httptest.Server) and returns the previous value so the caller can
// restore it.
func TestSetBackendBaseURL(url string) string {
	prev := backendBaseURL
	backendBaseURL = url
	return prev
}

// httpTimeout bounds every call here -- always invoked from a background
// watchdog (see internal/app's deviceCertWatchdog), never a path a user is
// blocked on, so a slow/unreachable backend just delays that tick.
const httpTimeout = 10 * time.Second

func httpClient() *http.Client { return &http.Client{Timeout: httpTimeout} }

// RegisterIP tells the backend this machine's current LAN ip, and returns
// the hostname (<label>.device.usbridge.io) it should now answer to over
// TLS. ip must be a private-use/link-local address (see
// internal/netutil.PreferredIPv4) -- the backend refuses anything else.
func RegisterIP(ctx context.Context, hwID, ip string) (hostname string, err error) {
	reqBody, _ := json.Marshal(map[string]string{"hw_id": hwID, "ip": ip})
	var raw struct {
		Hostname string `json:"hostname"`
	}
	if err := doJSON(ctx, http.MethodPost, "/v1/device/dns", reqBody, &raw); err != nil {
		return "", err
	}
	return raw.Hostname, nil
}

// Cert is the shared wildcard cert/key this machine's HTTPS listener
// should present for the hostname RegisterIP returned -- the SAME cert
// every other device/agent in the fleet gets (see the backend's README for
// why: one shared private key, not one per install, at the cost of the
// key existing on every install -- a deliberate, documented tradeoff, not
// an oversight).
type Cert struct {
	CertPEM        string `json:"cert"`
	KeyPEM         string `json:"key"`
	HostnameSuffix string `json:"hostname_suffix"`
	NotAfter       string `json:"not_after"` // RFC 3339
}

// FetchCert retrieves the current shared wildcard cert+key. Gated
// server-side only on hwID looking like a real hardware id (see this
// package's doc comment) -- never fails because a machine hasn't purchased
// anything.
func FetchCert(ctx context.Context, hwID string) (*Cert, error) {
	var out Cert
	if err := doJSON(ctx, http.MethodGet, "/v1/device/cert?hw_id="+hwID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, backendBaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "usbridge-agent-devicecert")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func doJSON(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("devicecert: request %s: %w", path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("devicecert: %s: HTTP %d: %s", path, resp.StatusCode, truncate(respBody))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("devicecert: parse response from %s: %w", path, err)
	}
	return nil
}

func truncate(b []byte) string {
	const max = 300
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
