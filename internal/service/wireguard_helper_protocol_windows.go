//go:build windows

package service

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"usbridge-client/internal/models"
)

type windowsWireGuardHelperRequest struct {
	Token   string                           `json:"token"`
	Command string                           `json:"command"`
	Payload *windowsWireGuardHelperUpPayload `json:"payload,omitempty"`
}

type windowsWireGuardHelperUpPayload struct {
	Bootstrap  *models.WireGuardBootstrapResponse `json:"bootstrap"`
	PrivateKey string                             `json:"private_key"`
	ListenPort int                                `json:"listen_port"`
	IfaceName  string                             `json:"iface_name"`
}

type windowsWireGuardHelperResponse struct {
	OK                bool   `json:"ok"`
	Error             string `json:"error,omitempty"`
	IfaceName         string `json:"iface_name,omitempty"`
	LastHandshakeSec  int64  `json:"last_handshake_sec,omitempty"`
	LastHandshakeNSec int64  `json:"last_handshake_nsec,omitempty"`
	RxBytes           uint64 `json:"rx_bytes,omitempty"`
	TxBytes           uint64 `json:"tx_bytes,omitempty"`
	PeerCount         int    `json:"peer_count,omitempty"`
}

func (c *windowsWireGuardHelperClient) call(request windowsWireGuardHelperRequest) (*windowsWireGuardHelperResponse, error) {
	request.Token = c.token

	conn, err := net.DialTimeout("tcp", c.addr, 1500*time.Millisecond)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	if request.Command == "up" {
		deadline = time.Now().Add(90 * time.Second)
	}
	_ = conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return nil, fmt.Errorf("failed to send request to WireGuard helper: %w", err)
	}

	var response windowsWireGuardHelperResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to read response from WireGuard helper: %w", err)
	}
	if !response.OK {
		return nil, fmt.Errorf(strings.TrimSpace(response.Error))
	}
	return &response, nil
}

func (r *windowsWireGuardHelperResponse) PeerStatus(serverPublicKey string, persistentKeepalive int) *WireGuardPeerStatus {
	status := &WireGuardPeerStatus{
		ServerPublicKey:     strings.TrimSpace(serverPublicKey),
		PersistentKeepalive: persistentKeepalive,
		RxBytes:             r.RxBytes,
		TxBytes:             r.TxBytes,
		PeerCount:           r.PeerCount,
	}
	if r.LastHandshakeSec > 0 {
		status.LastHandshakeAt = time.Unix(r.LastHandshakeSec, r.LastHandshakeNSec)
	}
	return status
}
