package devicecert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// server starts an httptest.Server answering both routes this package
// calls, and points backendBaseURL at it for the duration of the test --
// mirrors internal/entitlement/download_test.go's downloadInfoServer.
func server(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prev := TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() { TestSetBackendBaseURL(prev) })
}

func TestRegisterIP_SendsHwIDAndIPReturnsHostname(t *testing.T) {
	var gotBody map[string]string
	server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/device/dns" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"hostname": "abc123.device.usbridge.io"})
	})

	hostname, err := RegisterIP(context.Background(), "hw-1", "192.168.1.50")
	if err != nil {
		t.Fatalf("RegisterIP: %v", err)
	}
	if hostname != "abc123.device.usbridge.io" {
		t.Errorf("hostname = %q, want abc123.device.usbridge.io", hostname)
	}
	if gotBody["hw_id"] != "hw-1" || gotBody["ip"] != "192.168.1.50" {
		t.Errorf("request body = %+v, want hw_id=hw-1 ip=192.168.1.50", gotBody)
	}
}

func TestRegisterIP_PropagatesBackendError(t *testing.T) {
	server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"refusing to publish non-private address"}`))
	})

	if _, err := RegisterIP(context.Background(), "hw-1", "8.8.8.8"); err == nil {
		t.Fatal("expected an error when the backend rejects the IP, got nil")
	}
}

func TestFetchCert_ReturnsCertKeyAndMetadata(t *testing.T) {
	var gotQuery string
	server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/device/cert" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Cert{
			CertPEM:        "CERTPEM",
			KeyPEM:         "KEYPEM",
			HostnameSuffix: "device.usbridge.io",
			NotAfter:       "2027-01-01T00:00:00Z",
		})
	})

	cert, err := FetchCert(context.Background(), "hw-1")
	if err != nil {
		t.Fatalf("FetchCert: %v", err)
	}
	if cert.CertPEM != "CERTPEM" || cert.KeyPEM != "KEYPEM" || cert.HostnameSuffix != "device.usbridge.io" {
		t.Errorf("cert = %+v, unexpected shape", cert)
	}
	if gotQuery != "hw_id=hw-1" {
		t.Errorf("query = %q, want hw_id=hw-1", gotQuery)
	}
}

func TestFetchCert_PropagatesBackendError(t *testing.T) {
	server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"wildcard cert unavailable"}`))
	})

	if _, err := FetchCert(context.Background(), "hw-1"); err == nil {
		t.Fatal("expected an error on HTTP 503, got nil")
	}
}
