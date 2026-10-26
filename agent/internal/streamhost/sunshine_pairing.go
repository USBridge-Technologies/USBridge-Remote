package streamhost

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// sunshineAdminHTTPClient is shared across every ListClients/SubmitPIN/
// UnpairClient call instead of a fresh `&http.Client{...}` per call -- see
// rustshineAdminHTTPClient's doc comment (rustshine_codec.go) for why a
// throwaway Client/Transport per call leaks a persistent connection instead
// of reusing one (confirmed live: exhausted gamestream-server's 1024 fd
// limit via the identical pattern in the rustshine backend's sibling
// functions). Needs its own InsecureSkipVerify TLS config -- unlike
// serverinfoHTTPClient (sunshine_codec.go), which hits the plain-HTTP
// NvHTTP port, these hit Sunshine's self-signed HTTPS admin API.
var sunshineAdminHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	},
}

// readErrBody reads (and truncates) a failed admin-API response body so
// error messages carry Sunshine's actual explanation (e.g. "Missing CSRF
// token", "PIN must be between 0000 and 9999") instead of a bare status
// code. Without this we were logging "sunshine returned HTTP 400" with no
// way to tell which of savePin's half-dozen 400 paths actually fired --
// see the itsme228/Sunshine fork's confighttp.cpp::savePin/
// validate_csrf_token for the exact set of causes.
func readErrBody(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := string(b)
	if msg == "" {
		return resp.Status
	}
	return msg
}

// fetchCSRFToken requests a short-lived CSRF token from Sunshine's admin API,
// required by POST/DELETE/PATCH admin endpoints (savePin, unpair, ...) since
// the itsme228/Sunshine fork added CSRF protection to confighttp.cpp.
// validate_csrf_token there only *enforces* the token when the request
// carries an Origin or Referer header naming a scheme+host not present in
// Sunshine's csrf_allowed_origins config; a header-less request (which is
// what Go's http.Client sends here) is exempt. Attaching a valid token
// anyway removes that exemption as a load-bearing assumption -- if a proxy,
// VPN client, or a future Go release ever adds either header transparently,
// the PIN relay silently starts failing with HTTP 400 again otherwise.
//
// Returns ("", nil) -- not an error -- when the endpoint doesn't exist
// (older/stock Sunshine predating CSRF protection): callers should proceed
// without the header rather than treat that as fatal.
func fetchCSRFToken(adminPort int, user, pass string) (string, error) {
	url := fmt.Sprintf("https://%s:%d/api/csrf-token", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(user, pass)
	resp, err := sunshineAdminHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Sunshine unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("csrf-token request returned HTTP %d: %s", resp.StatusCode, readErrBody(resp))
	}
	var result struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.CSRFToken, nil
}

// fetchSunshineSessionStatus reports whether the itsme228/Sunshine fork
// running on adminPort has an active Moonlight streaming session, read from
// its /api/session-status admin route (confighttp.cpp::getSessionStatus,
// backed directly by rtsp_stream::session_count()) rather than grepped from
// this process's log -- see sunshineBackend.SessionActive's doc comment for
// why that log scrape is unreliable on a long-running session. ok is false
// (not an error) both when the endpoint doesn't exist yet (an
// already-staged Sunshine build that predates this route -- same
// "treat 404 as absent, not fatal" contract fetchCSRFToken established) and
// on any other failure to reach it; callers should fall back to the log
// scrape in either case rather than guessing.
func fetchSunshineSessionStatus(adminPort int, user, pass string) (active bool, ok bool) {
	url := fmt.Sprintf("https://%s:%d/api/session-status", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, false
	}
	req.SetBasicAuth(user, pass)
	resp, err := sunshineAdminHTTPClient.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, false
	}
	var result struct {
		SessionActive bool `json:"session_active"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, false
	}
	return result.SessionActive, true
}

// ListClients returns the Moonlight clients currently paired with the
// Sunshine instance running on adminPort. Requires valid admin credentials
// to have been bootstrapped first.
func (b *sunshineBackend) ListClients(adminPort int) ([]Client, error) {
	url := fmt.Sprintf("https://%s:%d/api/clients/list", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(b.AdminUser(), b.adminPass())
	resp, err := sunshineAdminHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sunshine returned HTTP %d: %s", resp.StatusCode, readErrBody(resp))
	}
	var result struct {
		NamedCerts []Client `json:"named_certs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.NamedCerts, nil
}

// SubmitPIN sends a Moonlight pairing PIN to Sunshine's admin API on adminPort.
// The PIN is the 4-digit code shown by the Moonlight client during pairing.
func (b *sunshineBackend) SubmitPIN(adminPort int, pin string) error {
	user, pass := b.AdminUser(), b.adminPass()
	token, err := fetchCSRFToken(adminPort, user, pass)
	if err != nil {
		// Non-fatal: fall through and try without the header. Sunshine's own
		// CSRF check only enforces the token for requests carrying an
		// Origin/Referer header, which this client never sends -- see
		// fetchCSRFToken's doc comment.
		log.Printf("[sunshine] csrf-token fetch failed, submitting PIN without one: %v", err)
	}

	pending, err := pendingPairing(adminPort, user, pass)
	if err != nil {
		return err
	}
	// Sunshine 2026.9+ rejects a PIN without a client name (1-128 bytes);
	// use the device name Moonlight sent with its pairing request, as the
	// web UI pre-fills it.
	fields := map[string]string{"pin": pin, "name": pending.Name}
	if fields["name"] == "" {
		fields["name"] = defaultPairingClientName
	}
	if pending.ID != "" {
		fields["pairing_id"] = pending.ID
	}
	body, _ := json.Marshal(fields)
	url := fmt.Sprintf("https://%s:%d/api/pin", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-CSRF-Token", token)
	}
	req.SetBasicAuth(user, pass)
	resp, err := sunshinePinHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("Sunshine unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sunshine returned HTTP %d: %s", resp.StatusCode, readErrBody(resp))
	}
	// A wrong PIN or a handshake that didn't finish is still HTTP 200, with
	// {"status": false}.
	var result struct {
		Status *bool `json:"status"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) == nil && result.Status != nil && !*result.Status {
		return fmt.Errorf("sunshine rejected the PIN (wrong PIN, or the client's pairing handshake did not finish)")
	}
	return nil
}

// defaultPairingClientName names a paired client whose pairing request
// carried no device name.
const defaultPairingClientName = "USBridge Client"

// sunshinePinHTTPClient is sunshineAdminHTTPClient with room for POST
// /api/pin, which in Sunshine 2026.9+ only answers once the client has
// finished the whole pairing handshake (up to Sunshine's ping_timeout).
var sunshinePinHTTPClient = &http.Client{
	Timeout:   30 * time.Second,
	Transport: sunshineAdminHTTPClient.Transport,
}

// pendingPairingWait bounds how long SubmitPIN waits for Sunshine to list
// the pairing request the PIN belongs to: the client relays the PIN at
// about the same moment Moonlight's own /pair request reaches Sunshine, so
// the request can show up a moment after the PIN does.
var pendingPairingWait = 5 * time.Second

// pairingRequest is one entry of GET /api/pin's "pairings" list.
type pairingRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// pendingPairing returns the pairing request newer Sunshine builds require
// the PIN to name (they keep several pending and reject a PIN without its
// "pairing_id": "pairing_id must contain exactly 32 hexadecimal
// characters"), read from GET /api/pin the same way Sunshine's own web UI
// does. The list is oldest first, so with several pending the last one is
// the request the just-relayed PIN belongs to. Returns a zero value for
// older builds, which have no GET /api/pin and take the PIN alone.
func pendingPairing(adminPort int, user, pass string) (pairingRequest, error) {
	url := fmt.Sprintf("https://%s:%d/api/pin", adminHost(), adminPort)
	deadline := time.Now().Add(pendingPairingWait)
	for {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return pairingRequest{}, err
		}
		req.SetBasicAuth(user, pass)
		resp, err := sunshineAdminHTTPClient.Do(req)
		if err != nil {
			return pairingRequest{}, fmt.Errorf("Sunshine unreachable: %w", err)
		}
		var result struct {
			Pairings []pairingRequest `json:"pairings"`
		}
		ok := resp.StatusCode == http.StatusOK
		if ok {
			ok = json.NewDecoder(resp.Body).Decode(&result) == nil
		}
		resp.Body.Close()
		if !ok {
			// Older Sunshine: no pending-pairing list, PIN goes alone.
			return pairingRequest{}, nil
		}
		if n := len(result.Pairings); n > 0 {
			return result.Pairings[n-1], nil
		}
		if time.Now().After(deadline) {
			return pairingRequest{}, fmt.Errorf("sunshine has no pending pairing request for this PIN")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// UnpairClient removes the Moonlight client with the given uniqueID from
// Sunshine's authorized client list via the admin API on adminPort.
func (b *sunshineBackend) UnpairClient(adminPort int, uniqueID string) error {
	user, pass := b.AdminUser(), b.adminPass()
	token, err := fetchCSRFToken(adminPort, user, pass)
	if err != nil {
		log.Printf("[sunshine] csrf-token fetch failed, submitting unpair without one: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"uuid": uniqueID})
	url := fmt.Sprintf("https://%s:%d/api/clients/unpair", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-CSRF-Token", token)
	}
	req.SetBasicAuth(user, pass)
	resp, err := sunshineAdminHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unpair failed: %s: %s", resp.Status, readErrBody(resp))
	}
	return nil
}

// Moonlight client permissions of the Sunshine fork (crypto::PERM in its
// crypto.h): the groups a paired client may use.
const (
	PermInputController uint32 = 1 << 8
	PermInputTouch      uint32 = 1 << 9
	PermInputPen        uint32 = 1 << 10
	PermInputMouse      uint32 = 1 << 11
	PermInputKbd        uint32 = 1 << 12
	PermClipboardSet    uint32 = 1 << 16
	PermClipboardRead   uint32 = 1 << 17
	PermFileUpload      uint32 = 1 << 18
	PermFileDownload    uint32 = 1 << 19
	PermServerCmd       uint32 = 1 << 20
	PermList            uint32 = 1 << 24
	PermView            uint32 = 1 << 25
	PermLaunch          uint32 = 1 << 26
)

// ClientPermissioner is implemented by a stream host that can limit what a
// paired Moonlight client does (the Sunshine fork). Hosts without it trust a
// paired client fully, so a guest role that must not have input cannot be given
// a Moonlight client there.
type ClientPermissioner interface {
	// SetClientPermissions limits the client with this certificate fingerprint
	// to perm (the Perm* bits).
	SetClientPermissions(adminPort int, stateDir, fingerprint string, perm uint32) error
}

// SetClientPermissions sets the permission mask of a paired client through the
// admin API (/api/clients/update). The client is found by the fingerprint of its
// certificate in the host's own trust file.
func (b *sunshineBackend) SetClientPermissions(adminPort int, stateDir, fingerprint string, perm uint32) error {
	_, clients, _ := readSunshineTrusted(stateDir)
	uuid, name := "", ""
	for _, c := range clients {
		if c.Fingerprint != fingerprint {
			continue
		}
		uuid, name = c.HostUUID, c.Name
		if uuid == "" {
			uuid = c.UniqueID
		}
	}
	if uuid == "" {
		return fmt.Errorf("client %.8s is not in the host's trust list", fingerprint)
	}
	user, pass := b.AdminUser(), b.adminPass()
	token, err := fetchCSRFToken(adminPort, user, pass)
	if err != nil {
		log.Printf("[sunshine] csrf-token fetch failed, submitting permissions without one: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"uuid": uuid, "name": name, "perm": perm})
	url := fmt.Sprintf("https://%s:%d/api/clients/update", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-CSRF-Token", token)
	}
	req.SetBasicAuth(user, pass)
	resp, err := sunshineAdminHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("client update failed: %s: %s", resp.Status, readErrBody(resp))
	}
	return nil
}
