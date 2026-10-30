package api

import (
	"encoding/json"

	"usbridge-client/internal/models"
)

// KVM power, update and SD card settings (usbridge web/settings_api.go).

func (c *USBClient) GetPowerSettings() (*models.PowerSettings, error) {
	var p models.PowerSettings
	resp, err := c.makeRequest("GET", "/api/settings/power", nil)
	if err := apiData(resp, err, "power settings", &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *USBClient) SetPowerSetting(key string, value int) (*models.PowerSettings, error) {
	body, _ := json.Marshal(map[string]interface{}{"key": key, "value": value})
	var p models.PowerSettings
	resp, err := c.makeRequest("POST", "/api/settings/power", body)
	if err := apiData(resp, err, "power settings", &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *USBClient) GetUpdateStatus() (*models.UpdateStatus, error) {
	var u models.UpdateStatus
	resp, err := c.makeRequest("GET", "/api/settings/update", nil)
	if err := apiData(resp, err, "updates", &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// CheckForUpdate makes the KVM look for (and install) an update now.
func (c *USBClient) CheckForUpdate() error {
	resp, err := c.makeRequest("POST", "/api/settings/update/check", []byte("{}"))
	return apiData(resp, err, "update check", nil)
}

func (c *USBClient) CommitUpdate() error {
	resp, err := c.makeRequest("POST", "/api/settings/update/commit", []byte("{}"))
	return apiData(resp, err, "update commit", nil)
}

// RebootKVM restarts the KVM itself (not the target PC).
func (c *USBClient) RebootKVM() error {
	resp, err := c.makeRequest("POST", "/api/settings/reboot", []byte("{}"))
	return apiData(resp, err, "reboot", nil)
}

func (c *USBClient) GetSDCardStatus() (*models.SDCardStatus, error) {
	var s models.SDCardStatus
	resp, err := c.makeRequest("GET", "/api/settings/sdcard", nil)
	if err := apiData(resp, err, "SD card", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// FormatSDCard formats a blank SD card as backup storage. The KVM refuses
// a card with btrfs on it (backups, snapshots).
func (c *USBClient) FormatSDCard() error {
	resp, err := c.makeRequest("POST", "/api/settings/sdcard/format", []byte(`{"confirm":"FORMAT"}`))
	return apiData(resp, err, "SD card", nil)
}

func (c *USBClient) SetSnapshotTiming(t models.SnapshotTiming) error {
	body, _ := json.Marshal(t)
	resp, err := c.makeRequest("POST", "/api/settings/sdcard/snapshots", body)
	return apiData(resp, err, "snapshots", nil)
}

// The KVM's own Tailscale (usbridge web/handlers.go AuthTailscale*).

func (c *USBClient) GetKVMTailscale() (*models.TailscaleStatus, error) {
	var s models.TailscaleStatus
	resp, err := c.makeRequest("GET", "/api/auth/tailscale/status", nil)
	if err := apiData(resp, err, "Tailscale", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// LoginKVMTailscale joins the KVM to a tailnet: with an auth key directly,
// without one the reply carries the login URL to open.
func (c *USBClient) LoginKVMTailscale(authKey string) (*models.TailscaleStatus, error) {
	body, _ := json.Marshal(map[string]string{"device_token": string(c.apiSecret), "auth_key": authKey})
	var s models.TailscaleStatus
	resp, err := c.makeRequest("POST", "/api/auth/tailscale/register", body)
	if err := apiData(resp, err, "Tailscale", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *USBClient) LogoutKVMTailscale() error {
	resp, err := c.makeRequest("POST", "/api/auth/tailscale/logout", []byte("{}"))
	return apiData(resp, err, "Tailscale", nil)
}
