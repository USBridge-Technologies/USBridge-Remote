package service

import (
	"bufio"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"usbridge-client/internal/models"
)

type WireGuardService interface {
	Connect(*models.WireGuardBootstrapResponse) error
	Disconnect() error
	IsRunning() bool
	GetServerHost() string
	GetClientHost() string
	GetPeerStatus() (*WireGuardPeerStatus, error)
	GeneratePublicKey() (string, error)
	GetPrivateKey() string
	SetPrivateKey(string) error
}

type WireGuardPeerStatus struct {
	ServerPublicKey     string
	PersistentKeepalive int
	LastHandshakeAt     time.Time
	RxBytes             uint64
	TxBytes             uint64
	PeerCount           int
}

func NewWireGuardService(config *models.AppConfig) WireGuardService {
	return newWireGuardService(config)
}

func GenerateWireGuardPrivateKey() (string, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to generate X25519 key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key.Bytes()), nil
}

func WireGuardPublicKeyFromPrivate(privateKey string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privateKey))
	if err != nil {
		return "", fmt.Errorf("invalid WireGuard private key: %w", err)
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("failed to build X25519 private key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

func parseWireGuardIPCStatus(uapi, serverPublicKey string, persistentKeepalive int) (*WireGuardPeerStatus, error) {
	status := &WireGuardPeerStatus{
		ServerPublicKey:     strings.TrimSpace(serverPublicKey),
		PersistentKeepalive: persistentKeepalive,
	}

	var current *WireGuardPeerStatus
	scanner := bufio.NewScanner(strings.NewReader(uapi))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "errno=0" {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		switch key {
		case "public_key":
			status.PeerCount++
			peerStatus := &WireGuardPeerStatus{
				ServerPublicKey:     strings.TrimSpace(value),
				PersistentKeepalive: persistentKeepalive,
			}
			current = peerStatus
			if status.ServerPublicKey == "" || status.ServerPublicKey == peerStatus.ServerPublicKey {
				status.ServerPublicKey = peerStatus.ServerPublicKey
				status.LastHandshakeAt = peerStatus.LastHandshakeAt
				status.RxBytes = peerStatus.RxBytes
				status.TxBytes = peerStatus.TxBytes
			}
		case "last_handshake_time_sec":
			if current == nil {
				continue
			}
			sec, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid wireguard handshake seconds %q: %w", value, err)
			}
			if sec > 0 {
				current.LastHandshakeAt = time.Unix(sec, current.LastHandshakeAt.UnixNano()%int64(time.Second))
			}
			if status.ServerPublicKey == current.ServerPublicKey {
				status.LastHandshakeAt = current.LastHandshakeAt
			}
		case "last_handshake_time_nsec":
			if current == nil {
				continue
			}
			nsec, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid wireguard handshake nanoseconds %q: %w", value, err)
			}
			if !current.LastHandshakeAt.IsZero() {
				current.LastHandshakeAt = time.Unix(current.LastHandshakeAt.Unix(), nsec)
			}
			if status.ServerPublicKey == current.ServerPublicKey {
				status.LastHandshakeAt = current.LastHandshakeAt
			}
		case "tx_bytes":
			if current == nil {
				continue
			}
			txBytes, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid wireguard tx_bytes %q: %w", value, err)
			}
			current.TxBytes = txBytes
			if status.ServerPublicKey == current.ServerPublicKey {
				status.TxBytes = txBytes
			}
		case "rx_bytes":
			if current == nil {
				continue
			}
			rxBytes, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid wireguard rx_bytes %q: %w", value, err)
			}
			current.RxBytes = rxBytes
			if status.ServerPublicKey == current.ServerPublicKey {
				status.RxBytes = rxBytes
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan wireguard ipc status: %w", err)
	}
	if status.PeerCount == 0 {
		return nil, fmt.Errorf("wireguard peer status is empty")
	}
	return status, nil
}
