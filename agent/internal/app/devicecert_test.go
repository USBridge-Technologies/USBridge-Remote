package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/devicecert"
	"usbridge_agent/internal/netutil"
	"usbridge_agent/internal/tlshost"
)

// generateTestServerCert builds a throwaway self-signed cert/key PEM pair
// covering dns, standing in for what the real backend's GET
// /v1/device/cert would return -- mirrors internal/tlshost's own
// generateTestCert test helper (unexported there, so duplicated here
// rather than imported).
func generateTestServerCert(t *testing.T, dns string) (certPEM, keyPEM string) {
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

// withDeviceCertBackendURL mirrors app_test.go's own withBackendURL, for
// the separate devicecert package's own backend base URL var (see that
// package's doc comment on why every backend-calling package here keeps
// its own copy rather than sharing one).
func withDeviceCertBackendURL(t *testing.T, url string) {
	t.Helper()
	orig := devicecert.TestSetBackendBaseURL(url)
	t.Cleanup(func() { devicecert.TestSetBackendBaseURL(orig) })
}

// newTestAppWithTLS is newTestApp (app_test.go) plus a real tlshost.Manager
// backed by a scratch temp dir, for tickDeviceCert's own tests.
func newTestAppWithTLS(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	return &App{
		cfgPath: filepath.Join(dir, "config.yaml"),
		cfg:     config.Config{StateDir: dir},
		tlsMgr:  tlshost.NewManager(dir),
	}
}

func TestTickDeviceCert_RegistersIPAndInstallsCertOnFirstRun(t *testing.T) {
	if netutil.PreferredIPv4() == "" {
		t.Skip("no LAN interface available in this sandbox to derive a preferred IPv4 from")
	}

	var registeredIP string
	var certRequested bool
	withDeviceCertBackendURL(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/dns":
			var body struct {
				HwID string `json:"hw_id"`
				IP   string `json:"ip"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			registeredIP = body.IP
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"hostname": "abc123.device.usbridge.test"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/cert":
			certRequested = true
			certPEM, keyPEM := generateTestServerCert(t, "abc123.device.usbridge.test")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(devicecert.Cert{
				CertPEM:        certPEM,
				KeyPEM:         keyPEM,
				HostnameSuffix: "device.usbridge.test",
				NotAfter:       "2027-01-01T00:00:00Z",
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})).URL)

	a := newTestAppWithTLS(t)
	a.tickDeviceCert(context.Background())

	if registeredIP == "" {
		t.Fatal("tickDeviceCert never called POST /v1/device/dns")
	}
	if !certRequested {
		t.Fatal("tickDeviceCert did not fetch a cert on first run (no cert was installed yet)")
	}
	hostname, needsRefresh := a.tlsMgr.DeviceCertStatus()
	if hostname != "abc123.device.usbridge.test" || needsRefresh {
		t.Errorf("DeviceCertStatus after tick = (%q, %v), want (abc123.device.usbridge.test, false)", hostname, needsRefresh)
	}
}

func TestTickDeviceCert_SkipsCertFetchWhenHostnameUnchangedAndFresh(t *testing.T) {
	if netutil.PreferredIPv4() == "" {
		t.Skip("no LAN interface available in this sandbox to derive a preferred IPv4 from")
	}

	certRequests := 0
	withDeviceCertBackendURL(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/dns":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"hostname": "abc123.device.usbridge.test"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/cert":
			certRequests++
			certPEM, keyPEM := generateTestServerCert(t, "abc123.device.usbridge.test")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(devicecert.Cert{CertPEM: certPEM, KeyPEM: keyPEM, HostnameSuffix: "device.usbridge.test", NotAfter: "2027-01-01T00:00:00Z"})
		}
	})).URL)

	a := newTestAppWithTLS(t)
	a.tickDeviceCert(context.Background())
	a.tickDeviceCert(context.Background()) // second tick: same hostname, cert still fresh

	if certRequests != 1 {
		t.Errorf("GET /v1/device/cert called %d times, want 1 (second tick should skip the fetch, see DeviceCertStatus)", certRequests)
	}
}
