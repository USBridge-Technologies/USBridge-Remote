package moonlight

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

// Identity represents the Moonlight client's cryptographic identity
type Identity struct {
	PrivateKey *rsa.PrivateKey
	Cert       *x509.Certificate
	CertPEM    []byte
}

// ConfigDirOverride lets platform-specific code (e.g. Android) inject a
// persistent writable directory so the identity survives app restarts.
var ConfigDirOverride string

func GetConfigDir() (string, error) {
	if ConfigDirOverride != "" {
		if err := os.MkdirAll(ConfigDirOverride, 0700); err == nil {
			return ConfigDirOverride, nil
		}
	}
	// Try candidates in order; use the first one where MkdirAll succeeds.
	candidates := []func() (string, error){
		func() (string, error) {
			d, err := os.UserConfigDir()
			if err != nil {
				return "", err
			}
			return filepath.Join(d, "usbridge-client", "moonlight"), nil
		},
		func() (string, error) {
			d, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			return filepath.Join(d, ".usbridge-client", "moonlight"), nil
		},
		// Android / restricted environments: fall back to the process temp dir.
		// os.TempDir() is always writable and is tied to the app's lifetime on Android.
		func() (string, error) {
			return filepath.Join(os.TempDir(), "usbridge-client", "moonlight"), nil
		},
	}

	var lastErr error
	for _, candidate := range candidates {
		dir, err := candidate()
		if err != nil {
			lastErr = err
			continue
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			lastErr = err
			continue
		}
		return dir, nil
	}
	return "", fmt.Errorf("no writable config directory found: %w", lastErr)
}

// LoadOrGenerateIdentity loads the identity from disk or generates a new one
func LoadOrGenerateIdentity() (*Identity, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return nil, err
	}

	keyPath := filepath.Join(dir, "client.key")
	certPath := filepath.Join(dir, "client.pem")

	keyBytes, err1 := os.ReadFile(keyPath)
	certBytes, err2 := os.ReadFile(certPath)

	if err1 == nil && err2 == nil {
		// Parse existing
		keyBlock, _ := pem.Decode(keyBytes)
		if keyBlock != nil {
			privKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
			if err == nil {
				certBlock, _ := pem.Decode(certBytes)
				if certBlock != nil {
					cert, err := x509.ParseCertificate(certBlock.Bytes)
					if err == nil {
						return &Identity{
							PrivateKey: privKey,
							Cert:       cert,
							CertPEM:    certBytes,
						}, nil
					}
				}
			}
		}
	}

	// Generate new
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName: "NVIDIA GameStream Client",
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(20 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)})

	_ = os.WriteFile(certPath, certPEM, 0600)
	_ = os.WriteFile(keyPath, keyPEM, 0600)

	return &Identity{
		PrivateKey: privKey,
		Cert:       cert,
		CertPEM:    certPEM,
	}, nil
}

// ecb is a simple ECB mode implementation since Go's standard library doesn't provide it
type ecb struct {
	b         cipher.Block
	blockSize int
}

func newECB(b cipher.Block) *ecb {
	return &ecb{
		b:         b,
		blockSize: b.BlockSize(),
	}
}

func (x *ecb) BlockSize() int { return x.blockSize }

func (x *ecb) CryptBlocks(dst, src []byte) {
	if len(src)%x.blockSize != 0 {
		panic("crypto/cipher: input not full blocks")
	}
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	for len(src) > 0 {
		x.b.Encrypt(dst, src[:x.blockSize])
		src = src[x.blockSize:]
		dst = dst[x.blockSize:]
	}
}

func (x *ecb) DecryptBlocks(dst, src []byte) {
	if len(src)%x.blockSize != 0 {
		panic("crypto/cipher: input not full blocks")
	}
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	for len(src) > 0 {
		x.b.Decrypt(dst, src[:x.blockSize])
		src = src[x.blockSize:]
		dst = dst[x.blockSize:]
	}
}

func AES128ECBEncrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	mode := newECB(block)

	// GameStream uses zero padding if not multiple of 16
	paddingSize := 16 - (len(data) % 16)
	if paddingSize != 16 {
		data = append(data, bytes.Repeat([]byte{0}, paddingSize)...)
	}

	dst := make([]byte, len(data))
	mode.CryptBlocks(dst, data)
	return dst, nil
}

func AES128ECBDecrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	mode := newECB(block)
	dst := make([]byte, len(data))
	mode.DecryptBlocks(dst, data)
	return dst, nil
}

// ExtractPlainCert removes PEM headers to get raw hex/base64 compatible string
func ExtractPlainCert(certPEM []byte) []byte {
	block, _ := pem.Decode(certPEM)
	if block != nil {
		return block.Bytes
	}
	return nil
}

func GenerateSalt() []byte {
	salt := make([]byte, 16)
	rand.Read(salt)
	return salt
}

func GeneratePIN() string {
	pinBytes := make([]byte, 2)
	rand.Read(pinBytes)
	pin := int(pinBytes[0])<<8 | int(pinBytes[1])
	return fmt.Sprintf("%04d", pin%10000)
}

func DeriveAESKey(salt []byte, pin string) []byte {
	saltedPin := append(salt, []byte(pin)...)
	hash := sha256.Sum256(saltedPin)
	return hash[:16] // Truncate to 16 bytes for AES-128
}

func SignData(privKey *rsa.PrivateKey, data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	return rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash[:])
}

func VerifySignature(cert *x509.Certificate, data, signature []byte) error {
	hash := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(cert.PublicKey.(*rsa.PublicKey), crypto.SHA256, hash[:], signature)
}
