package streamhost

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
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
	body, _ := json.Marshal(map[string]string{"pin": pin})
	url := fmt.Sprintf("https://%s:%d/api/pin", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(b.AdminUser(), b.adminPass())
	resp, err := sunshineAdminHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("Sunshine unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sunshine returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// UnpairClient removes the Moonlight client with the given uniqueID from
// Sunshine's authorized client list via the admin API on adminPort.
func (b *sunshineBackend) UnpairClient(adminPort int, uniqueID string) error {
	body, _ := json.Marshal(map[string]string{"uuid": uniqueID})
	url := fmt.Sprintf("https://%s:%d/api/clients/unpair", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(b.AdminUser(), b.adminPass())
	resp, err := sunshineAdminHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unpair failed: %s", resp.Status)
	}
	return nil
}
