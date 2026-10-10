package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"usbridge-client/internal/models"
)

// KVM network settings and event log (usbridge web/network_api.go).

// apiData decodes a {success, data} reply into out.
func apiData(resp []byte, err error, what string, out interface{}) error {
	if err != nil {
		return apiErr(err)
	}
	var r struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Error   string          `json:"error"`
		Details string          `json:"details"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return fmt.Errorf("%s: bad reply: %v", what, err)
	}
	if !r.Success {
		return fmt.Errorf("%s: %s", what, firstNonEmpty(r.Details, r.Error, r.Message))
	}
	if out != nil && len(r.Data) > 0 && string(r.Data) != "null" {
		if err := json.Unmarshal(r.Data, out); err != nil {
			return fmt.Errorf("%s: bad reply: %v", what, err)
		}
	}
	return nil
}

// apiErr turns "HTTP error 400: {json}" into the KVM's own message, or
// "this KVM's firmware doesn't have it" for a 404 (older firmware).
func apiErr(err error) error {
	msg := err.Error()
	if !strings.HasPrefix(msg, "HTTP error ") {
		return err
	}
	if strings.HasPrefix(msg, "HTTP error 404:") && !strings.Contains(msg, `"success"`) {
		return fmt.Errorf("not supported by this KVM's firmware: update it first")
	}
	if i := strings.Index(msg, "{"); i >= 0 {
		var r struct {
			Error   string `json:"error"`
			Details string `json:"details"`
		}
		if json.Unmarshal([]byte(msg[i:]), &r) == nil && (r.Error != "" || r.Details != "") {
			if r.Details != "" && r.Error != "" {
				return fmt.Errorf("%s: %s", r.Error, r.Details)
			}
			return fmt.Errorf("%s", firstNonEmpty(r.Details, r.Error))
		}
	}
	return err
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// GetNetworkStatus returns the KVM's Ethernet and Wi-Fi state.
func (c *USBClient) GetNetworkStatus() (*models.NetworkStatus, error) {
	var st models.NetworkStatus
	resp, err := c.makeRequest("GET", "/api/network/status", nil)
	if err := apiData(resp, err, "network status", &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// SetEthernetIP sets the Ethernet port to DHCP or a static address.
func (c *USBClient) SetEthernetIP(cfg models.NetIPConfig) error {
	body, _ := json.Marshal(cfg)
	resp, err := c.makeRequest("POST", "/api/network/ethernet", body)
	return apiData(resp, err, "Ethernet", nil)
}

// SetWiFiIP sets the Wi-Fi address to DHCP or a static one.
func (c *USBClient) SetWiFiIP(cfg models.NetIPConfig) error {
	body, _ := json.Marshal(cfg)
	resp, err := c.makeRequest("POST", "/api/network/wifi/ip", body)
	return apiData(resp, err, "Wi-Fi", nil)
}

// ScanWiFi scans for Wi-Fi networks (takes up to ~20 s).
func (c *USBClient) ScanWiFi() ([]models.WiFiNetwork, error) {
	var nets []models.WiFiNetwork
	resp, err := c.PostRawWithTimeout("/api/network/wifi/scan", []byte("{}"), 40*time.Second)
	if err := apiData(resp, err, "Wi-Fi scan", &nets); err != nil {
		return nil, err
	}
	return nets, nil
}

// ConnectWiFi starts connecting to a network; GetNetworkStatus shows how
// it goes.
func (c *USBClient) ConnectWiFi(ssid, password string) error {
	body, _ := json.Marshal(map[string]string{"ssid": ssid, "password": password})
	resp, err := c.makeRequest("POST", "/api/network/wifi/connect", body)
	return apiData(resp, err, "Wi-Fi", nil)
}

// DisconnectWiFi disconnects, and with forget drops the saved network.
func (c *USBClient) DisconnectWiFi(forget bool) error {
	body, _ := json.Marshal(map[string]bool{"forget": forget})
	resp, err := c.makeRequest("POST", "/api/network/wifi/disconnect", body)
	return apiData(resp, err, "Wi-Fi", nil)
}

// GetKVMEvents returns the KVM's event log, newest first.
func (c *USBClient) GetKVMEvents(limit int) ([]models.KVMEvent, error) {
	var ev []models.KVMEvent
	resp, err := c.makeRequest("GET", fmt.Sprintf("/api/events?limit=%d", limit), nil)
	if err := apiData(resp, err, "event log", &ev); err != nil {
		return nil, err
	}
	return ev, nil
}
