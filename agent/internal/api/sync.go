package api

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"usbridge_agent/internal/sunshine"
)

// MasterSyncRequest is the outer envelope sent by the client — AES-GCM encrypted payload.
type MasterSyncRequest struct {
	Payload   string `json:"payload"`
	IV        string `json:"iv"`
	Timestamp int64  `json:"timestamp"`
}

// MasterSyncPayload is the decrypted inner payload from the client.
type MasterSyncPayload struct {
	MoonlightPIN      string `json:"moonlight_pin,omitempty"`
	TailscaleKey      string `json:"tailscale_key,omitempty"`
	TailscaleRegister bool   `json:"tailscale_register,omitempty"`
	Hostname          string `json:"hostname,omitempty"`
	ClientID          string `json:"client_id,omitempty"`
}

// TailscaleStatusInfo is embedded in the sync response.
type TailscaleStatusInfo struct {
	Running  bool   `json:"running"`
	LoggedIn bool   `json:"logged_in"`
	Backend  string `json:"backend"`
	DNSName  string `json:"dns_name,omitempty"`
	HostName string `json:"host_name,omitempty"`
	IP4      string `json:"ip4,omitempty"`
	AuthURL  string `json:"auth_url,omitempty"`
}

// MasterSyncResponse is returned by /api/auth/sync.
type MasterSyncResponse struct {
	TailscaleStatus *TailscaleStatusInfo `json:"tailscale_status,omitempty"`
	SunshineStatus  string               `json:"sunshine_status"`
}

// MoonlightPINRequest is sent to /api/moonlight/pin.
type MoonlightPINRequest struct {
	PIN string `json:"pin"`
}

// AuthQRResponse is returned by /api/auth/qr/link.
type AuthQRResponse struct {
	Link      string `json:"link"`
	MasterKey string `json:"master_key,omitempty"`
}

// decryptAESGCM decrypts a base64-encoded AES-GCM ciphertext using the given 32-byte key and base64-encoded IV.
func decryptAESGCM(ciphertextB64, ivB64 string, key []byte) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	iv, err := base64.StdEncoding.DecodeString(ivB64)
	if err != nil {
		return nil, fmt.Errorf("decode iv: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	plain, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm open: %w", err)
	}
	return plain, nil
}

// Sync implements POST /api/auth/sync.
// The request envelope is AES-GCM encrypted with SHA256(masterKey).
// No outer HMAC — replay protection via timestamp (±120s).
func (s *Server) Sync(w http.ResponseWriter, r *http.Request) {
	var env MasterSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		log.Printf("[api] sync: bad envelope: %v", err)
		s.fail(w, http.StatusBadRequest, "invalid_envelope", err)
		return
	}

	now := time.Now().Unix()
	if now-env.Timestamp > 120 || env.Timestamp-now > 120 {
		log.Printf("[api] sync: timestamp expired (skew=%ds)", now-env.Timestamp)
		s.fail(w, http.StatusUnauthorized, "timestamp_expired", nil)
		return
	}

	if len(s.masterKey) == 0 {
		s.fail(w, http.StatusInternalServerError, "master_key_not_configured", nil)
		return
	}

	aesKey := deriveKey(s.masterKey)
	plaintext, err := decryptAESGCM(env.Payload, env.IV, aesKey)
	if err != nil {
		log.Printf("[api] sync: decrypt failed: %v", err)
		s.fail(w, http.StatusUnauthorized, "decrypt_failed", err)
		return
	}

	var payload MasterSyncPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		log.Printf("[api] sync: decode payload: %v", err)
		s.fail(w, http.StatusBadRequest, "invalid_payload", err)
		return
	}

	log.Printf("[api] sync: client=%s hostname=%s moonlight_pin=%v tailscale_register=%v",
		payload.ClientID, payload.Hostname, payload.MoonlightPIN != "", payload.TailscaleRegister)

	sunshineStatus := "unknown"
	if strings.TrimSpace(payload.MoonlightPIN) != "" {
		if err := s.submitPinToSunshine(payload.MoonlightPIN); err != nil {
			log.Printf("[api] sync: sunshine pin relay failed: %v", err)
			sunshineStatus = "pin_relay_failed"
		} else {
			log.Printf("[api] sync: sunshine pin relay ok")
			sunshineStatus = "pin_accepted"
		}
	} else {
		sunshineStatus = "no_pin"
	}

	// Register / refresh Tailscale status.
	var tsStatus *TailscaleStatusInfo
	switch {
	case strings.TrimSpace(payload.TailscaleKey) != "":
		// Full auth-key registration (automated, no browser approval needed).
		log.Printf("[api] sync: registering tailscale with auth key")
		if status, err := s.app.RegisterTailscale(r.Context(), payload.TailscaleKey, payload.Hostname); err != nil {
			log.Printf("[api] sync: tailscale auth-key registration failed: %v", err)
		} else {
			tsStatus = status
		}
	case payload.TailscaleRegister:
		// Client requested registration without a key: trigger interactive login
		// so the client receives an AuthURL to open in a browser.
		log.Printf("[api] sync: triggering tailscale interactive registration")
		if status, err := s.app.RegisterTailscale(r.Context(), "", payload.Hostname); err != nil {
			log.Printf("[api] sync: tailscale interactive registration failed: %v", err)
		} else {
			tsStatus = status
		}
	default:
		tsStatus = s.app.TailscaleStatus()
	}

	resp := MasterSyncResponse{
		TailscaleStatus: tsStatus,
		SunshineStatus:  sunshineStatus,
	}
	s.ok(w, "sync_ok", resp)
}

// MoonlightPIN implements POST /api/moonlight/pin.
// This endpoint is intentionally public (no HMAC) — it is called during the Moonlight
// pairing handshake before the client has exchanged the master key.
func (s *Server) MoonlightPIN(w http.ResponseWriter, r *http.Request) {
	var req MoonlightPINRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[api] moonlight_pin: bad request: %v", err)
		s.fail(w, http.StatusBadRequest, "invalid_request", err)
		return
	}
	pin := strings.TrimSpace(req.PIN)
	if pin == "" {
		s.fail(w, http.StatusBadRequest, "missing_pin", nil)
		return
	}
	log.Printf("[api] moonlight_pin: relaying pin to sunshine")
	if err := s.submitPinToSunshine(pin); err != nil {
		log.Printf("[api] moonlight_pin: sunshine relay failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "sunshine_pin_failed", err)
		return
	}
	s.ok(w, "pin_accepted", nil)
}

// AuthQRLink implements GET /api/auth/qr/link.
// Returns the quick-connect link so a remote UI can display or scan it programmatically.
// This endpoint is public (no HMAC) — the response contains the master key, which is the
// shared secret itself; exposing it over local HTTP is equivalent to reading the QR code.
func (s *Server) AuthQRLink(w http.ResponseWriter, r *http.Request) {
	link := ""
	masterKey := ""
	if qr, ok := s.app.(interface{ QRLink() (string, string) }); ok {
		link, masterKey = qr.QRLink()
	}
	s.ok(w, "qr_link", AuthQRResponse{Link: link, MasterKey: masterKey})
}

// submitPinToSunshine forwards the Moonlight pairing PIN to Sunshine's local web API.
func (s *Server) submitPinToSunshine(pin string) error {
	port := s.sunshinePort
	if port == 0 {
		port = 47990
	}
	url := fmt.Sprintf("https://127.0.0.1:%d/api/pin", port)

	body, _ := json.Marshal(map[string]string{"pin": pin})

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Sunshine uses a self-signed cert on localhost
		},
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(sunshine.AdminUser, sunshine.AdminPass())

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sunshine returned HTTP %d", resp.StatusCode)
	}
	return nil
}
