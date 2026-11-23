package streamhost

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// clientEntry mirrors an entry of gamestream-server's GET /api/clients/list.
// Current servers answer {"named": [{"uniqueid","uuid","name"}]} (Sunshine-like
// envelope, name = cert CN); older ones answered a bare array of
// {"uniqueid"} — ListClients accepts both.
type clientEntry struct {
	UniqueID string `json:"uniqueid"`
	Name     string `json:"name"`
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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gamestream-server returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Named []clientEntry `json:"named"`
	}
	var entries []clientEntry
	if err := json.Unmarshal(raw, &envelope); err == nil {
		entries = envelope.Named
	} else if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	out := make([]Client, 0, len(entries))
	for _, e := range entries {
		out = append(out, Client{Name: e.Name, UniqueID: e.UniqueID})
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
