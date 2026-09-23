// Package tlshost owns the two cert/key pairs the agent's HTTPS listener
// (see internal/app's new tlsServer, cfg.TLSPort) can present:
//   - a self-signed cert (default, works with zero external dependencies --
//     LAN IP or plain hostname access) that a browser must manually accept
//     a TOFU warning for once.
//   - the shared *.device.usbridge.io wildcard cert (see
//     internal/devicecert), fetched from the usbridge-entitlement backend
//     once this machine's own <label>.device.usbridge.io hostname is
//     known -- publicly-CA-issued (Let's Encrypt), so a browser hitting
//     THAT hostname gets no warning at all. This is what makes the
//     browser-based web client (client/web) work at all: it's loaded from
//     https://web.usbridge.io and cannot fetch()/WebSocket to a plain-HTTP
//     or self-signed-HTTPS origin (mixed content / untrusted cert, neither
//     has a click-through for a background request).
//
// Mirrors usbridge_service/web/tls_manager.go's design (same self-signed
// generation shape, same SNI-based GetCertificate dispatch) but built fresh
// for this repo rather than shared code -- see this repo and that one's own
// READMEs for why they're separate products with no shared Go module.
package tlshost

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	selfCertFile   = "self-cert.pem"
	selfKeyFile    = "self-key.pem"
	deviceCertFile = "device-cert.pem"
	deviceKeyFile  = "device-key.pem"
	deviceHostFile = "device-hostname.txt"

	// How long before expiry the device wildcard cert is considered due for
	// a re-fetch -- matches usbridge_service's identical tlsRenewBefore
	// reasoning: the backend itself renews well ahead of this, so under
	// normal operation this is just how promptly THIS process notices a
	// rotation, not a real renewal deadline.
	deviceCertRenewBefore = 30 * 24 * time.Hour
	// Self-signed cert validity -- long-lived since renewing it invalidates
	// every browser's TOFU exception for this machine, forcing the "not
	// private" warning to reappear.
	selfSignedValidity = 5 * 365 * 24 * time.Hour
)

// Manager is safe for concurrent use -- GetCertificate runs on every TLS
// handshake, potentially concurrently, while EnsureSelfSigned/
// InstallDeviceCert run from a single background watchdog goroutine but
// must never race a handshake reading the fields they update.
type Manager struct {
	mu       sync.Mutex
	dir      string // persistence directory, e.g. <StateDir>/web-tls
	self     *tls.Certificate
	selfLeaf *x509.Certificate

	device         *tls.Certificate
	deviceLeaf     *x509.Certificate
	deviceHostname string // the "<label>.device.usbridge.io" this device's cert covers, if any yet
}

// NewManager returns a Manager persisting its certs under dir (created on
// first write if missing) -- typically filepath.Join(cfg.StateDir,
// "web-tls").
func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

// LoadPersisted reads back whatever cert pairs were saved on a previous
// run, so a restart doesn't throw away a self-signed cert a browser
// already has a TOFU exception for, or force an immediate re-fetch of the
// device cert before the network's even up.
func (m *Manager) LoadPersisted() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cert, leaf, err := loadCertKeyPairFiles(filepath.Join(m.dir, selfCertFile), filepath.Join(m.dir, selfKeyFile)); err == nil {
		m.self, m.selfLeaf = cert, leaf
	}
	if hostnameBytes, err := os.ReadFile(filepath.Join(m.dir, deviceHostFile)); err == nil {
		if cert, leaf, err := loadCertKeyPairFiles(filepath.Join(m.dir, deviceCertFile), filepath.Join(m.dir, deviceKeyFile)); err == nil {
			m.device, m.deviceLeaf, m.deviceHostname = cert, leaf, strings.TrimSpace(string(hostnameBytes))
		}
	}
}

// EnsureSelfSigned (re)generates and persists the self-signed cert if none
// exists yet, or if the existing one no longer covers every ip in `ips`
// (e.g. a fresh DHCP lease) or is expiring soon. Cheap to call on every
// listener-refresh tick when unchanged -- a single in-memory coverage
// check, no disk I/O.
func (m *Manager) EnsureSelfSigned(ips []net.IP, dnsNames []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.selfLeaf != nil && certCoversIPs(m.selfLeaf, ips) && certCoversDNSNames(m.selfLeaf, dnsNames) && !certExpiringSoon(m.selfLeaf, time.Now(), 30*24*time.Hour) {
		return nil
	}

	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("tlshost: create %s: %w", m.dir, err)
	}
	certPEM, keyPEM, err := generateSelfSignedCertificate(ips, dnsNames)
	if err != nil {
		return fmt.Errorf("tlshost: generate self-signed cert: %w", err)
	}
	cert, leaf, err := parseCertKeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("tlshost: parse generated self-signed cert: %w", err)
	}
	if err := os.WriteFile(filepath.Join(m.dir, selfCertFile), certPEM, 0644); err != nil {
		log.Printf("tlshost: failed to persist self-signed cert: %v", err)
	}
	if err := os.WriteFile(filepath.Join(m.dir, selfKeyFile), keyPEM, 0600); err != nil {
		log.Printf("tlshost: failed to persist self-signed key: %v", err)
	}
	m.self, m.selfLeaf = cert, leaf
	log.Printf("🔒 [tls] self-signed cert ready (ips=%v dns=%v, valid until %s)", ips, dnsNames, leaf.NotAfter.Format(time.RFC3339))
	return nil
}

// InstallDeviceCert installs a freshly fetched shared wildcard cert/key
// (see internal/devicecert.FetchCert) for this device's own hostname,
// persisting it so a restart doesn't need a network round trip before
// HTTPS on that name works again.
func (m *Manager) InstallDeviceCert(hostname, certPEM, keyPEM string) error {
	cert, leaf, err := parseCertKeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return fmt.Errorf("tlshost: parse device cert: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("tlshost: create %s: %w", m.dir, err)
	}
	if err := os.WriteFile(filepath.Join(m.dir, deviceCertFile), []byte(certPEM), 0644); err != nil {
		return fmt.Errorf("tlshost: persist device cert: %w", err)
	}
	if err := os.WriteFile(filepath.Join(m.dir, deviceKeyFile), []byte(keyPEM), 0600); err != nil {
		return fmt.Errorf("tlshost: persist device key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(m.dir, deviceHostFile), []byte(hostname), 0644); err != nil {
		return fmt.Errorf("tlshost: persist device hostname: %w", err)
	}

	m.device, m.deviceLeaf, m.deviceHostname = cert, leaf, hostname
	log.Printf("🔒 [tls] device cert ready for %s (valid until %s)", hostname, leaf.NotAfter.Format(time.RFC3339))
	return nil
}

// DeviceCertStatus reports the hostname the current device cert (if any)
// was installed for, and whether it's missing or expiring soon -- what the
// background watchdog needs to decide if a FetchCert round trip is worth
// making this tick. Cheap: in-memory only.
func (m *Manager) DeviceCertStatus() (hostname string, needsRefresh bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deviceLeaf == nil {
		return m.deviceHostname, true
	}
	return m.deviceHostname, certExpiringSoon(m.deviceLeaf, time.Now(), deviceCertRenewBefore)
}

// GetCertificate is a tls.Config.GetCertificate callback: picks the device
// wildcard cert when the client's SNI name matches the hostname it was
// issued for, the self-signed cert otherwise (including when no SNI name
// was sent at all, e.g. a bare-IP connection).
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if hello.ServerName != "" && m.device != nil && hostnamesEqual(hello.ServerName, m.deviceHostname) {
		return m.device, nil
	}
	if m.self != nil {
		return m.self, nil
	}
	return nil, fmt.Errorf("tlshost: no certificate available yet")
}

func hostnamesEqual(a, b string) bool {
	norm := func(s string) string { return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".") }
	a, b = norm(a), norm(b)
	return a != "" && a == b
}

// --- pure, unit-testable helpers ---

func certCoversIPs(leaf *x509.Certificate, ips []net.IP) bool {
	for _, want := range ips {
		found := false
		for _, have := range leaf.IPAddresses {
			if have.Equal(want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func certCoversDNSNames(leaf *x509.Certificate, names []string) bool {
	have := make(map[string]bool, len(leaf.DNSNames))
	for _, n := range leaf.DNSNames {
		have[strings.ToLower(n)] = true
	}
	for _, want := range names {
		if !have[strings.ToLower(want)] {
			return false
		}
	}
	return true
}

func certExpiringSoon(leaf *x509.Certificate, now time.Time, within time.Duration) bool {
	return leaf.NotAfter.IsZero() || now.Add(within).After(leaf.NotAfter)
}

func generateSelfSignedCertificate(ips []net.IP, dnsNames []string) (certPEM, keyPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "usbridge-agent", Organization: []string{"USBridge"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(selfSignedValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // self-signed leaf acting as its own issuer
		IPAddresses:           ips,
		DNSNames:              dnsNames,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal key: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

func parseCertKeyPair(certPEM, keyPEM []byte) (*tls.Certificate, *x509.Certificate, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, nil, err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, nil, err
	}
	return &cert, leaf, nil
}

func loadCertKeyPairFiles(certPath, keyPath string) (*tls.Certificate, *x509.Certificate, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}
	return parseCertKeyPair(certPEM, keyPEM)
}
