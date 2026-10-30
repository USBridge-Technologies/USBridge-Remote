package api

import (
	"encoding/json"

	"usbridge-client/internal/models"
)

// The monitor and USB device the KVM shows the PC (usbridge
// web/identity_api.go). Older firmware has neither: the calls fail and the
// client shows no picker.

func (c *USBClient) GetEDID() (*models.EDIDStatus, error) {
	var s models.EDIDStatus
	resp, err := c.makeRequest("GET", "/api/device/edid", nil)
	if err := apiData(resp, err, "EDID", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SetEDID writes a preset into the KVM's HDMI chip; the PC sees it once the
// KVM is power cycled.
func (c *USBClient) SetEDID(id string) (*models.EDIDStatus, error) {
	body, _ := json.Marshal(map[string]string{"id": id})
	var s models.EDIDStatus
	resp, err := c.makeRequest("POST", "/api/device/edid", body)
	if err := apiData(resp, err, "EDID", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *USBClient) GetUSBIdentity() (*models.USBIdentityStatus, error) {
	var s models.USBIdentityStatus
	resp, err := c.makeRequest("GET", "/api/device/usb_identity", nil)
	if err := apiData(resp, err, "USB identity", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SetUSBIdentity saves the identities ("" = USBridge's own); the KVM uses
// them the next time it starts the USB devices.
func (c *USBClient) SetUSBIdentity(kbm, audio string) (*models.USBIdentityStatus, error) {
	body, _ := json.Marshal(map[string]string{"kbm": kbm, "audio": audio})
	var s models.USBIdentityStatus
	resp, err := c.makeRequest("POST", "/api/device/usb_identity", body)
	if err := apiData(resp, err, "USB identity", &s); err != nil {
		return nil, err
	}
	return &s, nil
}
