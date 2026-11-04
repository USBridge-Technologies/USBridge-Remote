package tlshost

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureSelfSigned_GeneratesAndCovers(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	ip := net.ParseIP("127.0.0.1")
	if err := m.EnsureSelfSigned([]net.IP{ip}, []string{"usbridge-agent.local"}); err != nil {
		t.Fatalf("EnsureSelfSigned: %v", err)
	}
	cert, err := m.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if cert == nil {
		t.Fatal("GetCertificate returned nil cert with no error")
	}
}

func TestEnsureSelfSigned_NoOpWhenAlreadyCovers(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	ip := net.ParseIP("127.0.0.1")
	if err := m.EnsureSelfSigned([]net.IP{ip}, nil); err != nil {
		t.Fatal(err)
	}
	first := m.selfLeaf
	if err := m.EnsureSelfSigned([]net.IP{ip}, nil); err != nil {
		t.Fatal(err)
	}
	if m.selfLeaf != first {
		t.Error("EnsureSelfSigned regenerated the cert even though it already covered the requested IPs")
	}
}

func TestEnsureSelfSigned_RegeneratesWhenIPMissing(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	if err := m.EnsureSelfSigned([]net.IP{net.ParseIP("127.0.0.1")}, nil); err != nil {
		t.Fatal(err)
	}
	first := m.selfLeaf
	if err := m.EnsureSelfSigned([]net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("192.168.1.5")}, nil); err != nil {
		t.Fatal(err)
	}
	if m.selfLeaf == first {
		t.Error("EnsureSelfSigned did not regenerate when a new IP needed covering")
	}
	if !certCoversIPs(m.selfLeaf, []net.IP{net.ParseIP("192.168.1.5")}) {
		t.Error("regenerated cert still doesn't cover the new IP")
	}
}

func TestLoadPersisted_RoundTripsSelfSignedAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	m1 := newTestManager(t, dir)
	if err := m1.EnsureSelfSigned([]net.IP{net.ParseIP("127.0.0.1")}, nil); err != nil {
		t.Fatal(err)
	}

	m2 := newTestManager(t, dir)
	m2.LoadPersisted()
	if m2.selfLeaf == nil {
		t.Fatal("LoadPersisted did not pick up the previously generated self-signed cert")
	}
	if m2.selfLeaf.SerialNumber.Cmp(m1.selfLeaf.SerialNumber) != 0 {
		t.Error("loaded cert has a different serial than what was persisted -- not actually the same cert")
	}
}

// signCSRForTest builds a leaf certificate for csr's OWN public key, signed
// by a throwaway CA key generated here -- standing in for what the real
// backend (ACME) does: sign the caller's CSR, never mint a keypair of its
// own. Mirrors internal/app/devicecert_test.go's identical helper (that
// package can't import this one's unexported symbols, and vice versa).
func signCSRForTest(t *testing.T, csr *x509.CertificateRequest) (certPEM string) {
	t.Helper()
	caPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPriv.PublicKey, caPriv)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
		DNSNames:     csr.DNSNames,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, csr.PublicKey, caPriv)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}))
}

// installTestDeviceCert drives the real DeviceCSR -> (sign) -> InstallDeviceCert
// sequence for hostname, the same round trip tickDeviceCert performs against
// the real backend -- exercises the actual key-pairing path rather than
// installing an unrelated cert/key pair.
func installTestDeviceCert(t *testing.T, m *Manager, hostname string) {
	t.Helper()
	csrDER, err := m.DeviceCSR(hostname)
	if err != nil {
		t.Fatalf("DeviceCSR: %v", err)
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		t.Fatalf("parse generated CSR: %v", err)
	}
	certPEM := signCSRForTest(t, csr)
	if err := m.InstallDeviceCert(hostname, certPEM); err != nil {
		t.Fatalf("InstallDeviceCert: %v", err)
	}
}

func TestInstallDeviceCert_ThenGetCertificatePicksItBySNI(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	if err := m.EnsureSelfSigned([]net.IP{net.ParseIP("127.0.0.1")}, nil); err != nil {
		t.Fatal(err)
	}
	installTestDeviceCert(t, m, "abc123.device.usbridge.io")

	deviceCert, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "abc123.device.usbridge.io"})
	if err != nil {
		t.Fatalf("GetCertificate(device hostname): %v", err)
	}
	if deviceCert.Leaf != nil && deviceCert.Leaf.Subject.CommonName != "abc123.device.usbridge.io" {
		t.Errorf("got wrong cert for device hostname SNI")
	}

	fallback, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "192.168.1.5"})
	if err != nil {
		t.Fatalf("GetCertificate(bare IP): %v", err)
	}
	if fallback == deviceCert {
		t.Error("a non-matching SNI name should fall back to the self-signed cert, not the device cert")
	}
}

func TestDeviceCertStatus_NeedsRefreshWhenNoneInstalled(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	hostname, needsRefresh := m.DeviceCertStatus()
	if hostname != "" || !needsRefresh {
		t.Errorf("DeviceCertStatus on a fresh Manager = (%q, %v), want (\"\", true)", hostname, needsRefresh)
	}
}

func TestDeviceCertStatus_NoRefreshWhenFresh(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	installTestDeviceCert(t, m, "abc123.device.usbridge.io")
	hostname, needsRefresh := m.DeviceCertStatus()
	if hostname != "abc123.device.usbridge.io" || needsRefresh {
		t.Errorf("DeviceCertStatus after install = (%q, %v), want (abc123.device.usbridge.io, false)", hostname, needsRefresh)
	}
}

func TestGetCertificate_ErrorsWhenNothingInstalledYet(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	if _, err := m.GetCertificate(&tls.ClientHelloInfo{}); err == nil {
		t.Fatal("expected an error before EnsureSelfSigned/InstallDeviceCert has ever run")
	}
}

func TestDeviceCSR_NamesHostnameInSAN(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	csrDER, err := m.DeviceCSR("abc123.device.usbridge.io")
	if err != nil {
		t.Fatalf("DeviceCSR: %v", err)
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		t.Fatalf("parse generated CSR: %v", err)
	}
	if len(csr.DNSNames) != 1 || csr.DNSNames[0] != "abc123.device.usbridge.io" {
		t.Errorf("csr.DNSNames = %v, want [abc123.device.usbridge.io]", csr.DNSNames)
	}
}

func TestDeviceCSR_ReusesTheSameKeyAcrossCalls(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	csr1DER, err := m.DeviceCSR("abc123.device.usbridge.io")
	if err != nil {
		t.Fatal(err)
	}
	// A second CSR (e.g. a renewal for the same hostname) must be signed by
	// the SAME device key, not a freshly generated one -- otherwise every
	// renewal would silently orphan whatever cert the previous key's CSR
	// earned.
	csr2DER, err := m.DeviceCSR("abc123.device.usbridge.io")
	if err != nil {
		t.Fatal(err)
	}
	csr1, err := x509.ParseCertificateRequest(csr1DER)
	if err != nil {
		t.Fatal(err)
	}
	csr2, err := x509.ParseCertificateRequest(csr2DER)
	if err != nil {
		t.Fatal(err)
	}
	pub1, ok1 := csr1.PublicKey.(*ecdsa.PublicKey)
	pub2, ok2 := csr2.PublicKey.(*ecdsa.PublicKey)
	if !ok1 || !ok2 {
		t.Fatalf("CSR public keys are not ECDSA: %T, %T", csr1.PublicKey, csr2.PublicKey)
	}
	if pub1.X.Cmp(pub2.X) != 0 || pub1.Y.Cmp(pub2.Y) != 0 {
		t.Error("DeviceCSR signed two CSRs with two different keys -- the device key must be stable across calls")
	}
}

func TestDeviceCSR_ByteIdenticalWhilePolling(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	a, err := m.DeviceCSR("abc123.device.usbridge.io")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.DeviceCSR("abc123.device.usbridge.io")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two DeviceCSR calls for the same hostname returned different bytes -- each poll must send the same CSR")
	}
	c, err := m.DeviceCSR("other.device.usbridge.io")
	if err != nil {
		t.Fatal(err)
	}
	if csr, err := x509.ParseCertificateRequest(c); err != nil || len(csr.DNSNames) != 1 || csr.DNSNames[0] != "other.device.usbridge.io" {
		t.Errorf("CSR after hostname change names %v (err %v), want [other.device.usbridge.io]", csr.DNSNames, err)
	}
}

func TestDeviceCSR_KeySurvivesAcrossManagerInstances(t *testing.T) {
	dir := t.TempDir()
	m1 := newTestManager(t, dir)
	csr1DER, err := m1.DeviceCSR("abc123.device.usbridge.io")
	if err != nil {
		t.Fatal(err)
	}
	csr1, err := x509.ParseCertificateRequest(csr1DER)
	if err != nil {
		t.Fatal(err)
	}

	// A fresh Manager over the SAME directory (simulating a process
	// restart) must sign with the SAME persisted key, not generate a new
	// one -- otherwise a restart between DeviceCSR and the backend's
	// response would orphan any in-flight cert request.
	m2 := newTestManager(t, dir)
	csr2DER, err := m2.DeviceCSR("abc123.device.usbridge.io")
	if err != nil {
		t.Fatal(err)
	}
	csr2, err := x509.ParseCertificateRequest(csr2DER)
	if err != nil {
		t.Fatal(err)
	}

	pub1 := csr1.PublicKey.(*ecdsa.PublicKey)
	pub2 := csr2.PublicKey.(*ecdsa.PublicKey)
	if pub1.X.Cmp(pub2.X) != 0 || pub1.Y.Cmp(pub2.Y) != 0 {
		t.Error("device key was not persisted -- a fresh Manager over the same dir generated a different key")
	}
}

func TestInstallDeviceCert_FailsBeforeDeviceCSREverRan(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	// Build a cert for some unrelated key, since there's no device key yet
	// to build one for that would legitimately pair.
	otherPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: []string{"abc123.device.usbridge.io"}}, otherPriv)
	if err != nil {
		t.Fatal(err)
	}
	parsedCSR, err := x509.ParseCertificateRequest(csr)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := signCSRForTest(t, parsedCSR)
	if err := m.InstallDeviceCert("abc123.device.usbridge.io", certPEM); err == nil {
		t.Fatal("expected InstallDeviceCert to fail when DeviceCSR was never called to establish a local key")
	}
}

func TestCertStatus_SurfacesAndClearsDeviceCertError(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	if st := m.CertStatus(); st.LastError != "" {
		t.Errorf("CertStatus.LastError on a fresh Manager = %q, want empty", st.LastError)
	}

	m.SetDeviceCertError(fmt.Errorf("backend unreachable"))
	st := m.CertStatus()
	if st.LastError != "backend unreachable" {
		t.Errorf("CertStatus.LastError = %q, want %q", st.LastError, "backend unreachable")
	}

	// A subsequent successful install clears it -- the retry button's whole
	// point is that a later success removes the error, not just adds a cert
	// alongside a stale one.
	installTestDeviceCert(t, m, "abc123.device.usbridge.io")
	if st := m.CertStatus(); st.LastError != "" {
		t.Errorf("CertStatus.LastError after a successful InstallDeviceCert = %q, want empty", st.LastError)
	}
}

// Agents <= 3.0.50 persisted the shared *.device.usbridge.io wildcard cert
// together with its fleet-wide private key under the same file names the
// per-device flow uses. LoadPersisted must drop both, so the next DeviceCSR
// uses a fresh key instead of the leaked one.
func TestLoadPersisted_DiscardsLegacyWildcardCertAndKey(t *testing.T) {
	dir := t.TempDir()
	const hostname = "abc123.device.usbridge.io"

	sharedKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(5),
		Subject:      pkix.Name{CommonName: "*.device.usbridge.io"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(80 * 24 * time.Hour),
		DNSNames:     []string{"*.device.usbridge.io", "device.usbridge.io"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &sharedKey.PublicKey, sharedKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(sharedKey)
	mustWrite := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(deviceCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	mustWrite(deviceKeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	mustWrite(deviceHostFile, []byte(hostname))

	m := newTestManager(t, dir)
	m.LoadPersisted()

	if _, needsRefresh := m.DeviceCertStatus(); !needsRefresh {
		t.Fatal("legacy wildcard cert must not count as an installed device cert")
	}
	for _, f := range []string{deviceCertFile, deviceKeyFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed, stat err = %v", f, err)
		}
	}
	csrDER, err := m.DeviceCSR(hostname)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		t.Fatal(err)
	}
	if csr.PublicKey.(*ecdsa.PublicKey).Equal(&sharedKey.PublicKey) {
		t.Fatal("CSR reused the leaked shared wildcard key")
	}
}

func TestLoadPersisted_KeepsOwnPerDeviceCert(t *testing.T) {
	dir := t.TempDir()
	m := newTestManager(t, dir)
	installTestDeviceCert(t, m, "abc123.device.usbridge.io")

	m2 := newTestManager(t, dir)
	m2.LoadPersisted()
	if host, needsRefresh := m2.DeviceCertStatus(); host != "abc123.device.usbridge.io" || needsRefresh {
		t.Fatalf("own per-device cert should survive a restart, got host=%q needsRefresh=%v", host, needsRefresh)
	}
}

func TestInstallTailnetCert_GetCertificatePicksItBySNI(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	if err := m.EnsureSelfSigned([]net.IP{net.ParseIP("127.0.0.1")}, nil); err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM, err := generateSelfSignedCertificate(nil, []string{"pc.tail1234.ts.net"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.InstallTailnetCert("pc.tail1234.ts.net", certPEM, keyPEM); err != nil {
		t.Fatalf("InstallTailnetCert: %v", err)
	}
	if got := m.TailnetHostname(); got != "pc.tail1234.ts.net" {
		t.Errorf("TailnetHostname = %q", got)
	}
	got, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "PC.tail1234.ts.net."})
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(got.Certificate[0])
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "pc.tail1234.ts.net" {
		t.Errorf("SNI ts.net got cert for %v, want the tailnet cert", leaf.DNSNames)
	}
	other, _ := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "other.example"})
	if other == got {
		t.Error("another SNI name must get the self-signed cert, not the tailnet one")
	}
	if st := m.CertStatus(); st.TailnetHostname != "pc.tail1234.ts.net" {
		t.Errorf("CertStatus.TailnetHostname = %q", st.TailnetHostname)
	}
	m.ClearTailnetCert()
	if m.TailnetHostname() != "" {
		t.Error("TailnetHostname should be empty after ClearTailnetCert")
	}
}

func TestWithdrawRegisteredHostname_HidesInstalledCertName(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	installTestDeviceCert(t, m, "abc.device.usbridge.test")
	if got := m.RegisteredHostname(); got != "abc.device.usbridge.test" {
		t.Fatalf("RegisteredHostname = %q before withdraw", got)
	}
	m.WithdrawRegisteredHostname()
	if got := m.RegisteredHostname(); got != "" {
		t.Errorf("RegisteredHostname = %q after withdraw, want empty", got)
	}
	m.SetRegisteredHostname("abc.device.usbridge.test")
	if got := m.RegisteredHostname(); got != "abc.device.usbridge.test" {
		t.Errorf("RegisteredHostname = %q after re-register", got)
	}
}

// newTestManager is NewManager(dir) that waits, before the test's dir is
// removed, for the demand-stamp write a handshake starts in the background.
func newTestManager(t *testing.T, dir string) *Manager {
	t.Helper()
	m := NewManager(dir)
	t.Cleanup(m.WaitBackground)
	return m
}
