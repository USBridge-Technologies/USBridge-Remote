package streamhost

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// clientEntry mirrors gamestream-server's confirmed GET /api/clients/list
// response shape: a JSON array of {"uniqueid": "<string>"} objects — no
// "name" field (unlike Sunshine's named_certs), so Client.Name is always ""
// for this backend.
type clientEntry struct {
	UniqueID string `json:"uniqueid"`
}

func (b *rustshineBackend) ListClients(adminPort int) ([]Client, error) {
	url := fmt.Sprintf("https://%s:%d/api/clients/list", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(b.AdminUser(), b.AdminPass())
	// Shared, package-level client — see rustshineAdminHTTPClient's doc
	// comment (rustshine_codec.go) for why a fresh &http.Client{} per call
	// leaks a connection: this exact function, polled by the UI's 2s
	// refresh ticker, was the actual source of a real live fd exhaustion
	// ("Too many open files" on gamestream-server) before this fix.
	resp, err := rustshineAdminHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var entries []clientEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	out := make([]Client, 0, len(entries))
	for _, e := range entries {
		out = append(out, Client{UniqueID: e.UniqueID})
	}
	return out, nil
}

// SubmitPIN sends a Moonlight pairing PIN to gamestream-server's confirmed
// POST /api/pin route — same {"pin": "..."} body shape as Sunshine's.
func (b *rustshineBackend) SubmitPIN(adminPort int, pin string) error {
	body, _ := json.Marshal(map[string]string{"pin": pin})
	url := fmt.Sprintf("https://%s:%d/api/pin", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(b.AdminUser(), b.AdminPass())
	resp, err := rustshineAdminHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("gamestream-server unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gamestream-server returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// UnpairClient removes a paired client via the confirmed POST
// /api/clients/unpair route. Note the body field is "uniqueid" (no
// underscore) — different from Sunshine's "uuid".
func (b *rustshineBackend) UnpairClient(adminPort int, uniqueID string) error {
	body, _ := json.Marshal(map[string]string{"uniqueid": uniqueID})
	url := fmt.Sprintf("https://%s:%d/api/clients/unpair", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(b.AdminUser(), b.AdminPass())
	resp, err := rustshineAdminHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unpair failed: %s", resp.Status)
	}
	return nil
}
