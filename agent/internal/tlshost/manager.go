// Package tlshost owns the two cert/key pairs the agent's HTTPS listener
// (see internal/app's new tlsServer, cfg.TLSPort) can present:
//   - a self-signed cert (default, works with zero external dependencies --
//     LAN IP or plain hostname access) that a browser must manually accept
//     a TOFU warning for once.
//   - its own <label>.device.usbridge.io Let's Encrypt cert (see
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
	"strconv"
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
	// deviceDemandFile: unix time a browser last asked for this device's
	// own name (see NoteDeviceCertDemand), so renewals survive restarts.
	deviceDemandFile = "device-demand.txt"

	// How long before expiry the device cert is considered due for
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

	// deviceKey is this device's OWN persistent TLS key (see DeviceCSR) --
	// generated locally on first use and never sent anywhere; only a CSR
	// signed by it ever leaves this machine. Cached here once loaded/
	// generated so repeated DeviceCSR calls don't re-read disk; deliberately
	// separate from `device`/`deviceLeaf` above since this key outlives many
	// cert renewals (same key, new cert each time).
	deviceKey *ecdsa.PrivateKey

	// csrDER/csrHostname cache the last CSR DeviceCSR built, so every poll
	// sends byte-identical CSRs. An ECDSA signature is randomized, so
	// re-signing yields new bytes each call -- and the backend used to
	// fingerprint the raw CSR DER, so every poll looked like a new key:
	// it re-queued the row, burned the per-IP quota and made the relay
	// issue certs nobody could claim until Let's Encrypt's duplicate-cert
	// limit was hit. The backend now fingerprints the public key instead;
	// this is the belt to that pair of braces.
	csrDER      []byte
	csrHostname string

	// lastCertErr/lastCertErrAt record the most recent device-cert issuance
	// failure (set by internal/app's tickDeviceCert via SetDeviceCertError)
	// so CertStatus can surface it to the Status UI's retry button instead
	// of silently leaving the user stuck on a stale self-signed fallback
	// with no explanation. Cleared the moment InstallDeviceCert next
	// succeeds.
	lastCertErr   error
	lastCertErrAt time.Time

	// Certificates are issued on demand: Let's Encrypt allows 50 per week
	// for all of usbridge.io, and fetching one at every install's first
	// start (VMs, machines nobody opens in a browser) used the whole budget
	// up. registeredHostname is the name the backend points at this machine
	// (known before any cert); a TLS handshake for it is the demand.
	registeredHostname string
	lastDemand         time.Time
	persistedDemand    time.Time
	demandC            chan struct{}
}

// NewManager returns a Manager persisting its certs under dir (created on
// first write if missing) -- typically filepath.Join(cfg.StateDir,
// "web-tls").
func NewManager(dir string) *Manager {
	return &Manager{dir: dir, demandC: make(chan struct{}, 1)}
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
	if b, err := os.ReadFile(filepath.Join(m.dir, deviceDemandFile)); err == nil {
		if secs, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
			m.lastDemand = time.Unix(secs, 0)
			m.persistedDemand = m.lastDemand
		}
	}
	if hostnameBytes, err := os.ReadFile(filepath.Join(m.dir, deviceHostFile)); err == nil {
		hostname := strings.TrimSpace(string(hostnameBytes))
		if cert, leaf, err := loadCertKeyPairFiles(filepath.Join(m.dir, deviceCertFile), filepath.Join(m.dir, deviceKeyFile)); err == nil {
			if isOwnDeviceCert(leaf, hostname) {
				m.device, m.deviceLeaf, m.deviceHostname = cert, leaf, hostname
			} else {
				m.discardLegacyDeviceCertLocked(leaf)
			}
		}
	}
}

// isOwnDeviceCert reports whether leaf is a per-device cert issued for
// exactly hostname -- as opposed to the old shared *.device.usbridge.io
// wildcard (agents <= 3.0.50 fetched it WITH its fleet-wide private key;
// revoked 2026-09-30 for keyCompromise).
func isOwnDeviceCert(leaf *x509.Certificate, hostname string) bool {
	return len(leaf.DNSNames) == 1 && leaf.DNSNames[0] == hostname
}

// discardLegacyDeviceCertLocked deletes a legacy shared-wildcard cert AND
// its key from disk. The key must go too: it is the leaked fleet-wide
// wildcard key, and ensureDeviceKeyLocked would otherwise reuse it as this
// device's "own" key for every future CSR (Let's Encrypt refuses it anyway
// -- keys revoked for keyCompromise are blocked). Callers must hold m.mu.
func (m *Manager) discardLegacyDeviceCertLocked(leaf *x509.Certificate) {
	log.Printf("🔒 [tls] discarding legacy shared device cert (SANs %v, serial %X) and its key -- will request a per-device cert", leaf.DNSNames, leaf.SerialNumber)
	for _, f := range []string{deviceCertFile, deviceKeyFile} {
		if err := os.Remove(filepath.Join(m.dir, f)); err != nil && !os.IsNotExist(err) {
			log.Printf("tlshost: failed to remove legacy %s: %v", f, err)
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

// ensureDeviceKeyLocked returns this device's own persistent TLS key,
// loading it from disk or generating (and persisting) a fresh one on first
// use. Callers must hold m.mu.
func (m *Manager) ensureDeviceKeyLocked() (*ecdsa.PrivateKey, error) {
	if m.deviceKey != nil {
		return m.deviceKey, nil
	}
	if keyPEM, err := os.ReadFile(filepath.Join(m.dir, deviceKeyFile)); err == nil {
		if key, err := parseECDSAKeyPEM(keyPEM); err == nil {
			m.deviceKey = key
			return key, nil
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tlshost: generate device key: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("tlshost: marshal device key: %w", err)
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return nil, fmt.Errorf("tlshost: create %s: %w", m.dir, err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(m.dir, deviceKeyFile), keyPEM, 0600); err != nil {
		return nil, fmt.Errorf("tlshost: persist device key: %w", err)
	}
	m.deviceKey = key
	return key, nil
}

// DeviceCSR returns a DER-encoded PKCS#10 CSR naming hostname, signed by
// this device's own persistent key (see ensureDeviceKeyLocked) -- the
// private key itself never leaves this call. See
// devicecert.RequestCert for how the caller sends this to the backend, and
// this package's top doc comment for why that's the whole point: unlike
// the old shared-wildcard-key design, there is no key here for the backend
// (or anyone intercepting the request) to ever see.
func (m *Manager) DeviceCSR(hostname string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key, err := m.ensureDeviceKeyLocked()
	if err != nil {
		return nil, err
	}
	if m.csrDER != nil && m.csrHostname == hostname {
		return m.csrDER, nil
	}
	template := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: hostname},
		DNSNames: []string{hostname},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, err
	}
	m.csrDER, m.csrHostname = der, hostname
	return der, nil
}

// SetDeviceCertError records the most recent device-cert issuance failure
// (see internal/app's tickDeviceCert) so CertStatus can surface it to the
// Status UI's retry button. Pass nil to clear it outside of a successful
// InstallDeviceCert (not currently needed, but keeps the setter symmetric).
func (m *Manager) SetDeviceCertError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCertErr = err
	m.lastCertErrAt = time.Now()
}

// InstallDeviceCert installs a freshly issued leaf certificate (see
// devicecert.RequestCert) for this device's own hostname, pairing it with
// the local key DeviceCSR already signed a CSR with -- there is no key
// parameter: the backend only ever returns a certificate now (see this
// package's top doc comment). Persisting only the cert here is sufficient
// for a restart to work without a network round trip: ensureDeviceKeyLocked
// already persisted the matching key the moment DeviceCSR first generated
// it.
func (m *Manager) InstallDeviceCert(hostname, certPEM string) error {
	m.mu.Lock()
	key := m.deviceKey
	m.mu.Unlock()
	if key == nil {
		return fmt.Errorf("tlshost: InstallDeviceCert called before DeviceCSR generated a device key")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("tlshost: marshal device key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	cert, leaf, err := parseCertKeyPair([]byte(certPEM), keyPEM)
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
	if err := os.WriteFile(filepath.Join(m.dir, deviceHostFile), []byte(hostname), 0644); err != nil {
		return fmt.Errorf("tlshost: persist device hostname: %w", err)
	}

	m.device, m.deviceLeaf, m.deviceHostname = cert, leaf, hostname
	m.lastCertErr = nil
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

// CertStatus is a snapshot of what GetCertificate is currently serving --
// what the agent's Status UI (and any thin client reading it over adminapi)
// shows the user so they don't have to inspect the browser's own padlock to
// know whether this device has a browser-trusted Let's Encrypt cert yet or
// is still on the self-signed fallback (see this file's own top doc comment
// for what each one is for and why the difference matters for client/web).
type CertStatus struct {
	// Hostname is this device's "<label>.device.usbridge.io" name once
	// InstallDeviceCert has run for it, "" if none has ever been installed.
	Hostname string `json:"hostname"`
	// LetsEncrypt is true once a device cert for Hostname is actually
	// loaded and being served -- false means GetCertificate is still
	// falling back to the self-signed cert for every SNI name, even if
	// Hostname is already registered but the cert fetch hasn't landed yet.
	LetsEncrypt bool `json:"letsEncrypt"`
	// ExpiresAt is the NotAfter of whichever cert is currently active (the
	// device cert if LetsEncrypt, the self-signed one otherwise), zero if
	// neither has been generated/installed yet.
	ExpiresAt time.Time `json:"expiresAt"`
	// LastError is the most recent device-cert issuance failure's message
	// (see SetDeviceCertError), "" once the next attempt succeeds. The
	// Status UI's cert dialog shows this alongside a retry button rather
	// than leaving the user stuck on a stale self-signed fallback with no
	// explanation of why the trusted hostname never showed up.
	LastError string `json:"lastError,omitempty"`
}

// CertStatus reports what GetCertificate is currently serving -- cheap,
// in-memory only, safe to call from a UI refresh tick.
func (m *Manager) CertStatus() CertStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	errMsg := ""
	if m.lastCertErr != nil {
		errMsg = m.lastCertErr.Error()
	}
	if m.device != nil && m.deviceLeaf != nil {
		return CertStatus{Hostname: m.deviceHostname, LetsEncrypt: true, ExpiresAt: m.deviceLeaf.NotAfter, LastError: errMsg}
	}
	st := CertStatus{Hostname: m.deviceHostname, LastError: errMsg}
	if m.selfLeaf != nil {
		st.ExpiresAt = m.selfLeaf.NotAfter
	}
	return st
}

// GetCertificate is a tls.Config.GetCertificate callback: picks the device
// cert when the client's SNI name matches the hostname it was
// issued for, the self-signed cert otherwise (including when no SNI name
// was sent at all, e.g. a bare-IP connection).
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if hello.ServerName != "" && (hostnamesEqual(hello.ServerName, m.registeredHostname) || hostnamesEqual(hello.ServerName, m.deviceHostname)) {
		m.noteDemandLocked()
	}
	if hello.ServerName != "" && m.device != nil && hostnamesEqual(hello.ServerName, m.deviceHostname) {
		return m.device, nil
	}
	if m.self != nil {
		return m.self, nil
	}
	return nil, fmt.Errorf("tlshost: no certificate available yet")
}

// SetRegisteredHostname records the name the backend points at this
// machine (devicecert.RegisterIP), before any cert exists for it.
func (m *Manager) SetRegisteredHostname(hostname string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registeredHostname = hostname
}

// RegisteredHostname is the registered name, else the one the installed
// cert covers, "" before either.
func (m *Manager) RegisteredHostname() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.registeredHostname != "" {
		return m.registeredHostname
	}
	return m.deviceHostname
}

// NoteDeviceCertDemand records that the device cert is wanted now (a
// browser reached the device's name, or the user pressed retry) and wakes
// DemandC.
func (m *Manager) NoteDeviceCertDemand() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.noteDemandLocked()
}

// noteDemandLocked: callers hold m.mu. Runs inside TLS handshakes, so the
// file write is rare (hourly) and off the handshake.
func (m *Manager) noteDemandLocked() {
	now := time.Now()
	m.lastDemand = now
	if now.Sub(m.persistedDemand) >= time.Hour {
		m.persistedDemand = now
		dir, stamp := m.dir, strconv.FormatInt(now.Unix(), 10)
		go func() {
			if err := os.MkdirAll(dir, 0700); err == nil {
				_ = os.WriteFile(filepath.Join(dir, deviceDemandFile), []byte(stamp), 0644)
			}
		}()
	}
	select {
	case m.demandC <- struct{}{}:
	default:
	}
}

// DeviceCertDemanded reports whether demand was noted within the window.
func (m *Manager) DeviceCertDemanded(within time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.lastDemand.IsZero() && time.Since(m.lastDemand) <= within
}

// DemandC fires (coalesced) when demand is noted.
func (m *Manager) DemandC() <-chan struct{} { return m.demandC }

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

// parseECDSAKeyPEM decodes a single PKCS#8-in-PEM ECDSA private key, the
// same shape ensureDeviceKeyLocked persists -- used to load the device key
// back in on a later process start.
func parseECDSAKeyPEM(keyPEM []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("device key is not ECDSA")
	}
	return ecKey, nil
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
