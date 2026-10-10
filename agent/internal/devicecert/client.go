// Package devicecert talks to the usbridge-entitlement backend's
// per-device dynamic-DNS + per-device TLS scheme (see
// usbridge-entitlement-backend's README "Per-device dynamic DNS + per-device
// TLS" section, and this repo's client/web mixed-content problem it
// solves): a browser served from https://web.usbridge.io can't
// fetch()/WebSocket to this agent's plain-HTTP or self-signed-HTTPS local
// listener at all (mixed content / untrusted-cert rejection, neither of
// which has a click-through for a background fetch the way top-level
// navigation does). This package gets the agent a real, browser-trusted
// hostname (`<label>.device.usbridge.io`) and a per-device
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
	"encoding/base64"
	"encoding/json"
	"errors"
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

// Cert is the leaf certificate the backend signed for this device's own
// CSR (see tlshost.Manager.DeviceCSR) -- unlike the shared-wildcard design
// this replaced, there is no key here: the private half never left this
// machine, so there is nothing this struct could carry even if it wanted
// to. See usbridge-entitlement-backend's 2026-09-30 security review for why
// (the old GET /v1/device/cert handed back a private key shared by the
// whole fleet, gated by nothing stronger than "hw_id looks well-formed").
type Cert struct {
	CertPEM  string `json:"cert"`
	Hostname string `json:"hostname"`
	NotAfter string `json:"not_after"` // RFC 3339
}

// ErrRateLimited is returned by RequestCert when the backend reports that
// Let's Encrypt itself rate-limited the order (HTTP 503,
// {"error":"rate_limited"}) -- distinct from every other failure so a
// caller (tickDeviceCert, ultimately the Status UI's retry button) can show
// "try again later, this is expected under load" instead of a generic
// error.
var ErrRateLimited = errors.New("devicecert: certificate issuance is rate-limited, try again later")

// ErrPending is returned by RequestCert when the backend has queued this
// device's CSR for its external issuer (HTTP 202, {"status":"pending"}) --
// the normal first-issuance/renewal state, not a failure: the cert shows up
// on a later poll, typically within a few minutes.
var ErrPending = errors.New("devicecert: certificate is being issued, check back in a few minutes")

// ErrAccountRequired: the backend issues a desktop device's certificate only
// to a signed-in USBridge account (HTTP 401 account_required) -- or there's
// no account token to send at all.
var ErrAccountRequired = errors.New("devicecert: sign in to your USBridge account to get a trusted certificate")

// RequestCert asks the backend to issue (or return the cached, still-fresh)
// leaf certificate for this device's own hostname, signing csrDER -- a
// DER-encoded PKCS#10 CSR generated locally (see
// tlshost.Manager.DeviceCSR). The backend independently recomputes the
// hostname from hwID and rejects a CSR that doesn't name it, so csrDER must
// already be built for the hostname RegisterIP returned.
func RequestCert(ctx context.Context, hwID string, csrDER []byte, accountToken string) (*Cert, error) {
	if accountToken == "" {
		return nil, ErrAccountRequired
	}
	reqBody, _ := json.Marshal(map[string]string{
		"hw_id": hwID,
		"csr":   base64.StdEncoding.EncodeToString(csrDER),
	})
	var out Cert
	if err := doJSONAuth(ctx, http.MethodPost, "/v1/device/cert", reqBody, accountToken, &out); err != nil {
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
	return doJSONAuth(ctx, method, path, body, "", out)
}

func doJSONAuth(ctx context.Context, method, path string, body []byte, bearer string, out any) error {
	req, err := newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
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
	if resp.StatusCode == http.StatusAccepted {
		var p struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(respBody, &p) == nil && p.Detail != "" {
			return fmt.Errorf("%w (%s)", ErrPending, p.Detail)
		}
		return ErrPending
	}
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error == "rate_limited" {
			return fmt.Errorf("%w: %s", ErrRateLimited, apiErr.Detail)
		}
		if apiErr.Error == "account_required" {
			return ErrAccountRequired
		}
		if apiErr.Error == "device_owned_by_other_account" || apiErr.Error == "account_device_limit" {
			return fmt.Errorf("devicecert: %s", apiErr.Detail)
		}
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
