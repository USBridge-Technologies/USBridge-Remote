//go:build darwin

package service

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"usbridge-client/internal/models"
)

type darwinWireGuardHelperRequest struct {
	Command string                    `json:"command"`
	Payload *darwinWireGuardUpPayload `json:"payload,omitempty"`
}

type darwinWireGuardUpPayload struct {
	Bootstrap  *models.WireGuardBootstrapResponse `json:"bootstrap"`
	PrivateKey string                             `json:"private_key"`
	ListenPort int                                `json:"listen_port"`
	IfaceName  string                             `json:"iface_name"`
}

type darwinWireGuardHelperResponse struct {
	OK                bool   `json:"ok"`
	Error             string `json:"error,omitempty"`
	IfaceName         string `json:"iface_name,omitempty"`
	LastHandshakeSec  int64  `json:"last_handshake_sec,omitempty"`
	LastHandshakeNSec int64  `json:"last_handshake_nsec,omitempty"`
	RxBytes           uint64 `json:"rx_bytes,omitempty"`
	TxBytes           uint64 `json:"tx_bytes,omitempty"`
	PeerCount         int    `json:"peer_count,omitempty"`
}

func (c *darwinWireGuardHelperClient) call(request darwinWireGuardHelperRequest) (*darwinWireGuardHelperResponse, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 1500*time.Millisecond)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return nil, fmt.Errorf("failed to send request to WireGuard helper: %w", err)
	}

	var response darwinWireGuardHelperResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to read response from WireGuard helper: %w", err)
	}
	if !response.OK {
		return nil, fmt.Errorf("%s", strings.TrimSpace(response.Error))
	}
	return &response, nil
}

func runDarwinHelperCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (r *darwinWireGuardHelperResponse) PeerStatus(serverPublicKey string, persistentKeepalive int) *WireGuardPeerStatus {
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
