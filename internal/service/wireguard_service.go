package service

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"usbridge-client/internal/models"
)

type WireGuardService interface {
	Connect(*models.WireGuardBootstrapResponse) error
	Disconnect() error
	IsRunning() bool
	GetServerHost() string
	GetClientHost() string
	GeneratePublicKey() (string, error)
	GetPrivateKey() string
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
