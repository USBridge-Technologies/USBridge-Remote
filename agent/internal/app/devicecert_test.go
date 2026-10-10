package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
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

// parseCSRForHostname decodes the {hw_id, csr} POST body tickDeviceCert
// sent, fails the test unless the CSR's own SAN names hostname -- the real
// backend performs exactly this check (see usbridge-entitlement-backend's
// csrCoversHostname) before ever spending an ACME order on it -- and
// returns the parsed CSR for signTestCSR below.
func parseCSRForHostname(t *testing.T, r *http.Request, hostname string) *x509.CertificateRequest {
	t.Helper()
	var body struct {
		HwID string `json:"hw_id"`
		CSR  string `json:"csr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode /v1/device/cert request body: %v", err)
	}
	csrDER, err := base64.StdEncoding.DecodeString(body.CSR)
	if err != nil {
		t.Fatalf("csr is not valid base64: %v", err)
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		t.Fatalf("csr does not parse: %v", err)
	}
	for _, name := range csr.DNSNames {
		if name == hostname {
			return csr
		}
	}
	t.Fatalf("csr DNSNames = %v, want to include %q", csr.DNSNames, hostname)
	return nil
}

// signTestCSR builds a leaf certificate for csr's OWN public key, signed by
// a throwaway CA key generated here -- standing in for what a real CA
// (Let's Encrypt via ACME) does: sign the caller's CSR, never mint a
// keypair of its own. Using an unrelated keypair here (as an earlier,
// buggy version of this helper did) would make InstallDeviceCert correctly
// reject the result with "private key does not match public key", since in
// production the cert's public key must match the device's own persisted
// key that DeviceCSR signed the CSR with.
func signTestCSR(t *testing.T, csr *x509.CertificateRequest) (certPEM string) {
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
		cfg:     config.Config{StateDir: dir, AccountToken: "test-account-token"},
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
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/cert":
			certRequested = true
			csr := parseCSRForHostname(t, r, "abc123.device.usbridge.test")
			certPEM := signTestCSR(t, csr)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(devicecert.Cert{
				CertPEM:  certPEM,
				Hostname: "abc123.device.usbridge.test",
				NotAfter: "2027-01-01T00:00:00Z",
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})).URL)

	a := newTestAppWithTLS(t)
	a.tlsMgr.NoteDeviceCertDemand() // a browser came for the name
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

// Without a browser having come for the name, the name is registered (and
// handed out) but no certificate is spent on it.
func TestTickDeviceCert_NoCertWithoutDemand(t *testing.T) {
	if netutil.PreferredIPv4() == "" {
		t.Skip("no LAN interface available in this sandbox to derive a preferred IPv4 from")
	}
	certRequests := 0
	withDeviceCertBackendURL(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/device/dns":
			_ = json.NewEncoder(w).Encode(map[string]string{"hostname": "abc123.device.usbridge.test"})
		case "/v1/device/cert":
			certRequests++
			w.WriteHeader(http.StatusInternalServerError)
		}
	})).URL)

	a := newTestAppWithTLS(t)
	if err := a.tickDeviceCert(context.Background()); err != nil {
		t.Fatal(err)
	}
	if certRequests != 0 {
		t.Fatalf("cert requested %d times without demand", certRequests)
	}
	if got := a.tlsMgr.RegisteredHostname(); got != "abc123.device.usbridge.test" {
		t.Fatalf("RegisteredHostname = %q", got)
	}

	// A handshake for that name is the demand.
	_, _ = a.tlsMgr.GetCertificate(&tls.ClientHelloInfo{ServerName: "abc123.device.usbridge.test"})
	select {
	case <-a.tlsMgr.DemandC():
	default:
		t.Fatal("a handshake for the registered name must signal demand")
	}
	if !a.tlsMgr.DeviceCertDemanded(deviceCertDemandWindow) {
		t.Fatal("demand not recorded")
	}
}

// Signed out: demand alone doesn't get a certificate; the reason is shown.
func TestTickDeviceCert_NeedsAccount(t *testing.T) {
	if netutil.PreferredIPv4() == "" {
		t.Skip("no LAN interface available in this sandbox to derive a preferred IPv4 from")
	}
	certRequests := 0
	withDeviceCertBackendURL(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/device/dns":
			_ = json.NewEncoder(w).Encode(map[string]string{"hostname": "abc123.device.usbridge.test"})
		case "/v1/device/cert":
			certRequests++
		}
	})).URL)
	a := newTestAppWithTLS(t)
	a.cfg.AccountToken = ""
	a.tlsMgr.NoteDeviceCertDemand()
	if err := a.tickDeviceCert(context.Background()); !errors.Is(err, devicecert.ErrAccountRequired) {
		t.Fatalf("tick error = %v, want ErrAccountRequired", err)
	}
	if certRequests != 0 {
		t.Fatalf("cert requested %d times while signed out", certRequests)
	}
	if st := a.tlsMgr.CertStatus(); st.LastError == "" {
		t.Fatal("the reason must show in the cert status")
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
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/cert":
			certRequests++
			csr := parseCSRForHostname(t, r, "abc123.device.usbridge.test")
			certPEM := signTestCSR(t, csr)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(devicecert.Cert{CertPEM: certPEM, Hostname: "abc123.device.usbridge.test", NotAfter: "2027-01-01T00:00:00Z"})
		}
	})).URL)

	a := newTestAppWithTLS(t)
	a.tlsMgr.NoteDeviceCertDemand() // a browser came for the name
	a.tickDeviceCert(context.Background())
	a.tickDeviceCert(context.Background()) // second tick: same hostname, cert still fresh

	if certRequests != 1 {
		t.Errorf("GET /v1/device/cert called %d times, want 1 (second tick should skip the fetch, see DeviceCertStatus)", certRequests)
	}
}

func TestTickDeviceCert_ReusesCertOnIPChange(t *testing.T) {
	if netutil.PreferredIPv4() == "" {
		t.Skip("no LAN interface available in this sandbox to derive a preferred IPv4 from")
	}

	dnsRegistrations := 0
	certRequests := 0
	withDeviceCertBackendURL(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/dns":
			dnsRegistrations++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"hostname": "abc123.device.usbridge.test"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/cert":
			certRequests++
			csr := parseCSRForHostname(t, r, "abc123.device.usbridge.test")
			certPEM := signTestCSR(t, csr)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(devicecert.Cert{CertPEM: certPEM, Hostname: "abc123.device.usbridge.test", NotAfter: "2027-01-01T00:00:00Z"})
		}
	})).URL)

	a := newTestAppWithTLS(t)
	a.tlsMgr.NoteDeviceCertDemand() // a browser came for the name

	// First tick registers IP and fetches wildcard cert once
	if err := a.tickDeviceCert(context.Background()); err != nil {
		t.Fatalf("first tick failed: %v", err)
	}

	// Second tick (simulating IP change or re-check): DNS registered again, but cert is reused!
	if err := a.tickDeviceCert(context.Background()); err != nil {
		t.Fatalf("second tick failed: %v", err)
	}

	if dnsRegistrations != 2 {
		t.Errorf("POST /v1/device/dns called %d times, want 2", dnsRegistrations)
	}
	if certRequests != 1 {
		t.Errorf("GET /v1/device/cert called %d times, want 1 (wildcard cert must be reused across IP changes)", certRequests)
	}
}

// TestDeviceCertTickOutcome covers deviceCertWatchdog's retry-backoff
// decision for every outcome tickDeviceCert can return. The last case is a
// regression test for the 2026-10-04 incident: an error that is neither nil
// nor ErrPending nor ErrRateLimited (a network failure, hwid unavailable, or
// -- what actually happened -- a non-JSON response like an edge/WAF block
// page) used to leave registeredIP/registerTime untouched, which made the
// caller's ticker loop retry on every 3-second tick forever instead of
// backing off, turning a transient backend problem into a self-reinforcing
// request storm.
func TestDeviceCertTickOutcome(t *testing.T) {
	const ip = "192.168.1.5"
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	t.Run("success backs off the full register interval", func(t *testing.T) {
		gotIP, gotTime := deviceCertTickOutcome(nil, ip, now)
		if gotIP != ip || !gotTime.Equal(now) {
			t.Errorf("deviceCertTickOutcome(nil, ...) = (%q, %v), want (%q, %v)", gotIP, gotTime, ip, now)
		}
	})

	t.Run("pending retries sooner than the full register interval", func(t *testing.T) {
		gotIP, gotTime := deviceCertTickOutcome(devicecert.ErrPending, ip, now)
		if gotIP != ip {
			t.Errorf("registeredIP = %q, want %q", gotIP, ip)
		}
		// runTick's ticker loop fires again once time.Since(registerTime) >=
		// deviceCertRegisterInterval, so the effective next-retry instant is
		// registerTime + deviceCertRegisterInterval -- confirm that lands
		// deviceCertPendingRetry after now, not a full deviceCertRegisterInterval.
		gotNextTick := gotTime.Add(deviceCertRegisterInterval)
		wantNextTick := now.Add(deviceCertPendingRetry)
		if !gotNextTick.Equal(wantNextTick) {
			t.Errorf("effective next retry = %v, want %v", gotNextTick, wantNextTick)
		}
	})

	t.Run("rate-limited backs off same as success, not every poll tick", func(t *testing.T) {
		gotIP, gotTime := deviceCertTickOutcome(devicecert.ErrRateLimited, ip, now)
		if gotIP != ip || !gotTime.Equal(now) {
			t.Errorf("deviceCertTickOutcome(ErrRateLimited, ...) = (%q, %v), want (%q, %v)", gotIP, gotTime, ip, now)
		}
	})

	t.Run("an unrecognized error still backs off instead of leaving state untouched", func(t *testing.T) {
		gotIP, gotTime := deviceCertTickOutcome(errors.New("boom: unexpected HTML from edge"), ip, now)
		if gotTime.IsZero() {
			t.Fatal("registerTime is zero -- caller's ticker loop will retry on every deviceCertPollInterval tick forever")
		}
		if gotIP != ip {
			t.Errorf("registeredIP = %q, want %q (must be set even on failure, or the ticker loop's IP-changed check never settles)", gotIP, ip)
		}
		if !gotTime.Equal(now) {
			t.Errorf("registerTime = %v, want %v", gotTime, now)
		}
	})
}
