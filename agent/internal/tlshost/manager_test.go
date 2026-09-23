package tlshost

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"
)

func TestEnsureSelfSigned_GeneratesAndCovers(t *testing.T) {
	m := NewManager(t.TempDir())
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
	m := NewManager(t.TempDir())
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
	m := NewManager(t.TempDir())
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
	m1 := NewManager(dir)
	if err := m1.EnsureSelfSigned([]net.IP{net.ParseIP("127.0.0.1")}, nil); err != nil {
		t.Fatal(err)
	}

	m2 := NewManager(dir)
	m2.LoadPersisted()
	if m2.selfLeaf == nil {
		t.Fatal("LoadPersisted did not pick up the previously generated self-signed cert")
	}
	if m2.selfLeaf.SerialNumber.Cmp(m1.selfLeaf.SerialNumber) != 0 {
		t.Error("loaded cert has a different serial than what was persisted -- not actually the same cert")
	}
}

// generateTestCert builds a throwaway self-signed cert/key PEM pair
// covering `dns`, standing in for what devicecert.FetchCert would return.
func generateTestCert(t *testing.T, dns string) (certPEM, keyPEM string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: dns},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
		DNSNames:     []string{dns},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	return certPEM, keyPEM
}

func TestInstallDeviceCert_ThenGetCertificatePicksItBySNI(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.EnsureSelfSigned([]net.IP{net.ParseIP("127.0.0.1")}, nil); err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM := generateTestCert(t, "abc123.device.usbridge.io")
	if err := m.InstallDeviceCert("abc123.device.usbridge.io", certPEM, keyPEM); err != nil {
		t.Fatalf("InstallDeviceCert: %v", err)
	}

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
	m := NewManager(t.TempDir())
	hostname, needsRefresh := m.DeviceCertStatus()
	if hostname != "" || !needsRefresh {
		t.Errorf("DeviceCertStatus on a fresh Manager = (%q, %v), want (\"\", true)", hostname, needsRefresh)
	}
}

func TestDeviceCertStatus_NoRefreshWhenFresh(t *testing.T) {
	m := NewManager(t.TempDir())
	certPEM, keyPEM := generateTestCert(t, "abc123.device.usbridge.io")
	if err := m.InstallDeviceCert("abc123.device.usbridge.io", certPEM, keyPEM); err != nil {
		t.Fatal(err)
	}
	hostname, needsRefresh := m.DeviceCertStatus()
	if hostname != "abc123.device.usbridge.io" || needsRefresh {
		t.Errorf("DeviceCertStatus after install = (%q, %v), want (abc123.device.usbridge.io, false)", hostname, needsRefresh)
	}
}

func TestGetCertificate_ErrorsWhenNothingInstalledYet(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.GetCertificate(&tls.ClientHelloInfo{}); err == nil {
		t.Fatal("expected an error before EnsureSelfSigned/InstallDeviceCert has ever run")
	}
}
