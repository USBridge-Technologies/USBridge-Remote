// Package moonlightclient is a minimal, pure-Go, loopback-only Moonlight
// (NVIDIA GameStream) client. It exists to let the agent drive its own
// bundled Sunshine instance into an actively-streaming state entirely on its
// own — no human types a PIN, and no external Moonlight app is involved —
// purely so that raw H.264/HEVC video RTP and Opus audio RTP packets start
// flowing on 127.0.0.1, where agent/internal/webrtcbridge can pick them up
// and forward them into a WebRTC PeerConnection for a browser client.
//
// This package deliberately does not link moonlight-common-c (the C
// reference client library nearly every Moonlight app is built on). Pulling
// that in would mean cgo, which conflicts with the agent's pure-Go build.
// Instead, every wire format used here (NvHTTP pairing, the RTSP-flavored
// handshake, the ENet-based control channel) was reverse-derived by reading
// moonlight-common-c's source directly (see doc comments throughout this
// package for exactly which file/function was used as the reference) and
// re-implemented from scratch in Go. The NvHTTP pairing crypto in
// identity.go/pairing.go started from client/internal/api/moonlight's
// existing pure-Go implementation (client and agent are separate Go
// modules, so it couldn't be imported directly) and was adapted for this
// package's narrower loopback-only use case.
package moonlightclient

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// identity is this client's persistent RSA keypair + self-signed
// certificate. Sunshine's NvHTTP pairing protocol has each side present a
// self-signed cert during pairing and then re-uses it as the TLS client
// certificate for every subsequent mutually-authenticated HTTPS request
// (/launch, /resume, /cancel) -- so the same identity must be used for the
// whole lifetime of a pairing, not regenerated per request.
type identity struct {
	privateKey *rsa.PrivateKey
	cert       *x509.Certificate
	certPEM    []byte
}

// loadOrGenerateIdentity loads a persisted identity from dir (client.key +
// client.pem), or generates and persists a new one if dir is empty or
// nothing valid is there yet. Persisting means the agent doesn't have to
// re-pair with Sunshine (a fresh 5-stage handshake, plus a SubmitPIN admin
// call) on every single restart -- Sunshine remembers our cert by its
// content, so reusing the same keypair keeps us paired across restarts.
//
// If dir is "", the identity is purely in-memory and a fresh keypair (and
// therefore a fresh pairing) is required every time -- acceptable for
// short-lived tests, but Start()'s real callers should always pass a
// persistent directory.
func loadOrGenerateIdentity(dir string) (*identity, error) {
	if dir != "" {
		if id, err := loadIdentity(dir); err == nil {
			return id, nil
		}
	}

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName: "USBridge Loopback Moonlight Client",
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(20 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, fmt.Errorf("parse generated certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	id := &identity{privateKey: privKey, cert: cert, certPEM: certPEM}

	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err == nil {
			keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)})
			_ = os.WriteFile(filepath.Join(dir, "client.key"), keyPEM, 0o600)
			_ = os.WriteFile(filepath.Join(dir, "client.pem"), certPEM, 0o600)
		}
	}

	return id, nil
}

func loadIdentity(dir string) (*identity, error) {
	keyBytes, err := os.ReadFile(filepath.Join(dir, "client.key"))
	if err != nil {
		return nil, err
	}
	certBytes, err := os.ReadFile(filepath.Join(dir, "client.pem"))
	if err != nil {
		return nil, err
	}

	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil {
		return nil, fmt.Errorf("invalid client.key PEM")
	}
	privKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}

	certBlock, _ := pem.Decode(certBytes)
	if certBlock == nil {
		return nil, fmt.Errorf("invalid client.pem PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, err
	}

	return &identity{privateKey: privKey, cert: cert, certPEM: certBytes}, nil
}

// extractPlainCertDER strips the PEM envelope, returning the raw DER bytes
// -- what Sunshine actually wants hex-encoded in the "clientcert" pairing
// parameter, and what client/internal/api/moonlight's ClientID derivation
// hashes.
func extractPlainCertDER(certPEM []byte) []byte {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil
	}
	return block.Bytes
}

// ─── AES-128-ECB (GameStream's pairing cipher) ─────────────────────────────
//
// Go's standard library deliberately doesn't provide ECB mode (it's unsafe
// for general use -- identical plaintext blocks produce identical
// ciphertext blocks), but Sunshine/GFE's pairing protocol specifically
// requires it: each pairing challenge is exactly one or two 16-byte blocks,
// so ECB's weakness (pattern leakage across blocks) doesn't apply here.

type ecbCipher struct {
	block     cipher.Block
	blockSize int
}

func newECB(b cipher.Block) *ecbCipher {
	return &ecbCipher{block: b, blockSize: b.BlockSize()}
}

func (x *ecbCipher) cryptBlocks(dst, src []byte, encrypt bool) {
	for len(src) > 0 {
		if encrypt {
			x.block.Encrypt(dst, src[:x.blockSize])
		} else {
			x.block.Decrypt(dst, src[:x.blockSize])
		}
		src = src[x.blockSize:]
		dst = dst[x.blockSize:]
	}
}

func aes128ECBEncrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	mode := newECB(block)

	// GameStream zero-pads plaintext that isn't already block-aligned.
	if pad := 16 - (len(data) % 16); pad != 16 {
		data = append(append([]byte{}, data...), bytes.Repeat([]byte{0}, pad)...)
	}

	dst := make([]byte, len(data))
	mode.cryptBlocks(dst, data, true)
	return dst, nil
}

func aes128ECBDecrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	mode := newECB(block)
	dst := make([]byte, len(data))
	mode.cryptBlocks(dst, data, false)
	return dst, nil
}

func generateSalt() []byte {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	return salt
}

// generatePIN returns a random 4-digit PIN string, the same format a human
// would type into a Moonlight client's pairing dialog. Since this package
// controls both ends of the pairing (it submits the PIN to Sunshine's admin
// API itself, via Config.SubmitPIN), the PIN's only job here is to be the
// shared secret that derives the AES key both sides use -- it never needs
// to be shown to anyone.
func generatePIN() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%04d", (int(b[0])<<8|int(b[1]))%10000)
}

func deriveAESKey(salt []byte, pin string) []byte {
	saltedPin := append(append([]byte{}, salt...), []byte(pin)...)
	hash := sha256.Sum256(saltedPin)
	return hash[:16]
}

func signData(privKey *rsa.PrivateKey, data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	return rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash[:])
}
