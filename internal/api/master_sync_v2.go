package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"usbridge-client/internal/models"
)

type MasterSyncRequestV2 struct {
	Payload   string `json:"payload"`
	IV        string `json:"iv"`
	Timestamp int64  `json:"timestamp"`
}

type MasterSyncPayloadV2 struct {
	MoonlightPIN string `json:"moonlight_pin,omitempty"`
	TailscaleKey string `json:"tailscale_key,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
}

type MasterSyncResponseV2 struct {
	FRPToken        string                `json:"frp_token"`
	TailscaleStatus *models.TailscaleStatus `json:"tailscale_status,omitempty"`
	SunshineStatus  string                `json:"sunshine_status"`
}

func (c *USBClient) MasterSyncV2(ctx context.Context, payload MasterSyncPayloadV2) (*MasterSyncResponseV2, error) {
	if len(c.apiSecret) == 0 {
		return nil, fmt.Errorf("API secret not set")
	}

	encrypted, iv, err := EncryptPayloadV2(payload, c.apiSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt sync payload: %v", err)
	}

	req := MasterSyncRequestV2{
		Payload:   encrypted,
		IV:        iv,
		Timestamp: time.Now().Unix(),
	}

	body, _ := json.Marshal(req)
	resp, err := c.makeRequestWithContext(ctx, "POST", "/api/auth/sync", body, nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse sync response: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("sync failed: %s", apiResp.Message)
	}

	raw, _ := json.Marshal(apiResp.Data)
	var syncResp MasterSyncResponseV2
	if err := json.Unmarshal(raw, &syncResp); err != nil {
		return nil, fmt.Errorf("failed to parse sync data: %v", err)
	}

	return &syncResp, nil
}

func (c *USBClient) SetAPISecretV2(secret []byte) {
	c.apiSecret = secret
}
